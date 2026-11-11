package runtime

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/jamesDeng/raptor-iap/components/agent-gateway/internal/execution"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func proxyStart() execution.LiveStart {
	b := execution.AttemptBinding{RequestID: "request", AttemptID: "attempt", DefinitionSHA256: "hash", EnvCode: "rdev.ali", ClusterID: "cluster", Operation: "db-proxy.replace-nodes", ExecutionMode: "replacement", ObjectKind: "db-proxy", ObjectCode: "proxy"}
	scope := execution.ReplacementScope{EnvCode: b.EnvCode, ProxyCode: b.ObjectCode, TargetDBCode: "db", GroupID: "group", ServerGroupID: "backend", Application: execution.ApplicationScope{EnvCode: b.EnvCode, AppCode: "app", ClusterID: b.ClusterID, Namespace: "raptor-test", Name: "client", UID: "uid"}}
	scope = resourceRuntimeScope(scope)
	return execution.LiveStart{Binding: b, Access: execution.AgentAccess{Binding: b, ReplacementScope: &scope, ExpiresAt: time.Now().Add(180 * time.Second)}, Deadline: time.Now().Add(180 * time.Second)}
}
func TestObservationProxyBindsAndRevokes(t *testing.T) {
	calls := 0
	allowed := true
	p, e := NewObservationProxy("https://api.rdev.raptor-iap.top/v1/gateway-observations", func(ctx context.Context, in execution.LiveStart, m ObservationMaterial, tool string, args json.RawMessage) (json.RawMessage, error) {
		calls++
		return json.RawMessage(`{"replicas":2}`), nil
	})
	if e != nil {
		t.Fatal(e)
	}
	in := proxyStart()
	m := ObservationMaterial{Config: []byte(`{"private":{"password":"do-not-deliver"}}`), Kubeconfig: []byte("private-kube")}
	guest, e := p.Register(in, m, func(context.Context) error {
		if !allowed {
			return errors.New("lost")
		}
		return nil
	})
	if e != nil {
		t.Fatal(e)
	}
	var c struct {
		Token string `json:"token"`
	}
	json.Unmarshal(guest.Config, &c)
	if len(c.Token) < 32 || strings.Contains(string(guest.Config), "do-not-deliver") || len(guest.Kubeconfig) != 0 {
		t.Fatal("credential isolation")
	}
	invoke := func(body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", "/v1/gateway-observations", strings.NewReader(body))
		r.Header.Set("Authorization", "Bearer "+c.Token)
		w := httptest.NewRecorder()
		p.ServeHTTP(w, r)
		return w
	}
	raw := `{"attemptId":"attempt","requestId":"request","definitionSha256":"hash","tool":"deployment_read","arguments":{}}`
	if w := invoke(raw); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	for _, bad := range []string{strings.Replace(raw, "request\"", "other\"", 1), strings.Replace(raw, "deployment_read", "restart", 1), strings.Replace(raw, `"arguments":{}`, `"arguments":{},"url":"http://evil"`, 1), raw + `{}`} {
		if w := invoke(bad); w.Code == 200 {
			t.Fatal("bad request executed", bad)
		}
	}
	allowed = false
	if w := invoke(raw); w.Code != 401 {
		t.Fatal("lost owner accepted", w.Code)
	}
	if calls != 1 {
		t.Fatal("invalid backend calls", calls)
	}
	p.Remove(in.Binding.AttemptID)
	allowed = true
	if w := invoke(raw); w.Code != 401 {
		t.Fatal("retired token accepted")
	}
}
func TestObservationProxySanitizesResult(t *testing.T) {
	p, _ := NewObservationProxy("https://api.rdev.raptor-iap.top/v1/gateway-observations", func(context.Context, execution.LiveStart, ObservationMaterial, string, json.RawMessage) (json.RawMessage, error) {
		return json.RawMessage(`{"secret":"do-not-export"}`), nil
	})
	m, _ := p.Register(proxyStart(), ObservationMaterial{Secrets: []string{"do-not-export"}}, func(context.Context) error { return nil })
	var c map[string]any
	json.Unmarshal(m.Config, &c)
	r := httptest.NewRequest("POST", "/v1/gateway-observations", strings.NewReader(`{"attemptId":"attempt","requestId":"request","definitionSha256":"hash","tool":"deployment_read","arguments":{}}`))
	r.Header.Set("Authorization", "Bearer "+c["token"].(string))
	w := httptest.NewRecorder()
	p.ServeHTTP(w, r)
	if w.Code != 503 || strings.Contains(w.Body.String(), "do-not-export") {
		t.Fatal("unsafe result", w.Code)
	}
}
func TestObservationProxyRejectsExpiredAndConcurrentAttempt(t *testing.T) {
	p, _ := NewObservationProxy("https://api.rdev.raptor-iap.top/v1/gateway-observations", func(context.Context, execution.LiveStart, ObservationMaterial, string, json.RawMessage) (json.RawMessage, error) {
		return nil, nil
	})
	in := proxyStart()
	in.Deadline = time.Now().Add(-time.Second)
	if _, e := p.Register(in, ObservationMaterial{}, func(context.Context) error { return nil }); e == nil {
		t.Fatal("expired")
	}
	in = proxyStart()
	if _, e := p.Register(in, ObservationMaterial{}, func(context.Context) error { return nil }); e != nil {
		t.Fatal(e)
	}
	in.Binding.AttemptID = "second"
	in.Access.Binding = in.Binding
	if _, e := p.Register(in, ObservationMaterial{}, func(context.Context) error { return nil }); e == nil {
		t.Fatal("concurrent")
	}
}
func TestObservationProxyRejectsExpiredSource(t *testing.T) {
	p, _ := NewObservationProxy("https://api.rdev.raptor-iap.top/v1/gateway-observations", func(context.Context, execution.LiveStart, ObservationMaterial, string, json.RawMessage) (json.RawMessage, error) {
		return nil, nil
	})
	raw, _ := json.Marshal(map[string]any{"expiresAt": time.Now().Add(-time.Second)})
	if _, e := p.Register(proxyStart(), ObservationMaterial{Config: raw}, func(context.Context) error { return nil }); e == nil {
		t.Fatal("expired underlying credentials registered")
	}
}

