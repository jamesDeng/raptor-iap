package main

import (
	"context"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"net/http"
	"net/http/httptest"
	"raptor-iap/infra-api/internal/domain"
	"strings"
	"testing"
)

type startupReader struct{ restarts int }

func (*startupReader) Identity(context.Context, domain.Environment) (string, error) {
	return "123", nil
}
func (*startupReader) Deployments(context.Context, domain.Environment, string, string) ([]domain.Deployment, error) {
	return nil, nil
}
func (*startupReader) Status(context.Context, domain.Environment, domain.Target) (domain.Status, error) {
	return domain.Status{}, nil
}
func (f *startupReader) Restart(_ context.Context, env domain.Environment, c domain.RestartCommand) (domain.RestartReceipt, error) {
	f.restarts++
	if env.Code != "dev" || c.UID != "uid" || c.RequestID != "request" {
		panic("wrong restart selectors")
	}
	return domain.RestartReceipt{Accepted: true, Target: c, EvidenceMode: "simulated"}, nil
}
func TestBasicAuthRestartWiringAndDefaultOff(t *testing.T) {
	body := `{"requestId":"request","envCode":"dev","appCode":"app","clusterId":"cluster","namespace":"ns","name":"app","uid":"uid"}`
	for _, tc := range []struct {
		enabled, authenticated bool
		want                   int
	}{{false, true, 403}, {true, false, 401}, {true, true, 200}} {
		f := &startupReader{}
		h, e := buildHandler(f, map[string]domain.Environment{"dev": {Code: "dev", AccountID: "123", ClusterID: "cluster"}}, "service", "fixture", "Authorization", tc.enabled)
		if e != nil {
			t.Fatal(e)
		}
		r := httptest.NewRequest("POST", "/v1/deployment-restart", strings.NewReader(body))
		if tc.authenticated {
			r.SetBasicAuth("service", "fixture")
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		expectedCalls := 0
		if tc.want == 200 {
			expectedCalls = 1
		}
		if w.Code != tc.want || f.restarts != expectedCalls {
			t.Fatalf("enabled=%t authenticated=%t: %d calls=%d", tc.enabled, tc.authenticated, w.Code, f.restarts)
		}
	}
}
func TestRestartModeDoesNotEnableProxyMutations(t *testing.T) {
	f := &startupReader{}
	h, e := buildHandler(f, map[string]domain.Environment{"dev": {Code: "dev"}}, "service", "fixture", "Authorization", true)
	if e != nil {
		t.Fatal(e)
	}
	r := httptest.NewRequest("POST", "/v1/db-proxy-scale", strings.NewReader(`{"requestId":"r","actionId":"a","envCode":"dev","proxyCode":"p","groupId":"g","desiredCapacity":1}`))
	r.SetBasicAuth("service", "fixture")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 403 || f.restarts != 0 {
		t.Fatal(w.Code, f.restarts)
	}
}

// Removing operation filtering would expose unwired proxy mutations to Pi.
func TestRestartStartupExposesOnlyRestartMutation(t *testing.T) {
	f := &startupReader{}
	h, e := buildHandler(f, map[string]domain.Environment{"dev": {Code: "dev"}}, "service", "fixture", "Authorization", true)
	if e != nil {
		t.Fatal(e)
	}
	server := httptest.NewServer(h)
	defer server.Close()
	client := mcp.NewClient(&mcp.Implementation{Name: "startup-test", Version: "1"}, nil)
	session, e := client.Connect(context.Background(), &mcp.StreamableClientTransport{Endpoint: server.URL + "/mcp", HTTPClient: &http.Client{Transport: startupTransport{}}, DisableStandaloneSSE: true}, nil)
	if e != nil {
		t.Fatal(e)
	}
	defer session.Close()
	tools, e := session.ListTools(context.Background(), &mcp.ListToolsParams{})
	if e != nil {
		t.Fatal(e)
	}
	if len(tools.Tools) != 4 {
		t.Fatalf("catalog=%v", tools)
	}
	found := false
	for _, tool := range tools.Tools {
		if tool.Name == "deployment_restart" {
			found = true
		}
		if strings.HasPrefix(tool.Name, "db_proxy") {
			t.Fatal("unwired proxy tool exposed")
		}
	}
	if !found {
		t.Fatal("restart tool missing")
	}
}

type startupTransport struct{}

func (startupTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.SetBasicAuth("service", "fixture")
	return http.DefaultTransport.RoundTrip(r)
}
