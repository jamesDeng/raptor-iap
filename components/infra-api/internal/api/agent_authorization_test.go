package api

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"net/http"
	"net/http/httptest"
	"raptor-iap/infra-api/internal/domain"
	"strings"
	"testing"
	"time"
)

func TestAgentBasicBoundaryEnforcesRequestScope(t *testing.T) {
	active := true
	expires := time.Now().Add(time.Minute)
	operation := "application.question"
	scope := AgentIdentity{Active: true, ExpiresAt: &expires, Binding: &AgentBinding{RequestID: "request", AttemptID: "attempt", Operation: operation, ObjectKind: "application", ObjectCode: "app", EnvCode: "dev", ClusterID: "cluster"}}
	check := func(_ context.Context, credential, tool string, selectors map[string]string) (AgentIdentity, error) {
		scope.Active = active
		scope.Binding.Operation = operation
		return scope, nil
	}
	cmd := &commandFixture{}
	h, err := NewWithAgentCommands(fakeReader{}, map[string]domain.Environment{"dev": {Code: "dev"}}, "user", "secret", "X-Infra-Authorization", cmd, allowCommands{}, check)
	if err != nil {
		t.Fatal(err)
	}
	invoke := func(path, body string) int {
		method := "POST"
		if body == "" {
			method = "GET"
		}
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("X-Infra-Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte("agent:raptor_at_"+strings.Repeat("a", 43))))
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w.Code
	}
	if got := invoke("/v1/cloud/identity?envCode=dev", ""); got != 200 {
		t.Fatal("bound read refused", got)
	}
	body := `{"requestId":"request","envCode":"dev","appCode":"app","clusterId":"cluster","namespace":"ns","name":"app","uid":"uid"}`
	if got := invoke("/v1/deployment-restart", body); got != 403 {
		t.Fatal("question reached mutation", got)
	}
	if cmd.calls != 0 {
		t.Fatal("unauthorized provider command")
	}
	active = false
	if got := invoke("/v1/cloud/identity?envCode=dev", ""); got != 401 {
		t.Fatal("revoked token accepted", got)
	}
	active = true
	expires = time.Now().Add(-time.Minute)
	if got := invoke("/v1/cloud/identity?envCode=dev", ""); got != 401 {
		t.Fatal("expired token accepted", got)
	}
}

func TestReplacementAgentRejectsForeignTargetsAtHTTPAndMCP(t *testing.T) {
	expires := time.Now().Add(time.Minute)
	active := true
	identity := AgentIdentity{Active: true, ExpiresAt: &expires, Binding: &AgentBinding{RequestID: "request", AttemptID: "attempt", Operation: "db-proxy.replace-nodes", ObjectKind: "db-proxy", ObjectCode: "proxy", EnvCode: "dev", ClusterID: "cluster"}, ReplacementScope: &AgentReplacementScope{EnvCode: "dev", ProxyCode: "proxy", GroupID: "group", ServerGroupID: "backend", TargetDBCode: "db", Application: AgentApplication{EnvCode: "dev", AppCode: "client", ClusterID: "cluster", Namespace: "ns", Name: "client", UID: "uid"}}}
	checks := 0
	check := func(_ context.Context, _, _ string, _ map[string]string) (AgentIdentity, error) {
		checks++
		identity.Active = active
		return identity, nil
	}
	cmd := &commandFixture{}
	h, err := NewWithAgentCommands(fakeReader{}, map[string]domain.Environment{"dev": {Code: "dev"}}, "user", "secret", "X-Infra-Authorization", cmd, allowCommands{}, check)
	if err != nil {
		t.Fatal(err)
	}
	bodies := []string{`{"requestId":"foreign","actionId":"scale","envCode":"dev","proxyCode":"proxy","groupId":"group","desiredCapacity":4}`, `{"requestId":"request","actionId":"scale","envCode":"dev","proxyCode":"foreign","groupId":"group","desiredCapacity":4}`, `{"requestId":"request","actionId":"scale","envCode":"dev","proxyCode":"proxy","groupId":"foreign","desiredCapacity":4}`}
	invoke := func(path, body string) int {
		r := httptest.NewRequest("POST", path, strings.NewReader(body))
		r.Header.Set("X-Infra-Authorization", agentHeader())
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Accept", "application/json, text/event-stream")
		r.Header.Set("MCP-Protocol-Version", "2025-06-18")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w.Code
	}
	for _, body := range bodies {
		if got := invoke("/v1/db-proxy-scale", body); got != 403 {
			t.Fatal("foreign HTTP command accepted", got)
		}
	}
	restart := `{"requestId":"request","envCode":"dev","appCode":"client","clusterId":"cluster","namespace":"ns","name":"client","uid":"foreign"}`
	if got := invoke("/v1/deployment-restart", restart); got != 403 {
		t.Fatal("foreign application UID accepted", got)
	}
	srv := httptest.NewServer(h)
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	client := mcp.NewClient(&mcp.Implementation{Name: "agent-test", Version: "1"}, nil)
	session, err := client.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: srv.URL + "/mcp", HTTPClient: &http.Client{Transport: agentBoundaryTransport{}}, DisableStandaloneSSE: true}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	for _, body := range bodies {
		result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "db_proxy_scale", Arguments: json.RawMessage(body)})
		if err != nil || !result.IsError {
			t.Fatal("foreign MCP command accepted", result, err)
		}
	}
	if cmd.calls != 0 {
		t.Fatal("foreign call reached provider")
	}
	allowed := `{"requestId":"request","actionId":"scale","envCode":"dev","proxyCode":"proxy","groupId":"group","desiredCapacity":4}`
	if got := invoke("/v1/db-proxy-scale", allowed); got != 200 {
		t.Fatal("bound command refused", got)
	}
	active = false
	if got := invoke("/mcp", `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`); got != 401 {
		t.Fatal("revoked catalog allowed", got)
	}
	if checks < 12 {
		t.Fatal("missing per-call authorization", checks)
	}
}
func agentHeader() string {
	return "Basic " + base64.StdEncoding.EncodeToString([]byte("agent:raptor_at_"+strings.Repeat("a", 43)))
}