func TestObservationProxyRejectsAttemptCredentialRepresentations(t *testing.T) {
	in := proxyStart()
	in.Access.Credential = "raptor_at_" + strings.Repeat("a", 43)
	encoded := base64.StdEncoding.EncodeToString([]byte("agent:" + in.Access.Credential))
	for _, secret := range []string{in.Access.Credential, "agent:" + in.Access.Credential, encoded, "Basic " + encoded} {
		t.Run(secret[:8], func(t *testing.T) {
			p, _ := NewObservationProxy("https://api.rdev.raptor-iap.top/v1/gateway-observations", func(context.Context, execution.LiveStart, ObservationMaterial, string, json.RawMessage) (json.RawMessage, error) {
				return json.Marshal(map[string]string{"leak": secret})
			})
			m, err := p.Register(in, ObservationMaterial{}, func(context.Context) error { return nil })
			if err != nil {
				t.Fatal(err)
			}
			var c map[string]any
			json.Unmarshal(m.Config, &c)
			if strings.Contains(string(m.Config), in.Access.Credential) || strings.Contains(string(m.Config), encoded) {
				t.Fatal("private access exported")
			}
			r := httptest.NewRequest("POST", observationRoute, strings.NewReader(`{"attemptId":"attempt","requestId":"request","definitionSha256":"hash","tool":"deployment_read","arguments":{}}`))
			r.Header.Set("Authorization", "Bearer "+c["token"].(string))
			w := httptest.NewRecorder()
			p.ServeHTTP(w, r)
			if w.Code != 503 || strings.Contains(w.Body.String(), secret) {
				t.Fatal("attempt credential escaped", w.Code)
			}
		})
	}
}

