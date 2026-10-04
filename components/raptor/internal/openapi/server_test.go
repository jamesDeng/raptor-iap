package openapi

import (
	"context"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"net/http"
	"net/http/httptest"
	"testing"
)

type serviceTransport struct{}

func (serviceTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.SetBasicAuth("svc", "test-password")
	return http.DefaultTransport.RoundTrip(r)
}
func TestStreamableHTTPAuthenticatedSDK(t *testing.T) {
	h, e := NewHandler(Client{BaseURL: "http://127.0.0.1:1", Username: "svc", Password: "test-password"})
	if e != nil {
		t.Fatal(e)
	}
	server := httptest.NewServer(h)
	defer server.Close()
	response, e := http.Get(server.URL + "/mcp")
	if e != nil {
		t.Fatal(e)
	}
	response.Body.Close()
	if response.StatusCode != 401 {
		t.Fatal("unauthenticated MCP")
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "1"}, nil)
	session, e := client.Connect(context.Background(), &mcp.StreamableClientTransport{Endpoint: server.URL + "/mcp", HTTPClient: &http.Client{Transport: serviceTransport{}}, DisableStandaloneSSE: true}, nil)
	if e != nil {
		t.Fatal(e)
	}
	defer session.Close()
	list, e := session.ListTools(context.Background(), &mcp.ListToolsParams{})
	if e != nil || len(list.Tools) == 0 {
		t.Fatal("real MCP transport failed")
	}
}
