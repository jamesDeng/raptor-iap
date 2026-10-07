package api

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"net/http"
	"net/http/httptest"
	"raptor-iap/infra-api/internal/domain"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type observedReader struct {
	calls atomic.Int32
	err   error
}

func (f *observedReader) Identity(context.Context, domain.Environment) (string, error) {
	f.calls.Add(1)
	return "123", f.err
}
func (f *observedReader) Deployments(context.Context, domain.Environment, string, string) ([]domain.Deployment, error) {
	f.calls.Add(1)
	return nil, f.err
}
func (f *observedReader) Status(context.Context, domain.Environment, domain.Target) (domain.Status, error) {
	f.calls.Add(1)
	return domain.Status{UID: "uid", ReadyReplicas: 1, EvidenceMode: "live"}, f.err
}

type mcpAuthTransport struct{}

func (mcpAuthTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Header.Set("X-Infra-Authorization", valid())
	return http.DefaultTransport.RoundTrip(r)
}
func mcpSession(t *testing.T, f *observedReader) (*mcp.ClientSession, context.Context) {
	t.Helper()
	h, e := New(f, map[string]domain.Environment{"dev": {Code: "dev", AccountID: "123"}}, "user", "secret", "X-Infra-Authorization")
	if e != nil {
		t.Fatal(e)
	}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "1"}, nil)
	s, e := client.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: srv.URL + "/mcp", HTTPClient: &http.Client{Transport: mcpAuthTransport{}}, DisableStandaloneSSE: true}, nil)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { s.Close() })
	return s, ctx
}
func TestMCPTransportAndReadResults(t *testing.T) {
	f := &observedReader{}
	s, ctx := mcpSession(t, f)
	list, e := s.ListTools(ctx, &mcp.ListToolsParams{})
	if e != nil || len(list.Tools) != 3 {
		t.Fatalf("list: %v %v", list, e)
	}
	cases := []struct {
		name string
		args map[string]any
		want string
	}{
		{"cloud_identity_get", map[string]any{"envCode": "dev"}, `"accountMatches":true`},
		{"deployments_list", map[string]any{"envCode": "dev", "kind": "application", "code": "app"}, `"data":[]`},
		{"deployment_status_get", map[string]any{"envCode": "dev", "appCode": "app", "clusterId": "cluster", "namespace": "ns", "name": "app", "uid": "uid"}, `"readyReplicas":1`},
	}
	for _, x := range cases {
		r, e := s.CallTool(ctx, &mcp.CallToolParams{Name: x.name, Arguments: x.args})
		if e != nil || r.IsError {
			t.Fatalf("%s: %v %v", x.name, r, e)
		}
		b, _ := json.Marshal(r.StructuredContent)
		if !strings.Contains(string(b), x.want) {
			t.Fatalf("%s", b)
		}
	}
	if f.calls.Load() != 3 {
		t.Fatal("missing provider calls")
	}
}
func TestMCPInvalidArgumentsDoNotReachProvider(t *testing.T) {
	f := &observedReader{}
	s, ctx := mcpSession(t, f)
	for _, args := range []map[string]any{
		{"envCode": "other", "kind": "application", "code": "app"},
		{"envCode": "dev", "kind": "application", "code": "app", "extra": "x"},
		{"envCode": "dev", "kind": "application", "code": ""},
		{"envCode": "dev", "kind": "application", "code": strings.Repeat("x", 257)},
		{"envCode": "dev", "kind": "invalid", "code": "app"},
		{"envCode": 12, "kind": "application", "code": "app"},
	} {
		r, e := s.CallTool(ctx, &mcp.CallToolParams{Name: "deployments_list", Arguments: args})
		if e == nil && !r.IsError {
			t.Fatalf("accepted %v", args)
		}
	}
	r, e := s.CallTool(ctx, &mcp.CallToolParams{Name: "unknown", Arguments: map[string]any{}})
	if e == nil && !r.IsError {
		t.Fatal("unknown tool accepted")
	}
	if f.calls.Load() != 0 {
		t.Fatal("invalid arguments reached provider")
	}
}
func TestMCPProviderErrorsAreSafe(t *testing.T) {
	for _, x := range []struct {
		err  error
		code string
	}{{domain.ErrScope, "ScopeMismatch"}, {domain.ErrIdentity, "IdentityChanged"}, {domain.ErrNotConfigured, "NotConfigured"}, {errors.New("private-secret"), "ProviderUnavailable"}} {
		f := &observedReader{err: x.err}
		s, ctx := mcpSession(t, f)
		r, e := s.CallTool(ctx, &mcp.CallToolParams{Name: "deployments_list", Arguments: map[string]any{"envCode": "dev", "kind": "application", "code": "app"}})
		b, _ := json.Marshal(r)
		if e != nil || !r.IsError || !strings.Contains(string(b), x.code) || strings.Contains(string(b), "private-secret") {
			t.Fatalf("unsafe result %s %v", b, e)
		}
	}
}
func TestMCPAuthenticationPrecedesProtocol(t *testing.T) {
	f := &observedReader{}
	h, _ := New(f, map[string]domain.Environment{}, "user", "secret", "X-Infra-Authorization")
	for _, a := range [][]string{nil, {"Basic bad"}, {valid(), valid()}, {"Bearer secret"}} {
		r := httptest.NewRequest("POST", "/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
		for _, v := range a {
			r.Header.Add("X-Infra-Authorization", v)
		}
		r.Header.Set("Authorization", valid())
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != 401 {
			t.Fatalf("auth got %d", w.Code)
		}
	}
	if f.calls.Load() != 0 {
		t.Fatal("auth reached provider")
	}
}

func TestMCPDuplicateSelectorsRejected(t *testing.T) {
	f := &observedReader{}
	s, ctx := mcpSession(t, f)
	r, e := s.CallTool(ctx, &mcp.CallToolParams{Name: "cloud_identity_get", Arguments: json.RawMessage(`{"envCode":"other","envCode":"dev"}`)})
	if e == nil && !r.IsError {
		t.Fatal("duplicate selectors accepted")
	}
	if f.calls.Load() != 0 {
		t.Fatal("duplicates reached provider")
	}
}