func resourceRuntimeScope(s execution.ReplacementScope) execution.ReplacementScope {
	s.AccountID = "1360282071200743"
	s.Region = "ap-southeast-1"
	s.ListenerID = "listener"
	s.Port = 6432
	s.DatabaseResourceID = "db-instance"
	a := s.Application
	raw := fmt.Sprintf(`{"envCode":%q,"accountId":%q,"region":%q,"clusterId":%q,"proxyCode":%q,"groupId":%q,"serverGroupId":%q,"listenerId":%q,"port":%d,"targetDbCode":%q,"databaseResourceId":%q,"application":{"envCode":%q,"appCode":%q,"clusterId":%q,"namespace":%q,"name":%q,"uid":%q}}`, s.EnvCode, s.AccountID, s.Region, a.ClusterID, s.ProxyCode, s.GroupID, s.ServerGroupID, s.ListenerID, s.Port, s.TargetDBCode, s.DatabaseResourceID, a.EnvCode, a.AppCode, a.ClusterID, a.Namespace, a.Name, a.UID)
	s.ResolutionDigest = fmt.Sprintf("%x", sha256.Sum256([]byte(raw)))
	return s
}

func preflight(t *testing.T, p *ObservationProxy, ctx context.Context, in execution.LiveStart) error {
	t.Helper()
	runner, ok := any(p).(interface {
		qualificationPreflight(context.Context, execution.LiveStart) error
	})
	if !ok {
		t.Fatal("private registered-entry qualification preflight is missing")
	}
	return runner.qualificationPreflight(ctx, in)
}

func TestObservationPreflightUsesExactRegisteredPrivateAccess(t *testing.T) {
	in := proxyStart()
	in.Access.Credential = "issued-private-access"
	calls := 0
	p, _ := NewObservationProxy("https://api.rdev.raptor-iap.top"+observationRoute, func(ctx context.Context, actual execution.LiveStart, m ObservationMaterial, tool string, args json.RawMessage) (json.RawMessage, error) {
		calls++
		if actual.Access.Credential != in.Access.Credential || string(m.Config) != `{"private":"source"}` || tool != "metrics_read" || string(args) != `{"kind":"pgcatClients"}` {
			t.Fatal("not fixed private observation")
		}
		deadline, ok := ctx.Deadline()
		if !ok || time.Until(deadline) > 20*time.Second {
			t.Fatal("missing observation budget")
		}
		return json.RawMessage(`{"nodes":[]}`), nil
	})
	guest, err := p.Register(in, ObservationMaterial{Config: []byte(`{"private":"source"}`)}, func(context.Context) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(guest.Config), `"private":"source"`) {
		t.Fatal("raw material exported")
	}
	if err = preflight(t, p, context.Background(), in); err != nil || calls != 1 {
		t.Fatal("registered read failed", err, calls)
	}
	for _, field := range []string{"binding", "credential", "expiry", "infra", "raptor", "scope", "missing-scope"} {
		t.Run(field, func(t *testing.T) {
			foreign := in
			scope := *in.Access.ReplacementScope
			foreign.Access.ReplacementScope = &scope
			switch field {
			case "binding":
				foreign.Binding.Model = "foreign"
				foreign.Access.Binding = foreign.Binding
			case "credential":
				foreign.Access.Credential = "another"
			case "expiry":
				foreign.Access.ExpiresAt = foreign.Access.ExpiresAt.Add(time.Second)
			case "infra":
				foreign.Access.InfraMcpURL = "other"
			case "raptor":
				foreign.Access.RaptorMcpURL = "other"
			case "scope":
				scope.DatabaseResourceID = "other"
			case "missing-scope":
				foreign.Access.ReplacementScope = nil
			}
			if preflight(t, p, context.Background(), foreign) == nil {
				t.Fatal("foreign access accepted")
			}
		})
	}
	if calls != 1 {
		t.Fatal("foreign access executed", calls)
	}
	p.Remove(in.Binding.AttemptID)
	if preflight(t, p, context.Background(), in) == nil {
		t.Fatal("removed entry accepted")
	}
}

