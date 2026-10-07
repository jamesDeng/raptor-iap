package api

import (
	"context"
	"encoding/json"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"net/http"
	"net/http/httptest"
	"raptor-iap/infra-api/internal/domain"
	"strings"
	"testing"
)

func TestMutationMCPMatchesHTTPAndRejectsInvalidCalls(t *testing.T) {
	cmd := &commandFixture{}
	h, e := NewWithCommands(fakeReader{}, map[string]domain.Environment{"dev": {Code: "dev"}}, "user", "secret", "X-Infra-Authorization", cmd, allowCommands{})
	if e != nil {
		t.Fatal(e)
	}
	server := httptest.NewServer(h)
	defer server.Close()
	ctx := context.Background()
	client := mcp.NewClient(&mcp.Implementation{Name: "commands-test", Version: "1"}, nil)
	session, e := client.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: server.URL + "/mcp", HTTPClient: &http.Client{Transport: mcpAuthTransport{}}, DisableStandaloneSSE: true}, nil)
	if e != nil {
		t.Fatal(e)
	}
	defer session.Close()
	cases := []struct{ tool, path, body string }{
		{"deployment_restart", "/v1/deployment-restart", `{"requestId":"r","envCode":"dev","appCode":"app","clusterId":"c","namespace":"n","name":"a","uid":"u"}`},
		{"db_proxy_scale", "/v1/db-proxy-scale", `{"requestId":"r","actionId":"a","envCode":"dev","proxyCode":"p","groupId":"g","desiredCapacity":2}`},
		{"db_proxy_node_protection_set", "/v1/db-proxy-node-protection", `{"requestId":"r","envCode":"dev","proxyCode":"p","groupId":"g","instanceIds":["i"],"protected":false}`},
		{"db_proxy_nodes_deregister", "/v1/db-proxy-node-deregistration", `{"requestId":"r","envCode":"dev","proxyCode":"p","groupId":"g","serverGroupId":"s","instanceIds":["i"]}`},
	}
	for _, tc := range cases {
		r := httptest.NewRequest("POST", tc.path, strings.NewReader(tc.body))
		r.Header.Set("X-Infra-Authorization", valid())
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		result, e := session.CallTool(ctx, &mcp.CallToolParams{Name: tc.tool, Arguments: json.RawMessage(tc.body)})
		if e != nil || result.IsError {
			t.Fatal(result, e)
		}
		var httpResult any
		json.Unmarshal(w.Body.Bytes(), &httpResult)
		a, _ := json.Marshal(httpResult)
		b, _ := json.Marshal(result.StructuredContent)
		if w.Code != 200 || string(a) != string(b) {
			t.Fatalf("HTTP/MCP mismatch: %s %s", a, b)
		}
	}
	before := cmd.calls
	for _, body := range []string{
		`{"requestId":"r","actionId":"a","envCode":"dev","proxyCode":"p","groupId":"g"}`,
		`{"requestId":"r","actionId":"a","envCode":"dev","proxyCode":"p","groupId":"g","desiredCapacity":2.5}`,
		`{"requestId":"r","actionId":"a","envCode":"foreign","proxyCode":"p","groupId":"g","desiredCapacity":2}`,
	} {
		result, e := session.CallTool(ctx, &mcp.CallToolParams{Name: "db_proxy_scale", Arguments: json.RawMessage(body)})
		if e != nil || !result.IsError {
			t.Fatal(result, e)
		}
	}
	if cmd.calls != before {
		t.Fatal("invalid calls reached provider")
	}
}

func TestQuestionServerDoesNotExposeCommandCatalog(t *testing.T) {
	a := QuestionAuthorizer{RequestID: "r", AttemptID: "a", EnvCode: "dev", AppCode: "app", Verify: func(context.Context, string) (QuestionScope, error) {
		return QuestionScope{RequestID: "r", AttemptID: "a", EnvCode: "dev", AppCode: "app", Active: true}, nil
	}}
	h, e := NewWithCommands(fakeReader{}, map[string]domain.Environment{"dev": {Code: "dev"}}, "user", "secret", "X-Infra-Authorization", &commandFixture{}, a)
	if e != nil {
		t.Fatal(e)
	}
	server := httptest.NewServer(h)
	defer server.Close()
	client := mcp.NewClient(&mcp.Implementation{Name: "question-test", Version: "1"}, nil)
	session, e := client.Connect(context.Background(), &mcp.StreamableClientTransport{Endpoint: server.URL + "/mcp", HTTPClient: &http.Client{Transport: mcpAuthTransport{}}, DisableStandaloneSSE: true}, nil)
	if e != nil {
		t.Fatal(e)
	}
	defer session.Close()
	list, e := session.ListTools(context.Background(), &mcp.ListToolsParams{})
	if e != nil || len(list.Tools) != 3 {
		t.Fatalf("question catalog widened: %v %v", list, e)
	}
}
