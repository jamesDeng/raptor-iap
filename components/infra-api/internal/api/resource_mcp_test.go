package api

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"raptor-iap/infra-api/internal/resources"
	"reflect"
	"strings"
	"testing"
	"time"
)

func resourceRPC(h http.Handler, body, auth string) *httptest.ResponseRecorder {
	r := httptest.NewRequest("POST", "/mcp", strings.NewReader(body))
	r.Header.Set("X-Infra-Authorization", auth)
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Accept", "application/json, text/event-stream")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}
func TestResourceMCPAndHTTPUseSameLogicalCommands(t *testing.T) {
	for _, tc := range []struct{ tool, path, body string }{{"db_proxy_scale", "/v1/db-proxy-scale", `{"requestId":"r","actionId":"a","envCode":"dev","proxyCode":"p","desiredCapacity":4}`}, {"db_proxy_node_protection_set", "/v1/db-proxy-node-protection", `{"requestId":"r","envCode":"dev","proxyCode":"p","nodeIds":["i"],"protected":false}`}, {"db_proxy_nodes_deregister", "/v1/db-proxy-node-deregistration", `{"requestId":"r","envCode":"dev","proxyCode":"p","nodeIds":["i"]}`}, {"deployment_restart", "/v1/deployment-restart", `{"requestId":"r","envCode":"dev","appCode":"app","workloadKey":"wk"}`}} {
		i, p := replacementResourceIdentity()
		f := &resourceFixture{rows: resourceRows(), proof: p}
		cmd := &resourceCommander{}
		h := resourceCommandHandler(t, f, cmd, &i)
		w := resourceHTTP(h, "POST", tc.path, tc.body, agentCredential())
		if w.Code != 200 {
			t.Fatal(w.Code, w.Body.String())
		}
		target := cmd.target
		w = resourceRPC(h, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"`+tc.tool+`","arguments":`+tc.body+`}}`, agentCredential())
		if w.Code != 200 || strings.Contains(w.Body.String(), `"isError":true`) || cmd.calls != 2 || !reflect.DeepEqual(target, cmd.target) {
			t.Fatal(w.Code, w.Body.String(), cmd.calls, cmd.target)
		}
	}
}
func TestResourceMCPAndHTTPDeploymentEnvelopeParity(t *testing.T) {
	f := &resourceFixture{rows: resourceRows()}
	h := resourceHandler(t, f, nil)
	httpResponse := resourceHTTP(h, "GET", "/v1/deployments?envCode=dev&kind=application&code=app", "", valid())
	rpc := resourceRPC(h, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"deployments_list","arguments":{"envCode":"dev","kind":"application","code":"app"}}}`, valid())
	var httpBody any
	var rpcBody struct {
		Result struct {
			StructuredContent any `json:"structuredContent"`
		} `json:"result"`
	}
	if json.Unmarshal(httpResponse.Body.Bytes(), &httpBody) != nil || json.Unmarshal(rpc.Body.Bytes(), &rpcBody) != nil || !reflect.DeepEqual(httpBody, rpcBody.Result.StructuredContent) {
		t.Fatal(httpResponse.Body.String(), rpc.Body.String())
	}
}
func TestResourceMCPForeignTypedCallHasNoEmptyCheckOrProviderRead(t *testing.T) {
	i, p := replacementResourceIdentity()
	f := &resourceFixture{rows: resourceRows(), proof: p}
	checks := 0
	h, e := NewResourceServer(&observedReader{}, f, "user", "secret", "X-Infra-Authorization", &resourceCommander{}, allowCommands{}, func(_ context.Context, _ string, tool string, selectors map[string]string) (AgentIdentity, error) {
		checks++
		if tool != "db_proxy_scale" || selectors["proxyCode"] != "foreign" {
			t.Error(tool, selectors)
		}
		return i, nil
	})
	if e != nil {
		t.Fatal(e)
	}
	w := resourceRPC(h, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"db_proxy_scale","arguments":{"requestId":"r","actionId":"a","envCode":"dev","proxyCode":"foreign","desiredCapacity":4}}}`, agentCredential())
	if w.Code != 200 || !strings.Contains(w.Body.String(), "ScopeMismatch") || checks != 1 || f.calls != 0 || f.proofs != 0 {
		t.Fatal(w.Code, w.Body.String(), checks, f.calls, f.proofs)
	}
}

type resourceRoundTripper func(*http.Request) (*http.Response, error)

func (f resourceRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestResourceInspectorIsExplicitAndLegacyModeRemainsStrict(t *testing.T) {
	identity := resourceIdentity()
	calls := 0
	client := &http.Client{Transport: resourceRoundTripper(func(r *http.Request) (*http.Response, error) {
		calls++
		user, password, ok := r.BasicAuth()
		if !ok || user != "svc" || password != "pw" {
			t.Fatal("missing service auth")
		}
		var body []byte
		if r.URL.Path == "/v1/agent-access/prepare-infra" {
			body, _ = json.Marshal(map[string]any{"data": map[string]any{"eligible": true, "prepared": strings.Repeat("a", 64), "identity": identity}})
		} else if r.URL.Path == "/v1/agent-access/validate-infra" || r.URL.Path == "/v1/agent-access/introspect" {
			body, _ = json.Marshal(map[string]any{"data": identity})
		} else {
			t.Fatal(r.URL)
		}
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(bytes.NewReader(body))}, nil
	})}
	resolve := func(context.Context, string, string) (resources.Proof, error) {
		t.Fatal("question must not resolve proxy proof")
		return resources.Proof{}, nil
	}
	resourceCheck, e := NewResourceAgentInspector("https://raptor.test", "svc", "pw", client, resolve)
	if e != nil {
		t.Fatal(e)
	}
	legacyCheck, e := NewAgentInspector("https://raptor.test", "svc", "pw", client)
	if e != nil {
		t.Fatal(e)
	}
	selectors := map[string]string{"envCode": "dev", "appCode": "app", "workloadKey": "wk"}
	if _, e = resourceCheck(context.Background(), "raptor_at_"+strings.Repeat("a", 43), "deployment_status_get", selectors); e != nil {
		t.Fatal(e)
	}
	if _, e = legacyCheck(context.Background(), "raptor_at_"+strings.Repeat("a", 43), "deployment_status_get", selectors); e == nil {
		t.Fatal("legacy inspector accepted logical mode")
	}
	if calls != 3 {
		t.Fatal(calls)
	}
}

func TestResourceQualificationDiscoversReadsWithoutMutationTools(t *testing.T) {
	i, p := replacementResourceIdentity()
	i.Binding.ExecutionMode = "qualification"
	f := &resourceFixture{rows: resourceRows(), proof: p}
	reader := &resourceObserver{at: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)}
	checks := []string{}
	h, e := NewResourceServer(reader, f, "user", "secret", "X-Infra-Authorization", &resourceCommander{}, allowCommands{}, func(_ context.Context, _ string, tool string, _ map[string]string) (AgentIdentity, error) {
		checks = append(checks, tool)
		return i, nil
	})
	if e != nil {
		t.Fatal(e)
	}
	w := resourceRPC(h, `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`, agentCredential())
	if w.Code != 200 || strings.Contains(w.Body.String(), "db_proxy_scale") || strings.Contains(w.Body.String(), "deployment_restart") || !strings.Contains(w.Body.String(), "proxy_observation_get") {
		t.Fatal(w.Code, w.Body.String())
	}
	w = resourceRPC(h, `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"proxy_observation_get","arguments":{"requestId":"r","envCode":"dev","proxyCode":"p","kind":"fleet"}}}`, agentCredential())
	if w.Code != 200 || strings.Contains(w.Body.String(), `"isError":true`) || !strings.Contains(w.Body.String(), `"fleet"`) {
		t.Fatal(w.Code, w.Body.String())
	}
	if !reflect.DeepEqual(checks, []string{"", "proxy_observation_get"}) {
		t.Fatal(checks)
	}
}

func TestResourceToolsDescribeApprovalPolicy(t *testing.T) {
	identity, proof := replacementResourceIdentity()
	handler, err := NewResourceServer(&observedReader{}, &resourceFixture{rows: resourceRows(), proof: proof}, "user", "secret", "X-Infra-Authorization", &resourceCommander{}, allowCommands{}, func(_ context.Context, _ string, tool string, _ map[string]string) (AgentIdentity, error) {
		if tool != "" {
			t.Errorf("catalog discovery unexpectedly checked operation %s", tool)
		}
		return identity, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	response := resourceRPC(handler, `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`, agentCredential())
	if response.Code != http.StatusOK {
		t.Fatalf("list tools: %d %s", response.Code, response.Body.String())
	}
	var listing struct {
		Result struct {
			Tools []struct {
				Name        string                     `json:"name"`
				Description string                     `json:"description"`
				Meta        map[string]json.RawMessage `json:"_meta"`
			} `json:"tools"`
		} `json:"result"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &listing); err != nil {
		t.Fatal(err)
	}
	byName := map[string]json.RawMessage{}
	descriptions := map[string]string{}
	for _, tool := range listing.Result.Tools {
		byName[tool.Name] = tool.Meta["io.raptor-iap/approval-policy"]
		descriptions[tool.Name] = tool.Description
	}
	var got struct {
		Version  int `json:"version"`
		Variants []struct {
			Capability            string `json:"capability"`
			TargetKind            string `json:"targetKind"`
			Condition             string `json:"condition"`
			RequiresHumanApproval bool   `json:"requiresHumanApproval"`
		} `json:"variants"`
	}
	if err := json.Unmarshal(byName["db_proxy_scale"], &got); err != nil {
		t.Fatalf("scale approval policy missing: %v", err)
	}
	if got.Version != 1 || len(got.Variants) != 2 {
		t.Fatalf("unexpected scale approval policy: %+v", got)
	}
	out, in := got.Variants[0], got.Variants[1]
	if out.Capability != "db-proxy.scale-out" || out.TargetKind != "db-proxy" || out.Condition != "desired-greater-than-current" || out.RequiresHumanApproval ||
		in.Capability != "db-proxy.scale-in" || in.TargetKind != "db-proxy" || in.Condition != "desired-less-than-current" || !in.RequiresHumanApproval {
		t.Fatalf("scale approval does not distinguish directions: %+v", got)
	}
	description := descriptions["db_proxy_scale"]
	if !strings.Contains(description, "Scale in") || !strings.Contains(description, "requires human approval") || !strings.Contains(description, "Scale out") {
		t.Fatalf("operator cannot discover both directions from the tool description: %q", description)
	}
	if err := json.Unmarshal(byName["deployments_list"], &got); err != nil || len(got.Variants) != 1 || got.Variants[0].RequiresHumanApproval {
		t.Fatalf("read-only discovery approval policy: %s (%v)", byName["deployments_list"], err)
	}
}
