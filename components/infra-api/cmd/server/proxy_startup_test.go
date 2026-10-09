package main

import (
	"context"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"net/http"
	"net/http/httptest"
	"raptor-iap/infra-api/internal/approval"
	"raptor-iap/infra-api/internal/commands"
	"raptor-iap/infra-api/internal/domain"
	"raptor-iap/infra-api/internal/policy"
	"strings"
	"testing"
)

type proxyDependencies struct{ calls int }

func (f *proxyDependencies) Snapshot(context.Context, domain.Environment, string) ([]policy.ProxySnapshot, error) {
	return []policy.ProxySnapshot{{EnvCode: "dev", ProxyCode: "proxy", GroupID: "asg", ServerGroupID: "sgp", Desired: 2, Min: 1, Max: 4, Nodes: []policy.Node{{ID: "i-one", Protected: true, Healthy: true, Registered: true}, {ID: "i-two", Healthy: true, Registered: true}}}}, nil
}
func (f *proxyDependencies) Scale(context.Context, domain.Environment, string, int) (string, error) {
	f.calls++
	return "ack", nil
}
func (f *proxyDependencies) Protect(context.Context, domain.Environment, string, []string, bool) error {
	f.calls++
	return nil
}
func (f *proxyDependencies) Deregister(context.Context, domain.Environment, string, []string) error {
	f.calls++
	return nil
}
func (f *proxyDependencies) ConnectedClients(context.Context, policy.ProxySnapshot) ([]policy.ClientObservation, error) {
	return nil, &domain.CommandError{Code: "MetricsInvalid"}
}
func (f *proxyDependencies) Check(context.Context, approval.Binding) (bool, error) { return false, nil }
func (f *proxyDependencies) Claim(context.Context, approval.Binding) (bool, error) { return false, nil }
func (f *proxyDependencies) Record(context.Context, approval.Binding, string, string) error {
	return nil
}
func proxyScopes() map[string]domain.Environment {
	return map[string]domain.Environment{"dev": {Code: "dev", AccountID: "123", Region: "ap-southeast-1", ClusterID: "cluster", Proxies: []domain.ProxyMapping{{Code: "proxy", GroupID: "asg", ServerGroupID: "sgp", ListenerID: "listener", Port: 6432, TargetDBCode: "db"}}}}
}
func TestProxyStartupRefusesMissingDependencyOrMapping(t *testing.T) {
	for _, kind := range []string{"backend", "metrics", "approval", "claim", "fleet", "mapping", "cluster"} {
		t.Run(kind, func(t *testing.T) {
			f := &proxyDependencies{}
			deps := runtimeDependencies{Backend: f, Metrics: f, Approval: f, Claims: f, Fleet: f}
			scopes := proxyScopes()
			switch kind {
			case "backend":
				deps.Backend = nil
			case "metrics":
				deps.Metrics = nil
			case "approval":
				deps.Approval = nil
			case "claim":
				deps.Claims = nil
			case "fleet":
				deps.Fleet = nil
			case "mapping":
				env := scopes["dev"]
				env.Proxies = nil
				scopes["dev"] = env
			case "cluster":
				env := scopes["dev"]
				env.ClusterID = ""
				scopes["dev"] = env
			}
			if _, e := buildRuntimeHandler(&startupReader{}, scopes, "service", "fixture", "Authorization", runtimeOptions{Proxy: true}, deps); e == nil {
				t.Fatal("enabled incomplete runtime")
			}
		})
	}
}
func TestProxyStartupGatesScaleInAndDeregistrationBeforeProvider(t *testing.T) {
	cases := []struct {
		name, path, body string
		want             int
	}{{"scale out", "/v1/db-proxy-scale", `{"requestId":"r","actionId":"a","envCode":"dev","proxyCode":"proxy","groupId":"asg","desiredCapacity":3}`, 200}, {"approval missing", "/v1/db-proxy-scale", `{"requestId":"r","actionId":"a","envCode":"dev","proxyCode":"proxy","groupId":"asg","desiredCapacity":1}`, 409}, {"foreign target", "/v1/db-proxy-scale", `{"requestId":"r","actionId":"a","envCode":"dev","proxyCode":"proxy","groupId":"other","desiredCapacity":3}`, 409}, {"equal majority", "/v1/db-proxy-node-deregistration", `{"requestId":"r","envCode":"dev","proxyCode":"proxy","groupId":"asg","serverGroupId":"sgp","instanceIds":["i-two"]}`, 409}}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := &proxyDependencies{}
			h, e := buildRuntimeHandler(&startupReader{}, proxyScopes(), "service", "fixture", "Authorization", runtimeOptions{Proxy: true}, runtimeDependencies{Backend: f, Metrics: f, Approval: f, Claims: f, Fleet: f})
			if e != nil {
				t.Fatal(e)
			}
			r := httptest.NewRequest("POST", tc.path, strings.NewReader(tc.body))
			r.SetBasicAuth("service", "fixture")
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			wantCalls := 0
			if tc.want == 200 {
				wantCalls = 1
			}
			if w.Code != tc.want || f.calls != wantCalls {
				t.Fatalf("status=%d body=%s calls=%d", w.Code, w.Body.String(), f.calls)
			}
		})
	}
}

