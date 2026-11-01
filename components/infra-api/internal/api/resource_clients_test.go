package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"raptor-iap/infra-api/internal/domain"
	"raptor-iap/infra-api/internal/resources"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

const clientsRPC = `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"proxy_pgcat_metrics_get","arguments":{"requestId":"r","envCode":"dev","proxyCode":"p","series":"clients"}}}`

type clientsResourceReader struct {
	resourceObserver
	bundle domain.PgcatClientsObservation
	calls  int
	read   func(context.Context)
}

func clientsResourceFixture() *clientsResourceReader {
	at := time.Date(2026, 10, 1, 0, 0, 3, 0, time.UTC)
	rows := []domain.PrometheusSample{{Metric: map[string]string{"job": "pgcat", "pool": "test", "user": "app", "ecs_instance_id": "i"}, Value: []json.RawMessage{json.RawMessage(`1790812800.125`), json.RawMessage(`"1.00"`)}}}
	return &clientsResourceReader{bundle: domain.PgcatClientsObservation{Series: "clients", ObservedAt: at,
		Fleet:   domain.FleetObservation{GroupID: "g", ObservedAt: at.Add(-2 * time.Second), Instances: []domain.FleetNode{}},
		Backend: domain.BackendObservation{ServerGroupID: "sg", ListenerID: "l", ObservedAt: at.Add(-time.Second), Nodes: []domain.BackendNode{}},
		Samples: domain.PgcatClientsSamples{Up: rows, UpTimestamp: rows, Active: rows, ActiveTimestamp: rows, Idle: rows, IdleTimestamp: rows, Waiting: rows, WaitingTimestamp: rows}}}
}

func (f *clientsResourceReader) ObservePgcatClients(ctx context.Context, _ domain.Environment, target domain.ProxyReadTarget) (domain.PgcatClientsObservation, error) {
	f.calls++
	f.target = target
	if f.read != nil {
		f.read(ctx)
	}
	return f.bundle, nil
}

type clientsResourceResolver struct {
	resourceFixture
	proofRead func(context.Context, int, resources.Proof) (resources.Proof, error)
}

func (f *clientsResourceResolver) ResolveProof(ctx context.Context, _, _ string) (resources.Proof, error) {
	f.proofs++
	if f.proofRead != nil {
		return f.proofRead(ctx, f.proofs, f.proof)
	}
	return f.proof, nil
}