func TestObservationPreflightSharesExecutionGuards(t *testing.T) {
	for _, mode := range []string{"busy", "expired", "lease-before", "lease-after", "cancel-before", "cancel-after", "removed", "non-json", "oversize", "source-secret", "credential", "basic", "late", "late-check", "removed-by-check"} {
		t.Run(mode, func(t *testing.T) {
			in := proxyStart()
			in.Access.Credential = "raptor_at_" + strings.Repeat("a", 43)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			var p *ObservationProxy
			calls, checks := 0, 0
			p, _ = NewObservationProxy("https://api.rdev.raptor-iap.top"+observationRoute, func(ctx context.Context, _ execution.LiveStart, _ ObservationMaterial, _ string, _ json.RawMessage) (json.RawMessage, error) {
				calls++
				switch mode {
				case "cancel-after":
					cancel()
				case "removed":
					p.Remove(in.Binding.AttemptID)
				case "non-json":
					return []byte(`oops`), nil
				case "oversize":
					return json.Marshal(strings.Repeat("x", 262145))
				case "source-secret":
					return []byte(`{"leak":"private-source"}`), nil
				case "credential":
					return json.Marshal(in.Access.Credential)
				case "basic":
					return json.Marshal(gatewayObservationSecrets(in.Access.Credential)[3])
				case "late":
					<-ctx.Done()
				}
				return []byte(`{"nodes":[]}`), nil
			})
			_, err := p.Register(in, ObservationMaterial{Secrets: []string{"private-source"}}, func(ctx context.Context) error {
				checks++
				if mode == "lease-before" || mode == "lease-after" && checks > 1 {
					return ErrRuntimeUnavailable
				}
				if mode == "removed-by-check" && checks > 1 {
					p.Remove(in.Binding.AttemptID)
				}
				if mode == "late-check" && checks > 1 {
					cancel()
				}
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
			switch mode {
			case "busy":
				for i := 0; i < cap(p.busy); i++ {
					p.busy <- struct{}{}
				}
				defer func() {
					for len(p.busy) > 0 {
						<-p.busy
					}
				}()
			case "expired":
				p.mu.Lock()
				for k, e := range p.entries {
					e.expires = time.Now().Add(-time.Second)
					p.entries[k] = e
				}
				p.mu.Unlock()
			case "cancel-before":
				cancel()
			case "late":
				var stop context.CancelFunc
				ctx, stop = context.WithTimeout(ctx, 10*time.Millisecond)
				defer stop()
			}
			if preflight(t, p, ctx, in) == nil {
				t.Fatal("guard bypassed")
			}
			if (mode == "busy" || mode == "expired" || mode == "lease-before" || mode == "cancel-before") && calls != 0 {
				t.Fatal("executed before guards", calls)
			}
		})
	}
}

func TestObservationPreflightSnapshotsScopeAndRejectsAmbiguousEntry(t *testing.T) {
	in := proxyStart()
	calls := 0
	p, _ := NewObservationProxy("https://api.rdev.raptor-iap.top"+observationRoute, func(context.Context, execution.LiveStart, ObservationMaterial, string, json.RawMessage) (json.RawMessage, error) {
		calls++
		return []byte(`{}`), nil
	})
	if _, e := p.Register(in, ObservationMaterial{}, func(context.Context) error { return nil }); e != nil {
		t.Fatal(e)
	}
	original := *in.Access.ReplacementScope
	in.Access.ReplacementScope.DatabaseResourceID = "changed-after-registration"
	if preflight(t, p, context.Background(), in) == nil || calls != 0 {
		t.Fatal("registered authority changed through source pointer")
	}
	in.Access.ReplacementScope = &original
	if e := preflight(t, p, context.Background(), in); e != nil {
		t.Fatal(e)
	}
	p.mu.Lock()
	for _, entry := range p.entries {
		p.entries[sha256.Sum256([]byte("ambiguous"))] = entry
		break
	}
	p.mu.Unlock()
	if preflight(t, p, context.Background(), in) == nil || calls != 1 {
		t.Fatal("ambiguous registered match accepted")
	}
}
func TestObservationPreflightUsesMinimumExpiry(t *testing.T) {
	for _, mode := range []string{"source", "access", "attempt", "context"} {
		t.Run(mode, func(t *testing.T) {
			in := proxyStart()
			until := time.Now().Add(time.Second)
			material := ObservationMaterial{}
			ctx := context.Background()
			if mode == "source" {
				material.Config, _ = json.Marshal(map[string]any{"expiresAt": until})
			}
			if mode == "access" {
				in.Access.ExpiresAt = until
			}
			if mode == "attempt" {
				in.Deadline = until
			}
			if mode == "context" {
				var cancel context.CancelFunc
				ctx, cancel = context.WithDeadline(ctx, until)
				defer cancel()
			}
			p, _ := NewObservationProxy("https://api.rdev.raptor-iap.top"+observationRoute, func(ctx context.Context, _ execution.LiveStart, _ ObservationMaterial, _ string, _ json.RawMessage) (json.RawMessage, error) {
				deadline, ok := ctx.Deadline()
				if !ok || !deadline.Equal(until) {
					t.Fatal("expiry not propagated to executor", deadline, until)
				}
				return []byte(`{}`), nil
			})
			if _, e := p.Register(in, material, func(context.Context) error { return nil }); e != nil {
				t.Fatal(e)
			}
			if e := preflight(t, p, ctx, in); e != nil {
				t.Fatal(e)
			}
		})
	}
}
func TestObservationPreflightRejectsEveryRegisteredSecret(t *testing.T) {
	in := proxyStart()
	in.Access.Credential = "raptor_at_" + strings.Repeat("a", 43)
	secrets := append([]string{"private-one", "private-two", "private-three"}, gatewayObservationSecrets(in.Access.Credential)...)
	for i, secret := range secrets {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			p, _ := NewObservationProxy("https://api.rdev.raptor-iap.top"+observationRoute, func(context.Context, execution.LiveStart, ObservationMaterial, string, json.RawMessage) (json.RawMessage, error) {
				return json.Marshal(map[string]string{"leak": secret})
			})
			if _, e := p.Register(in, ObservationMaterial{Secrets: secrets[:3]}, func(context.Context) error { return nil }); e != nil {
				t.Fatal(e)
			}
			if preflight(t, p, context.Background(), in) == nil {
				t.Fatal("registered secret accepted")
			}
		})
	}
}

