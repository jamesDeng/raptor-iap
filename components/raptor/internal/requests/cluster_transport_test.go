package requests_test

import (
	"context"
	"encoding/json"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/openapi"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/requests"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRenderedClusterURLsUseActualClients(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		u, p, ok := r.BasicAuth()
		if !ok || u != "fixture" || p != "fixture-only" {
			t.Error("authentication missing")
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"data":{"status":"running"}}`))
	}))
	defer server.Close()
	previous := http.DefaultTransport
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, network, server.Listener.Addr().String())
	}
	http.DefaultTransport = transport
	t.Cleanup(func() { transport.CloseIdleConnections(); http.DefaultTransport = previous })
	g := requests.HTTPGateway{BaseURL: "http://agent-gateway:8874", Username: "fixture", Password: "fixture-only", AllowClusterHTTP: true}
	if e := g.PutRequest(context.Background(), "fixture"); e != nil {
		t.Fatal(e)
	}
	if _, e := g.Execution(context.Background(), "fixture"); e != nil {
		t.Fatal(e)
	}
	c := openapi.Client{BaseURL: "http://raptor-backend:8871", Username: "fixture", Password: "fixture-only", AllowClusterHTTP: true}
	if _, e := c.Call(context.Background(), "POST", "/v1/agent-access/introspect", map[string]string{}); e != nil {
		t.Fatal(e)
	}
	if calls != 3 {
		t.Fatal("not all actual clients reached private services")
	}
	g.AllowClusterHTTP = false
	if e := g.SendSignal(context.Background(), "fixture", json.RawMessage(`{}`)); e == nil {
		t.Fatal("default unexpectedly permits cluster HTTP")
	}
	c.AllowClusterHTTP = false
	if _, e := c.Call(context.Background(), "GET", "/v1/requests/fixture", nil); e == nil {
		t.Fatal("default unexpectedly permits cluster HTTP")
	}
	if calls != 3 {
		t.Fatal("unapproved request sent")
	}
}