// Removing the resource-only clients selection must fail through the actual
// authenticated MCP HTTP path, before any provider fixture can hide the defect.
func TestResourceMCPClientsSelectionUsesBoundProof(t *testing.T) {
	i, proof := replacementResourceIdentity()
	i.Binding.ExecutionMode = "qualification"
	f := &resourceFixture{rows: resourceRows(), proof: proof}
	reader := clientsResourceFixture()
	checks := 0
	h, err := NewResourceServer(reader, f, "user", "secret", "X-Infra-Authorization", nil, nil, func(_ context.Context, _ string, tool string, selectors map[string]string) (AgentIdentity, error) {
		checks++
		if tool != "proxy_pgcat_metrics_get" || len(selectors) != 3 || selectors["requestId"] != "r" || selectors["envCode"] != "dev" || selectors["proxyCode"] != "p" {
			t.Errorf("unexpected authorization: %s %v", tool, selectors)
		}
		return i, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	w := resourceRPC(h, clientsRPC, agentCredential())
	if w.Code != 200 || strings.Contains(w.Body.String(), `"isError":true`) || !strings.Contains(w.Body.String(), `"series":"clients"`) {
		t.Fatalf("clients bundle rejected: %d %s", w.Code, w.Body.String())
	}
	if checks != 2 || f.calls != 1 || f.proofs != 2 {
		t.Fatalf("expected initial/final authority and proof checks; got checks=%d resolves=%d proofs=%d", checks, f.calls, f.proofs)
	}
	var rpc struct {
		Result struct {
			StructuredContent struct {
				Data struct {
					ObservedAt time.Time `json:"observedAt"`
					Workload   struct {
						Metrics domain.PgcatClientsObservation `json:"metrics"`
					} `json:"pgproxyWorkload"`
				} `json:"data"`
			} `json:"structuredContent"`
			Content []struct {
				Text string `json:"text"`
			} `json:"content"`
		} `json:"result"`
	}
	if json.Unmarshal(w.Body.Bytes(), &rpc) != nil || !reflect.DeepEqual(rpc.Result.StructuredContent.Data.Workload.Metrics, reader.bundle) || !rpc.Result.StructuredContent.Data.ObservedAt.Equal(reader.bundle.ObservedAt) {
		t.Fatal("lost source times/raw samples", w.Body.String())
	}
	var text, structured any
	if len(rpc.Result.Content) != 1 || json.Unmarshal([]byte(rpc.Result.Content[0].Text), &text) != nil {
		t.Fatal(w.Body.String())
	}
	// Decode the untyped structured result so all original keys participate.
	var full map[string]json.RawMessage
	_ = json.Unmarshal(w.Body.Bytes(), &full)
	var result map[string]json.RawMessage
	_ = json.Unmarshal(full["result"], &result)
	_ = json.Unmarshal(result["structuredContent"], &structured)
	if !reflect.DeepEqual(text, structured) {
		t.Fatal("text/structured envelopes differ")
	}
	data := structured.(map[string]any)["data"].(map[string]any)
	metrics := data["pgproxyWorkload"].(map[string]any)["metrics"].(map[string]any)
	if len(metrics) != 5 || len(metrics["samples"].(map[string]any)) != 8 {
		t.Fatal("wrong bundle keys", metrics)
	}
	if reader.target != (domain.ProxyReadTarget{RequestID: "r", EnvCode: "dev", ProxyCode: "p", GroupID: "g", ServerGroupID: "sg"}) {
		t.Fatal(reader.target)
	}
}

// A second full proof must catch database/app realization changes for both
// service and agent reads, without exporting the earlier successful bundle.
func TestResourceMCPClientsRequiresInitialAndFinalFullProof(t *testing.T) {
	for _, agent := range []bool{false, true} {
		for _, phase := range []int{1, 2} {
			for _, change := range []string{"database", "appUID", "digest", "proof-error"} {
				t.Run(fmt.Sprintf("agent=%v/phase=%d/%s", agent, phase, change), func(t *testing.T) {
					i, p := replacementResourceIdentity()
					f := &clientsResourceResolver{resourceFixture: resourceFixture{rows: resourceRows(), proof: p}}
					f.proofRead = func(_ context.Context, n int, got resources.Proof) (resources.Proof, error) {
						if n >= phase {
							switch change {
							case "database":
								got.Identity.DatabaseResourceID = "foreign"
							case "appUID":
								got.Identity.Application.UID = "foreign"
							case "digest":
								got.ResolutionDigest = "invalid"
							case "proof-error":
								return resources.Proof{}, errors.New("private credential")
							}
							if change != "digest" {
								got.ResolutionDigest, _ = resources.Digest(got.Identity)
							}
						}
						return got, nil
					}
					reader := clientsResourceFixture()
					var check AgentCheck
					auth := valid()
					if agent {
						auth = agentCredential()
						check = func(context.Context, string, string, map[string]string) (AgentIdentity, error) { return i, nil }
					}
					h, err := NewResourceServer(reader, f, "user", "secret", "X-Infra-Authorization", nil, NewServiceAuthorizer("user"), check)
					if err != nil {
						t.Fatal(err)
					}
					w := resourceRPC(h, clientsRPC, auth)
					// Service has no pre-bound DB/app identity; a valid first proof is its baseline.
					wantFailure := agent || phase == 2 || change == "digest" || change == "proof-error"
					if wantFailure {
						if !strings.Contains(w.Body.String(), `"isError":true`) || strings.Contains(w.Body.String(), `"samples"`) || strings.Contains(w.Body.String(), "private credential") {
							t.Fatal(w.Body.String())
						}
					} else if strings.Contains(w.Body.String(), `"isError":true`) {
						t.Fatal(w.Body.String())
					}
					if wantFailure && phase == 1 && reader.calls != 0 {
						t.Fatal("invalid initial proof reached provider")
					}
				})
			}
		}
	}
}

func TestResourceMCPClientsFinalAuthorityAndCancellation(t *testing.T) {
	for _, change := range []string{"revoked", "expired", "binding", "shared-binding", "scope", "check-error", "cancel-read", "cancel-proof", "deadline-proof", "expire-proof"} {
		t.Run(change, func(t *testing.T) {
			i, p := replacementResourceIdentity()
			reader := clientsResourceFixture()
			f := &clientsResourceResolver{resourceFixture: resourceFixture{rows: resourceRows(), proof: p}}
			ctx, cancel := context.WithCancel(context.Background())
			if change == "deadline-proof" {
				cancel()
				ctx, cancel = context.WithTimeout(context.Background(), 30*time.Millisecond)
			}
			defer cancel()
			reader.read = func(context.Context) {
				if change == "cancel-read" {
					cancel()
				}
			}
			f.proofRead = func(proofContext context.Context, n int, got resources.Proof) (resources.Proof, error) {
				if n == 2 {
					if change == "deadline-proof" {
						original, _ := ctx.Deadline()
						actual, _ := proofContext.Deadline()
						if !original.Equal(actual) {
							t.Error("final proof extended original budget", original, actual)
						}
						<-proofContext.Done()
					}
					if change == "cancel-proof" {
						cancel()
					}
					if change == "expire-proof" {
						*i.ExpiresAt = time.Now().Add(-time.Second)
					}
				}
				return got, nil
			}
			checks := 0
			check := func(context.Context, string, string, map[string]string) (AgentIdentity, error) {
				checks++
				if change == "shared-binding" {
					if checks == 2 {
						i.Binding.AttemptID = "changed"
					}
					return i, nil
				}
				current := i
				binding := *i.Binding
				scope := *i.ReplacementScope
				current.Binding = &binding
				current.ReplacementScope = &scope
				if checks == 2 {
					switch change {
					case "revoked":
						current.Active = false
					case "expired":
						past := time.Now().Add(-time.Second)
						current.ExpiresAt = &past
					case "binding":
						current.Binding.AttemptID = "changed"
					case "scope":
						current.ReplacementScope.Application.UID = "changed"
					case "check-error":
						return AgentIdentity{}, errors.New("private credential")
					}
				}
				return current, nil
			}
			h, err := NewResourceServer(reader, f, "user", "secret", "X-Infra-Authorization", nil, nil, check)
			if err != nil {
				t.Fatal(err)
			}
			r := httptest.NewRequest("POST", "/mcp", strings.NewReader(clientsRPC)).WithContext(ctx)
			r.Header.Set("X-Infra-Authorization", agentCredential())
			r.Header.Set("Content-Type", "application/json")
			r.Header.Set("Accept", "application/json, text/event-stream")
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if strings.Contains(w.Body.String(), `"samples"`) || strings.Contains(w.Body.String(), "private credential") || (w.Body.Len() > 0 && !strings.Contains(w.Body.String(), `"isError":true`) && w.Code == 200) {
				t.Fatal("late observation exported", w.Code, w.Body.String())
			}
			if reader.calls != 1 || f.proofs > 2 {
				t.Fatal(reader.calls, f.proofs)
			}
		})
	}
}

func TestResourceMCPClientsEncodedResponseIsBounded(t *testing.T) {
	for _, tc := range []struct {
		repeats, idLength int
		oversized         bool
	}{{1000, 1000, false}, {1288, 1000, false}, {1289, 1000, true}, {1288, 2000, true}, {12000, 15000, true}, {24000, 1000, true}, {40000, 1000, true}} {
		t.Run(fmt.Sprintf("labels=%d/id=%d", tc.repeats, tc.idLength), func(t *testing.T) {
			i, p := replacementResourceIdentity()
			reader := clientsResourceFixture()
			reader.bundle.Samples.Up[0].Metric["escaped"] = strings.Repeat("<\\\"", tc.repeats)
			f := &resourceFixture{rows: resourceRows(), proof: p}
			checks := 0
			h, err := NewResourceServer(reader, f, "user", "secret", "X-Infra-Authorization", nil, NewServiceAuthorizer("user"), func(context.Context, string, string, map[string]string) (AgentIdentity, error) {
				checks++
				return i, nil
			})
			if err != nil {
				t.Fatal(err)
			}
			id := strings.Repeat("<", tc.idLength)
			request := strings.Replace(clientsRPC, `"id":1`, `"id":"`+id+`"`, 1)
			w := resourceRPC(h, request, agentCredential())
			if checks != 2 || f.proofs != 2 || reader.calls != 1 {
				t.Fatal("wire limit skipped actual agent/proof path", checks, f.proofs, reader.calls)
			}
			if w.Body.Len() > 262144 {
				t.Fatalf("encoded body exceeded client limit: %d", w.Body.Len())
			}
			var response struct {
				ID string `json:"id"`
			}
			if json.Unmarshal(w.Body.Bytes(), &response) != nil || response.ID != id {
				t.Fatal("RPC id lost or response truncated")
			}
			if !tc.oversized {
				if strings.Contains(w.Body.String(), `"isError":true`) {
					t.Fatal(w.Body.String())
				}
			} else if !strings.Contains(w.Body.String(), `"isError":true`) || strings.Contains(w.Body.String(), `"samples"`) {
				t.Fatal("oversize result was not rejected intact", w.Body.Len())
			}
		})
	}
}

func TestResourceMCPClientsRejectsUnexpectedSelectorsBeforeResolution(t *testing.T) {
	for _, change := range []string{"query", "url", "groupId", "serverGroupId", "targetDbCode", "foreign-env", "foreign-proxy", "foreign-request", "duplicate", "unknown-series"} {
		t.Run(change, func(t *testing.T) {
			i, p := replacementResourceIdentity()
			reader := clientsResourceFixture()
			f := &resourceFixture{rows: resourceRows(), proof: p}
			h, err := NewResourceServer(reader, f, "user", "secret", "X-Infra-Authorization", nil, nil, func(context.Context, string, string, map[string]string) (AgentIdentity, error) { return i, nil })
			if err != nil {
				t.Fatal(err)
			}
			request := clientsRPC
			switch change {
			case "foreign-env":
				request = strings.Replace(request, `"envCode":"dev"`, `"envCode":"other"`, 1)
			case "foreign-proxy":
				request = strings.Replace(request, `"proxyCode":"p"`, `"proxyCode":"other"`, 1)
			case "foreign-request":
				request = strings.Replace(request, `"requestId":"r"`, `"requestId":"other"`, 1)
			case "duplicate":
				request = strings.Replace(request, `"series":"clients"`, `"requestId":"r","series":"clients"`, 1)
			case "unknown-series":
				request = strings.Replace(request, `"series":"clients"`, `"series":"clients?"`, 1)
			default:
				request = strings.Replace(request, `"series":"clients"`, `"series":"clients","`+change+`":"foreign"`, 1)
			}
			w := resourceRPC(h, request, agentCredential())
			if !strings.Contains(w.Body.String(), `"isError":true`) || reader.calls != 0 || f.calls != 0 || f.proofs != 0 {
				t.Fatalf("invalid selectors reached provider/resolver: %s reads=%d resolves=%d proofs=%d", w.Body.String(), reader.calls, f.calls, f.proofs)
			}
		})
	}
}

func TestResourceProxyReadsValidateResolvedPhysicalSelectors(t *testing.T) {
	for _, series := range []string{"active", "clients"} {
		for _, change := range []string{"group-too-long", "backend-blank"} {
			t.Run(series+"/"+change, func(t *testing.T) {
				_, p := replacementResourceIdentity()
				reader := clientsResourceFixture()
				f := &resourceFixture{rows: resourceRows(), proof: p}
				if change == "group-too-long" {
					f.rows.Proxy.GroupID = strings.Repeat("x", 257)
					f.proof.Identity.GroupID = f.rows.Proxy.GroupID
				} else {
					f.rows.Proxy.ServerGroupID = " "
					f.proof.Identity.ServerGroupID = " "
				}
				f.rows.Environment.Proxies[0] = *f.rows.Proxy
				f.proof.ResolutionDigest, _ = resources.Digest(f.proof.Identity)
				h, err := NewResourceServer(reader, f, "user", "secret", "X-Infra-Authorization", nil, NewServiceAuthorizer("user"), nil)
				if err != nil {
					t.Fatal(err)
				}
				request := strings.Replace(clientsRPC, `"series":"clients"`, `"series":"`+series+`"`, 1)
				w := resourceRPC(h, request, valid())
				if !strings.Contains(w.Body.String(), `"isError":true`) || !strings.Contains(w.Body.String(), "InvalidInput") || reader.target.RequestID != "" {
					t.Fatalf("invalid resolved physical selectors reached reader: %s target=%+v", w.Body.String(), reader.target)
				}
			})
		}
	}
}

func TestResourceMCPClientsKeepsLifecycleAndEightSingleSeries(t *testing.T) {
	i, p := replacementResourceIdentity()
	reader := clientsResourceFixture()
	f := &resourceFixture{rows: resourceRows(), proof: p}
	h, err := NewResourceServer(reader, f, "user", "secret", "X-Infra-Authorization", nil, nil, func(context.Context, string, string, map[string]string) (AgentIdentity, error) { return i, nil })
	if err != nil {
		t.Fatal(err)
	}
	w := resourceRPC(h, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-03-26","capabilities":{},"clientInfo":{"name":"fixture","version":"1"}}}`, agentCredential())
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"protocolVersion"`) {
		t.Fatal(w.Code, w.Body.String())
	}
	w = resourceRPC(h, `{"jsonrpc":"2.0","method":"notifications/initialized"}`, agentCredential())
	if w.Code != 202 || w.Body.Len() != 0 {
		t.Fatal(w.Code, w.Body.String())
	}
	for _, series := range []string{"up", "upTimestamp", "active", "activeTimestamp", "idle", "idleTimestamp", "waiting", "waitingTimestamp"} {
		w = resourceRPC(h, strings.Replace(clientsRPC, `"series":"clients"`, `"series":"`+series+`"`, 1), agentCredential())
		if w.Code != 200 || strings.Contains(w.Body.String(), `"isError":true`) || !strings.Contains(w.Body.String(), `"series":"`+series+`"`) || !strings.Contains(w.Body.String(), `"value":[123,"4"]`) {
			t.Fatal(series, w.Code, w.Body.String())
		}
	}
	if reader.calls != 0 || f.calls != 8 || f.proofs != 8 {
		t.Fatal("old reads changed", reader.calls, f.calls, f.proofs)
	}
}

func TestClientsResponseDoesNotExportAfterEncodingCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":"late"}`))
		cancel()
	})
	r := httptest.NewRequest("POST", "/mcp", strings.NewReader(clientsRPC)).WithContext(ctx)
	w := httptest.NewRecorder()
	boundClientsMCPResponse(next).ServeHTTP(w, r)
	if w.Body.Len() != 0 {
		t.Fatal("response exported after caller cancellation", w.Body.String())
	}
}

