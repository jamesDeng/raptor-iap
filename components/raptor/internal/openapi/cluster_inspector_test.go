package openapi

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAgentInspectorInheritsPrivateClusterTransport(t *testing.T) {
	t.Setenv("AGENT_INTROSPECTION_USERNAME", "inspector")
	t.Setenv("AGENT_INTROSPECTION_PASSWORD", "test-only-password")
	calls := 0
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		u, p, ok := r.BasicAuth()
		if !ok || u != "inspector" || p != "test-only-password" || r.URL.Path != "/v1/agent-access/introspect" {
			t.Error("incorrect inspector request")
			w.WriteHeader(401)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"data":{"active":false}}`))
	}))
	defer backend.Close()
	original := http.DefaultTransport
	transport := original.(*http.Transport).Clone()
	transport.Proxy = nil
	transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		if address != "raptor-backend:8871" {
			t.Errorf("unexpected backend %s", address)
		}
		return (&net.Dialer{}).DialContext(ctx, network, backend.Listener.Addr().String())
	}
	http.DefaultTransport = transport
	defer func() { transport.CloseIdleConnections(); http.DefaultTransport = original }()
	for _, allowed := range []bool{false, true} {
		calls = 0
		h, e := NewHandler(Client{BaseURL: "http://raptor-backend:8871", AllowClusterHTTP: allowed, Username: "controller", Password: "controller-test"})
		if e != nil {
			t.Fatal(e)
		}
		r := httptest.NewRequest("POST", "/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"initialize"}`))
		r.Header.Set("Authorization", "Bearer invalid-diagnostic-token")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if allowed {
			if w.Code != 403 || calls != 1 {
				t.Fatalf("cluster opt-in: status=%d backendCalls=%d; expected rejection after introspection", w.Code, calls)
			}
		} else if w.Code != 503 || calls != 0 {
			t.Fatalf("cluster HTTP unexpectedly allowed: status=%d calls=%d", w.Code, calls)
		}
	}
}
