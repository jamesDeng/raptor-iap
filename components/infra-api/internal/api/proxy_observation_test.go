package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"raptor-iap/infra-api/internal/domain"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Only the external provider is replaced: the MCP transport, dispatcher and
// authorization are real. Removing a selector/pin check must reach this spy.
type proxyReadFixture struct {
	fakeReader
	calls     int
	err       error
	malformed bool
	target    domain.ProxyReadTarget
	selection string
}

func (f *proxyReadFixture) ObserveProxy(_ context.Context, _ domain.Environment, target domain.ProxyReadTarget, kind string) (domain.ProxyObservation, error) {
	f.calls++
	f.target = target
	f.selection = kind
	out := domain.ProxyObservation{}
	at := time.Date(2026, 10, 30, 1, 2, 3, 0, time.UTC)
	switch kind {
	case "fleet":
		out.Fleet = &domain.FleetObservation{GroupID: "group", ObservedAt: at, Instances: []domain.FleetNode{}}
	case "backend":
		out.Backend = &domain.BackendObservation{ServerGroupID: "backend", ListenerID: "listener", ObservedAt: at, Nodes: []domain.BackendNode{}}
	case "capacity":
		out.Capacity = &domain.CapacityObservation{GroupID: "group", DesiredCapacity: 2, MinSize: 2, MaxSize: 4, ObservedAt: at}
	}
	if f.malformed {
		out.Capacity = &domain.CapacityObservation{}
	}
	return out, f.err
}
func (f *proxyReadFixture) ObservePgcatSeries(_ context.Context, _ domain.Environment, target domain.ProxyReadTarget, series string) (domain.PgcatSeriesObservation, error) {
	f.calls++
	f.target = target
	f.selection = series
	return domain.PgcatSeriesObservation{Series: series, ObservedAt: time.Date(2026, 10, 30, 1, 2, 3, 0, time.UTC), Rows: []domain.PrometheusSample{{Metric: map[string]string{"job": "pgcat"}, Value: []json.RawMessage{json.RawMessage(`1793322123`), json.RawMessage(`"1"`)}}}}, f.err
}
func proxyReadEnv() domain.Environment {
	return domain.Environment{Code: "dev", ClusterID: "cluster", Proxies: []domain.ProxyMapping{{Code: "proxy", GroupID: "group", ServerGroupID: "backend", TargetDBCode: "db", ListenerID: "listener", Port: 6432}}}
}
func proxyReadArgs(field, value string) map[string]string {
	return map[string]string{"requestId": "request", "envCode": "dev", "proxyCode": "proxy", "groupId": "group", "serverGroupId": "backend", field: value}
}
func proxySession(t *testing.T, h http.Handler, agent bool) (*mcp.ClientSession, context.Context) {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	var transport http.RoundTripper = mcpAuthTransport{}
	if agent {
		transport = agentBoundaryTransport{}
	}
	s, e := mcp.NewClient(&mcp.Implementation{Name: "proxy-test", Version: "1"}, nil).Connect(ctx, &mcp.StreamableClientTransport{Endpoint: srv.URL + "/mcp", HTTPClient: &http.Client{Transport: transport}, DisableStandaloneSSE: true}, nil)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { s.Close() })
	return s, ctx
}
func proxyServiceSession(t *testing.T, f Reader) (*mcp.ClientSession, context.Context) {
	t.Helper()
	h, e := NewWithCommands(f, map[string]domain.Environment{"dev": proxyReadEnv()}, "user", "secret", "X-Infra-Authorization", nil, NewServiceAuthorizer("user"))
	if e != nil {
		t.Fatal(e)
	}
	return proxySession(t, h, false)
}
func proxyCall(t *testing.T, s *mcp.ClientSession, ctx context.Context, tool string, args any) *mcp.CallToolResult {
	t.Helper()
	r, e := s.CallTool(ctx, &mcp.CallToolParams{Name: tool, Arguments: args})
	if e != nil {
		t.Fatal(e)
	}
	return r
}
func assertProxyFailure(t *testing.T, r *mcp.CallToolResult, code string) {
	t.Helper()
	b, _ := json.Marshal(r)
	if !r.IsError || !strings.Contains(string(b), code) || strings.Contains(string(b), "private-secret") {
		t.Fatalf("unsafe/refused error %s; want %s", b, code)
	}
}
func TestProxyMCPReadSchemasAndBranches(t *testing.T) {
	f := &proxyReadFixture{}
	s, ctx := proxyServiceSession(t, f)
	tools, e := s.ListTools(ctx, &mcp.ListToolsParams{})
	if e != nil {
		t.Fatal(e)
	}
	for _, tc := range []struct {
		name, field string
		values      []string
	}{{"proxy_observation_get", "kind", []string{"fleet", "backend", "capacity"}}, {"proxy_pgcat_metrics_get", "series", []string{"up", "upTimestamp", "active", "activeTimestamp", "idle", "idleTimestamp", "waiting", "waitingTimestamp"}}} {
		var found *mcp.Tool
		for _, tool := range tools.Tools {
			if tool.Name == tc.name {
				found = tool
			}
		}
		if found == nil {
			t.Fatalf("missing read tool %s", tc.name)
		}
		if found.Annotations == nil || !found.Annotations.ReadOnlyHint {
			t.Fatal("read annotation absent", tc.name)
		}
		b, _ := json.Marshal(found.InputSchema)
		var schema struct {
			Type       string
			Properties map[string]struct {
				Type                 string
				MinLength, MaxLength int
				Enum                 []string
			}
			Required             []string
			AdditionalProperties bool
		}
		if json.Unmarshal(b, &schema) != nil {
			t.Fatal(string(b))
		}
		if schema.Type != "object" || schema.AdditionalProperties || len(schema.Properties) != 6 || len(schema.Required) != 6 {
			t.Fatalf("schema %s", b)
		}
		required := map[string]bool{}
		for _, k := range schema.Required {
			required[k] = true
		}
		for k := range proxyReadArgs(tc.field, tc.values[0]) {
			p, ok := schema.Properties[k]
			if !ok || !required[k] || p.Type != "string" || p.MinLength != 1 || p.MaxLength != 256 {
				t.Fatalf("invalid field %s: %s", k, b)
			}
		}
		if !reflect.DeepEqual(schema.Properties[tc.field].Enum, tc.values) {
			t.Fatalf("wrong enum %s", b)
		}
		for _, value := range tc.values {
			r := proxyCall(t, s, ctx, tc.name, proxyReadArgs(tc.field, value))
			if r.IsError {
				t.Fatalf("read refused %s %v", value, r)
			}
			structured, _ := json.Marshal(r.StructuredContent)
			if len(r.Content) != 1 {
				t.Fatal("missing matching text")
			}
			text, ok := r.Content[0].(*mcp.TextContent)
			if !ok {
				t.Fatal("not text")
			}
			var a, b any
			if json.Unmarshal(structured, &a) != nil || json.Unmarshal([]byte(text.Text), &b) != nil || !reflect.DeepEqual(a, b) {
				t.Fatal("envelopes differ")
			}
			data := a.(map[string]any)["data"].(map[string]any)
			if data["observedAt"] != "2026-10-30T01:02:03Z" {
				t.Fatal("source timestamp lost", data)
			}
			if data["Fleet"] != nil || data["Backend"] != nil || data["Capacity"] != nil {
				t.Fatal("union leaked", data)
			}
			switch value {
			case "fleet":
				if _, ok := data["instances"].([]any); !ok {
					t.Fatal(data)
				}
			case "backend":
				if data["listenerId"] != "listener" {
					t.Fatal(data)
				}
			case "capacity":
				if data["desiredCapacity"] != float64(2) {
					t.Fatal(data)
				}
			default:
				if data["series"] != value || len(data["rows"].([]any)) != 1 {
					t.Fatal(data)
				}
			}
			if f.target != (domain.ProxyReadTarget{RequestID: "request", EnvCode: "dev", ProxyCode: "proxy", GroupID: "group", ServerGroupID: "backend"}) || f.selection != value {
				t.Fatal("wrong scoped provider target", f.target, f.selection)
			}
		}
	}
	if f.calls != 11 {
		t.Fatal("missing fixed reads", f.calls)
	}
}
func TestProxyMCPInvalidInputsNeverDispatch(t *testing.T) {
	f := &proxyReadFixture{}
	s, ctx := proxyServiceSession(t, f)
	for _, tc := range []struct{ tool, field, value string }{{"proxy_observation_get", "kind", "fleet"}, {"proxy_pgcat_metrics_get", "series", "up"}} {
		valid := proxyReadArgs(tc.field, tc.value)
		var invalid []any
		for k := range valid {
			m := proxyReadArgs(tc.field, tc.value)
			delete(m, k)
			invalid = append(invalid, m)
			m = proxyReadArgs(tc.field, tc.value)
			m[k] = ""
			invalid = append(invalid, m)
			m = proxyReadArgs(tc.field, tc.value)
			m[k] = strings.Repeat("x", 257)
			invalid = append(invalid, m)
		}
		for _, key := range []string{"extra", "targetDBCode", "clusterId", "query", "url"} {
			m := proxyReadArgs(tc.field, tc.value)
			m[key] = "foreign"
			invalid = append(invalid, m)
		}
		m := proxyReadArgs(tc.field, "db_proxy_scale")
		invalid = append(invalid, m)
		raw, _ := json.Marshal(valid)
		invalid = append(invalid, json.RawMessage(strings.TrimSuffix(string(raw), "}")+`,"requestId":"request"}`), json.RawMessage(strings.Replace(string(raw), `"requestId":"request"`, `"requestId":12`, 1)))
		for _, args := range invalid {
			r, e := s.CallTool(ctx, &mcp.CallToolParams{Name: tc.tool, Arguments: args})
			if e != nil {
				t.Fatalf("invalid input escaped sanitized tool error: %v", e)
			}
			assertProxyFailure(t, r, "InvalidInput")
		}
	}
	if f.calls != 0 {
		t.Fatal("invalid caller reached provider", f.calls)
	}
}
func TestProxyMCPProviderFailuresAndOptionalInterface(t *testing.T) {
	for _, tc := range []struct {
		err  error
		code string
	}{{domain.ErrScope, "ScopeMismatch"}, {domain.ErrIdentity, "IdentityChanged"}, {domain.ErrNotConfigured, "NotConfigured"}, {errors.New("private-secret"), "ProviderUnavailable"}} {
		f := &proxyReadFixture{err: tc.err}
		s, ctx := proxyServiceSession(t, f)
		assertProxyFailure(t, proxyCall(t, s, ctx, "proxy_observation_get", proxyReadArgs("kind", "fleet")), tc.code)
		assertProxyFailure(t, proxyCall(t, s, ctx, "proxy_pgcat_metrics_get", proxyReadArgs("series", "up")), tc.code)
	}
	s, ctx := proxyServiceSession(t, fakeReader{})
	assertProxyFailure(t, proxyCall(t, s, ctx, "proxy_observation_get", proxyReadArgs("kind", "fleet")), "NotConfigured")
	f := &proxyReadFixture{malformed: true}
	s, ctx = proxyServiceSession(t, f)
	assertProxyFailure(t, proxyCall(t, s, ctx, "proxy_observation_get", proxyReadArgs("kind", "fleet")), "ProviderUnavailable")
}
func proxyIdentity() AgentIdentity {
	expiry := time.Now().Add(time.Minute)
	return AgentIdentity{Active: true, ExpiresAt: &expiry, Binding: &AgentBinding{RequestID: "request", AttemptID: "attempt", Operation: "db-proxy.replace-nodes", ObjectKind: "db-proxy", ObjectCode: "proxy", EnvCode: "dev", ClusterID: "cluster"}, ReplacementScope: &AgentReplacementScope{EnvCode: "dev", ProxyCode: "proxy", GroupID: "group", ServerGroupID: "backend", TargetDBCode: "db", Application: AgentApplication{EnvCode: "dev", AppCode: "app", ClusterID: "cluster", Namespace: "ns", Name: "app", UID: "uid"}}}
}
func TestProxyMCPAttemptAndRegisteredPins(t *testing.T) {
	for _, tool := range []string{"proxy_observation_get", "proxy_pgcat_metrics_get"} {
		field, value := "kind", "fleet"
		if tool == "proxy_pgcat_metrics_get" {
			field, value = "series", "up"
		}
		cases := []string{"replacement", "qualification", "question", "revoked", "expired", "scope-change", "binding-change", "db-mismatch", "cluster-mismatch", "no-mapping", "duplicate-mapping", "missing-identity", "wrong-authorizer"}
		for _, key := range []string{"requestId", "envCode", "proxyCode", "groupId", "serverGroupId"} {
			cases = append(cases, "foreign-"+key)
		}
		for _, which := range cases {
			t.Run(tool+"/"+which, func(t *testing.T) {
				f := &proxyReadFixture{}
				initial := proxyIdentity()
				env := proxyReadEnv()
				args := proxyReadArgs(field, value)
				if which == "qualification" {
					initial.Binding.ExecutionMode = "qualification"
				}
				if which == "question" {
					initial.Binding.Operation = "application.question"
					initial.Binding.ObjectKind = "application"
					initial.ReplacementScope = nil
				}
				if strings.HasPrefix(which, "foreign-") {
					args[strings.TrimPrefix(which, "foreign-")] = "foreign"
				}
				switch which {
				case "db-mismatch":
					env.Proxies[0].TargetDBCode = "foreign"
				case "cluster-mismatch":
					env.ClusterID = "foreign"
				case "no-mapping":
					env.Proxies = nil
				case "duplicate-mapping":
					env.Proxies = append(env.Proxies, env.Proxies[0])
				}
				checks := 0
				check := func(_ context.Context, _ string, name string, selectors map[string]string) (AgentIdentity, error) {
					if name == "" {
						return initial, nil
					}
					checks++
					if name != tool || len(selectors) != 5 || selectors["kind"] != "" || selectors["series"] != "" {
						t.Error("raw selection escaped normalization", name, selectors)
					}
					current := proxyIdentity()
					current.Binding.ExecutionMode = initial.Binding.ExecutionMode
					if which == "question" {
						current = initial
					}
					switch which {
					case "revoked":
						current.Active = false
					case "expired":
						past := time.Now().Add(-time.Minute)
						current.ExpiresAt = &past
					case "scope-change":
						current.ReplacementScope.TargetDBCode = "changed"
					case "binding-change":
						current.Binding.AttemptID = "changed"
					}
					return current, nil
				}
				var h http.Handler
				if which == "missing-identity" || which == "wrong-authorizer" {
					var auth Authorizer
					if which == "wrong-authorizer" {
						auth = allowCommands{}
					}
					h = newMCPHandler(operations{reader: f, scopes: map[string]domain.Environment{"dev": env}, principal: "agent", authorizer: auth})
				} else {
					var e error
					h, e = NewWithAgentCommands(f, map[string]domain.Environment{"dev": env}, "user", "secret", "X-Infra-Authorization", nil, NewServiceAuthorizer("user"), check)
					if e != nil {
						t.Fatal(e)
					}
				}
				s, ctx := proxySession(t, h, true)
				r := proxyCall(t, s, ctx, tool, args)
				if which == "replacement" || which == "qualification" {
					if r.IsError || f.calls != 1 || checks != 1 {
						t.Fatal("bound read refused or redundant introspection", r, f.calls, checks)
					}
				} else {
					assertProxyFailure(t, r, "ScopeMismatch")
					if f.calls != 0 {
						t.Fatal("invalid scope dispatched", which)
					}
				}
			})
		}
	}
}
