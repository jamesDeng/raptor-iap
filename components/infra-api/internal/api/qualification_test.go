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
	"time"
)

func TestQualificationBlocksHTTPAndMCPDespiteActiveAuthorization(t *testing.T) {
	expiry := time.Now().Add(time.Minute)
	identity := AgentIdentity{Active: true, ExpiresAt: &expiry, Binding: &AgentBinding{RequestID: "request", AttemptID: "attempt", Operation: "db-proxy.replace-nodes", ObjectKind: "db-proxy", ObjectCode: "proxy", EnvCode: "dev", ClusterID: "cluster", ExecutionMode: "qualification"}, ReplacementScope: &AgentReplacementScope{EnvCode: "dev", ProxyCode: "proxy", GroupID: "group", ServerGroupID: "backend", TargetDBCode: "db", Application: AgentApplication{EnvCode: "dev", AppCode: "client", ClusterID: "cluster", Namespace: "ns", Name: "client", UID: "uid"}}}
	check := func(context.Context, string, string, map[string]string) (AgentIdentity, error) { return identity, nil }
	cmd := &commandFixture{}
	h, e := NewWithAgentCommands(fakeReader{}, map[string]domain.Environment{"dev": {Code: "dev"}}, "user", "secret", "X-Infra-Authorization", cmd, allowCommands{}, check)
	if e != nil {
		t.Fatal(e)
	}
	calls := []struct{ path, name, body string }{
		{"/v1/db-proxy-scale", "db_proxy_scale", `{"requestId":"request","envCode":"dev","proxyCode":"proxy","groupId":"group","actionId":"already-approved","desiredCapacity":1}`},
		{"/v1/db-proxy-node-protection", "db_proxy_node_protection_set", `{"requestId":"request","envCode":"dev","proxyCode":"proxy","groupId":"group","instanceIds":["i-one"],"protected":false}`},
		{"/v1/db-proxy-node-deregistration", "db_proxy_nodes_deregister", `{"requestId":"request","envCode":"dev","proxyCode":"proxy","groupId":"group","serverGroupId":"backend","instanceIds":["i-one"]}`},
		{"/v1/deployment-restart", "deployment_restart", `{"requestId":"request","envCode":"dev","appCode":"client","clusterId":"cluster","namespace":"ns","name":"client","uid":"uid"}`}}
	for _, c := range calls {
		r := httptest.NewRequest("POST", c.path, strings.NewReader(c.body))
		r.Header.Set("X-Infra-Authorization", agentHeader())
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != 403 || cmd.calls != 0 {
			t.Fatalf("qualification invoked provider: %s status%d calls%d", c.name, w.Code, cmd.calls)
		}
	}
	srv := httptest.NewServer(h)
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	s, e := mcp.NewClient(&mcp.Implementation{Name: "qualification", Version: "1"}, nil).Connect(ctx, &mcp.StreamableClientTransport{Endpoint: srv.URL + "/mcp", HTTPClient: &http.Client{Transport: agentBoundaryTransport{}}, DisableStandaloneSSE: true}, nil)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	tools, e := s.ListTools(ctx, &mcp.ListToolsParams{})
	if e != nil {
		t.Fatal(e)
	}
	if len(tools.Tools) != 5 {
		t.Fatal("mutation catalog exposed", len(tools.Tools))
	}
	for _, c := range calls {
		v, e := s.CallTool(ctx, &mcp.CallToolParams{Name: c.name, Arguments: json.RawMessage(c.body)})
		if e == nil && !v.IsError {
			t.Fatal("MCP bypass", c.name)
		}
	}
	if cmd.calls != 0 {
		t.Fatal("provider touched")
	}
}