type concurrentClientsReader struct {
	observedReader
	bundle domain.PgcatClientsObservation
}

func (f *concurrentClientsReader) ObservePgcatClients(context.Context, domain.Environment, domain.ProxyReadTarget) (domain.PgcatClientsObservation, error) {
	return f.bundle, nil
}

type concurrentClientsResolver struct {
	readProof func(context.Context) (resources.Proof, error)
}

func (f concurrentClientsResolver) Resolve(_ context.Context, env, kind, code string) (domain.ResolvedResource, error) {
	r := resourceRows()
	r.Context.EnvCode = env
	r.Context.Kind = kind
	r.Context.Code = code
	return r, nil
}
func (f concurrentClientsResolver) ResolveProof(ctx context.Context, _, _ string) (resources.Proof, error) {
	return f.readProof(ctx)
}

func TestResourceMCPClientsIsolatesOriginalAndDefaultRequestBudgets(t *testing.T) {
	type requestKey struct{}
	i, p := replacementResourceIdentity()
	short, cancelShort := context.WithTimeout(context.Background(), time.Second)
	defer cancelShort()
	shortDeadline, _ := short.Deadline()
	short = context.WithValue(short, requestKey{}, "short")
	long := context.WithValue(context.Background(), requestKey{}, "long")
	var mu sync.Mutex
	proofs := map[string]int{}
	deadlines := map[string]time.Time{}
	shortFinal := make(chan struct{})
	resolver := concurrentClientsResolver{readProof: func(ctx context.Context) (resources.Proof, error) {
		key, _ := ctx.Value(requestKey{}).(string)
		deadline, ok := ctx.Deadline()
		if !ok || time.Until(deadline) > 20*time.Second {
			t.Error("clients work lacks the overall20s bound", key, deadline)
		}
		if key == "short" && !deadline.Equal(shortDeadline) {
			t.Error("short caller budget replaced", deadline, shortDeadline)
		}
		mu.Lock()
		proofs[key]++
		n := proofs[key]
		first, seen := deadlines[key]
		if !seen {
			deadlines[key] = deadline
		}
		mu.Unlock()
		if seen && !first.Equal(deadline) {
			t.Error("final proof started a new budget", key, first, deadline)
		}
		if key == "short" && n == 2 {
			close(shortFinal)
			select {
			case <-ctx.Done():
			case <-short.Done():
			}
		}
		return p, nil
	}}
	h, err := NewResourceServer(&concurrentClientsReader{bundle: clientsResourceFixture().bundle}, resolver, "user", "secret", "X-Infra-Authorization", nil, nil, func(context.Context, string, string, map[string]string) (AgentIdentity, error) { return i, nil })
	if err != nil {
		t.Fatal(err)
	}
	serve := func(ctx context.Context) *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", "/mcp", strings.NewReader(clientsRPC)).WithContext(ctx)
		r.Header.Set("X-Infra-Authorization", agentCredential())
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Accept", "application/json, text/event-stream")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() { done <- serve(short) }()
	select {
	case <-shortFinal:
	case <-short.Done():
		t.Fatal("first request never reached its final proof")
	}
	w := serve(long)
	if w.Code != 200 || strings.Contains(w.Body.String(), `"isError":true`) || !strings.Contains(w.Body.String(), `"samples"`) {
		t.Fatal("independent request borrowed cancellation/budget", w.Code, w.Body.String())
	}
	cancelShort()
	w = <-done
	if strings.Contains(w.Body.String(), `"samples"`) {
		t.Fatal("cancelled request exported data", w.Body.String())
	}
	mu.Lock()
	defer mu.Unlock()
	if proofs["short"] != 2 || proofs["long"] != 2 {
		t.Fatal("request identity mixed", proofs)
	}
}