type agentBoundaryTransport struct{}

func (agentBoundaryTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Header.Set("X-Infra-Authorization", agentHeader())
	return http.DefaultTransport.RoundTrip(r)
}

func TestPrivateInspectorRejectsRedirectMalformedAndOversizedReplies(t *testing.T) {
	expiry := time.Now().Add(time.Minute)
	identity := AgentIdentity{Active: true, ExpiresAt: &expiry, Binding: &AgentBinding{RequestID: "request", AttemptID: "attempt", Operation: "application.question", ObjectKind: "application", ObjectCode: "app", EnvCode: "dev", ClusterID: "cluster"}}
	var destinationCalls int
	destination := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { destinationCalls++; w.WriteHeader(200) }))
	defer destination.Close()
	for _, mode := range []string{"valid", "redirect", "malformed", "oversized", "inactive"} {
		t.Run(mode, func(t *testing.T) {
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				user, password, ok := r.BasicAuth()
				if !ok || user != "inspector" || password != "private-inspector" || r.URL.Path != "/v1/agent-access/introspect" {
					t.Error("private inspector boundary")
				}
				var input struct {
					Credential, Audience, Tool string
					Selectors                  map[string]string
				}
				if json.NewDecoder(r.Body).Decode(&input) != nil || input.Audience != "infra" || input.Tool != "cloud_identity_get" || input.Selectors["envCode"] != "dev" {
					t.Error("incorrect normalized inspection")
				}
				switch mode {
				case "redirect":
					http.Redirect(w, r, destination.URL, 307)
				case "malformed":
					w.Write([]byte("not JSON"))
				case "oversized":
					w.Write([]byte(strings.Repeat(" ", 16385)))
				default:
					out := identity
					if mode == "inactive" {
						out.Active = false
					}
					json.NewEncoder(w).Encode(map[string]any{"data": out})
				}
			}))
			defer server.Close()
			check, err := NewAgentInspector(server.URL, "inspector", "private-inspector", server.Client())
			if err != nil {
				t.Fatal(err)
			}
			got, err := check(context.Background(), "raptor_at_"+strings.Repeat("a", 43), "cloud_identity_get", map[string]string{"envCode": "dev"})
			if mode == "valid" {
				if err != nil || !got.Active {
					t.Fatal("valid inspection failed", err)
				}
			} else if err == nil {
				t.Fatal("invalid inspection accepted")
			}
		})
	}
	if destinationCalls != 0 {
		t.Fatal("inspection credential followed redirect")
	}
}

func TestAgentCannotInvokeDisabledHTTPCommand(t *testing.T) {
	expiry := time.Now().Add(time.Minute)
	identity := AgentIdentity{Active: true, ExpiresAt: &expiry, Binding: &AgentBinding{RequestID: "request", AttemptID: "attempt", Operation: "db-proxy.replace-nodes", ObjectKind: "db-proxy", ObjectCode: "proxy", EnvCode: "dev", ClusterID: "cluster"}, ReplacementScope: &AgentReplacementScope{EnvCode: "dev", ProxyCode: "proxy", GroupID: "group", ServerGroupID: "backend", TargetDBCode: "db", Application: AgentApplication{EnvCode: "dev", AppCode: "client", ClusterID: "cluster", Namespace: "ns", Name: "client", UID: "uid"}}}
	check := func(context.Context, string, string, map[string]string) (AgentIdentity, error) { return identity, nil }
	cmd := &commandFixture{}
	h, err := NewWithAgentCommands(fakeReader{}, map[string]domain.Environment{"dev": {Code: "dev"}}, "user", "secret", "X-Infra-Authorization", cmd, NewServiceAuthorizer("user"), check)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest("POST", "/v1/db-proxy-scale", strings.NewReader(`{"requestId":"request","actionId":"scale","envCode":"dev","proxyCode":"proxy","groupId":"group","desiredCapacity":4}`))
	req.Header.Set("X-Infra-Authorization", agentHeader())
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != 403 || cmd.calls != 0 {
		t.Fatal("disabled command reached provider", w.Code, cmd.calls)
	}
}