var _ commands.Backend = (*proxyDependencies)(nil)

func TestEnabledProxyCatalogAndMCPDispatch(t *testing.T) {
	f := &proxyDependencies{}
	h, err := buildRuntimeHandler(&startupReader{}, proxyScopes(), "service", "fixture", "Authorization", runtimeOptions{Proxy: true}, runtimeDependencies{Backend: f, Metrics: f, Approval: f, Claims: f, Fleet: f})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(h)
	defer server.Close()
	client := mcp.NewClient(&mcp.Implementation{Name: "proxy-startup", Version: "1"}, nil)
	session, err := client.Connect(context.Background(), &mcp.StreamableClientTransport{Endpoint: server.URL + "/mcp", HTTPClient: &http.Client{Transport: startupTransport{}}, DisableStandaloneSSE: true}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	catalog, err := session.ListTools(context.Background(), &mcp.ListToolsParams{})
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	for _, tool := range catalog.Tools {
		names[tool.Name] = true
	}
	if len(names) != 6 || !names["db_proxy_scale"] || !names["db_proxy_node_protection_set"] || !names["db_proxy_nodes_deregister"] || names["deployment_restart"] {
		t.Fatalf("unexpected catalog %v", names)
	}
	result, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "db_proxy_scale", Arguments: map[string]any{"requestId": "r", "actionId": "a", "envCode": "dev", "proxyCode": "proxy", "groupId": "asg", "desiredCapacity": 3}})
	if err != nil || result.IsError || f.calls != 1 {
		t.Fatalf("dispatch err=%v result=%v calls=%d", err, result, f.calls)
	}
	result, err = session.CallTool(context.Background(), &mcp.CallToolParams{Name: "db_proxy_scale", Arguments: map[string]any{"requestId": "r", "actionId": "b", "envCode": "dev", "proxyCode": "proxy", "groupId": "asg", "desiredCapacity": 1}})
	if err != nil || !result.IsError || f.calls != 1 {
		t.Fatalf("approval bypass err=%v result=%v calls=%d", err, result, f.calls)
	}
}

func (f *proxyDependencies) FleetClaim(_ context.Context, in approval.FleetIntent, token string) (approval.FleetResult, error) {
	return approval.FleetResult{Claimed: true, Token: token, Intent: in}, nil
}
func (f *proxyDependencies) FleetResolve(context.Context, approval.FleetIntent, string) error {
	return nil
}

func (f *proxyDependencies) FleetRecord(context.Context, approval.FleetIntent, string) error {
	return nil
}
