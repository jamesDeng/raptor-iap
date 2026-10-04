package openapi

import (
	"context"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSDKInitializeListCall(t *testing.T) {
	ctx := context.Background()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if u, p, ok := r.BasicAuth(); !ok || u != "svc" || p != "test-password" {
			w.WriteHeader(401)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"data":{"requestId":"request","definition":{"envCode":"adev","object":{"kind":"database","code":"db"}}}}`))
	}))
	defer upstream.Close()
	server := NewMCP(Client{BaseURL: upstream.URL, Username: "svc", Password: "test-password"})
	ct, st := mcp.NewInMemoryTransports()
	ss, e := server.Connect(ctx, st, nil)
	if e != nil {
		t.Fatal(e)
	}
	defer ss.Close()
	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "1"}, nil)
	cs, e := client.Connect(ctx, ct, nil)
	if e != nil {
		t.Fatal(e)
	}
	defer cs.Close()
	list, e := cs.ListTools(ctx, &mcp.ListToolsParams{})
	if e != nil {
		t.Fatal(e)
	}
	found := false
	for _, tool := range list.Tools {
		if tool.Name == "request_get" {
			found = true
		}
		if tool.Name == "object_create" {
			t.Fatal("catalog mutation exposed")
		}
	}
	if !found {
		t.Fatal("request tool missing")
	}
	res, e := cs.CallTool(ctx, &mcp.CallToolParams{Name: "request_get", Arguments: map[string]any{"requestId": "request"}})
	if e != nil || res.IsError {
		t.Fatal("request context call failed")
	}
	res, e = cs.CallTool(ctx, &mcp.CallToolParams{Name: "deployments_list", Arguments: map[string]any{"requestId": "request", "envCode": "bdev"}})
	if e == nil && !res.IsError {
		t.Fatal("cross-env tool allowed")
	}
}