func TestObservationProxyAllowsFourBoundedReads(t *testing.T) {
	entered := make(chan struct{}, 4)
	release := make(chan struct{})
	defer close(release)
	p, _ := NewObservationProxy("https://api.rdev.raptor-iap.top/v1/gateway-observations", func(context.Context, execution.LiveStart, ObservationMaterial, string, json.RawMessage) (json.RawMessage, error) {
		entered <- struct{}{}
		<-release
		return json.RawMessage(`{"ready":true}`), nil
	})
	m, err := p.Register(proxyStart(), ObservationMaterial{}, func(context.Context) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	var c map[string]any
	json.Unmarshal(m.Config, &c)
	hash := sha256.Sum256([]byte(c["token"].(string)))
	entry := p.entries[hash]
	results := make(chan int, 4)
	for i := 0; i < 4; i++ {
		go func() {
			_, status := p.guardedExecute(context.Background(), hash, entry, "deployment_read", json.RawMessage(`{}`))
			results <- status
		}()
	}
	for i := 0; i < 4; i++ {
		select {
		case <-entered:
		case status := <-results:
			t.Fatalf("read refused before four active: %d", status)
		case <-time.After(time.Second):
			t.Fatal("reads did not overlap")
		}
	}
	_, status := p.guardedExecute(context.Background(), hash, entry, "deployment_read", json.RawMessage(`{}`))
	if status != 429 {
		t.Fatalf("overflow status %d", status)
	}
}

func TestObservationProxyWaitsAndRechecksFleet(t *testing.T) {
	calls := 0
	var first time.Time
	p, err := NewObservationProxy("https://api.rdev.raptor-iap.top"+observationRoute, func(_ context.Context, _ execution.LiveStart, _ ObservationMaterial, tool string, args json.RawMessage) (json.RawMessage, error) {
		if tool != "cloud_read" || string(args) != `{"kind":"fleet"}` {
			t.Fatalf("unexpected selector %s %s", tool, args)
		}
		calls++
		if calls == 1 {
			first = time.Now()
			return nil, ErrRuntimeUnavailable
		}
		if time.Since(first) < time.Second {
			t.Fatal("retried without waiting")
		}
		return json.RawMessage(`{"instances":[]}`), nil
	})
	if err != nil {
		t.Fatal(err)
	}
	in := proxyStart()
	_, err = p.Register(in, ObservationMaterial{}, func(context.Context) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	hash, entry, ok := p.registeredEntry(in)
	if !ok {
		t.Fatal("missing entry")
	}
	_, code := p.guardedExecute(t.Context(), hash, entry, "cloud_read", json.RawMessage(`{"kind":"fleet"}`))
	if code != 200 || calls != 2 {
		t.Fatalf("status %d, fleet calls %d", code, calls)
	}
}