func TestLegacyPhysicalClientsSelectionRejectedBeforeProvider(t *testing.T) {
	f := &proxyReadFixture{}
	h, err := NewWithCommands(f, map[string]domain.Environment{"dev": proxyReadEnv()}, "user", "secret", "X-Infra-Authorization", nil, NewServiceAuthorizer("user"))
	if err != nil {
		t.Fatal(err)
	}
	request := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"proxy_pgcat_metrics_get","arguments":{"requestId":"request","envCode":"dev","proxyCode":"proxy","groupId":"group","serverGroupId":"backend","series":"clients"}}}`
	w := resourceRPC(h, request, valid())
	if !strings.Contains(w.Body.String(), `"isError":true`) || !strings.Contains(w.Body.String(), "InvalidInput") {
		t.Fatal(w.Body.String())
	}
	if f.calls != 0 {
		t.Fatal("legacy clients dispatched")
	}
	// Keep direct legacy operation dispatch strict as well as its MCP schema.
	_, err = (operations{reader: f, scopes: map[string]domain.Environment{"dev": proxyReadEnv()}}).proxyRead(context.Background(), "proxy_pgcat_metrics_get", proxyReadArgs("series", "clients"))
	if err == nil || err.Error() != "InvalidInput" {
		t.Fatal(err)
	}
}
