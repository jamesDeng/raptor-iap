package openapi

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/backend"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/catalog"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/db"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/domain"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/testutil"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

type agentTransport struct{ token string }

func (t agentTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Header.Set("Authorization", "Bearer "+t.token)
	return http.DefaultTransport.RoundTrip(r)
}
func TestOpaqueMCPReadOnlyPrincipal(t *testing.T) {
	ctx := context.Background()
	p := testutil.Database(t)
	if e := db.Migrate(ctx, p); e != nil {
		t.Fatal(e)
	}
	t.Setenv("AGENT_INTROSPECTION_USERNAME", "inspector")
	t.Setenv("AGENT_INTROSPECTION_PASSWORD", "inspector-password")
	t.Setenv("RAPTOR_AGENT_MCP_URL", "https://raptor.fixture/mcp")
	t.Setenv("INFRA_AGENT_MCP_URL", "https://infra.fixture/mcp")
	b := backend.New(p)
	b.RegisterService("svc", "service-password")
	b.Catalog.CreateGroup(ctx, "g", "Group")
	b.Catalog.SaveEnvironment(ctx, domain.Environment{Code: "rdev.ali", GroupCode: "g", Stage: "dev", Config: map[string]any{"ackClusterId": "cluster", "region": "Singapore", "unreviewed": "private value"}}, false)
	o, e := b.Catalog.CreateObject(ctx, catalog.CreateObjectInput{Kind: "application", Name: "App"})
	if e != nil {
		t.Fatal(e)
	}
	b.Requests.ResolveSkills = func(context.Context, domain.SkillsVersion) (domain.SkillsVersion, error) {
		return domain.SkillsVersion{Tag: "skills-v1.0.0", CommitSHA: strings.Repeat("a", 40)}, nil
	}
	r, e := b.Requests.Create(ctx, domain.User{ID: domain.NewID()}, "mcp", domain.RequestInput{Type: "agent", Object: domain.ObjectRef{Kind: o.Kind, Code: o.Code}, EnvCode: "rdev.ali", Model: "gpt-5.6-luna", Operations: []domain.Operation{{Name: "application.question", Parameters: map[string]any{"question": "healthy?"}}}})
	if e != nil {
		t.Fatal(e)
	}
	raw, _ := json.Marshal(r.Definition)
	var canonical any
	json.Unmarshal(raw, &canonical)
	raw, _ = json.Marshal(canonical)
	sum := sha256.Sum256(raw)
	attempt := domain.NewID()
	issue, _ := json.Marshal(map[string]any{"attemptId": attempt, "definitionSha256": hex.EncodeToString(sum[:]), "expiresAt": time.Now().Add(5 * time.Minute)})
	req := httptest.NewRequest("POST", "/v1/requests/"+r.ID+"/agent-access", bytes.NewReader(issue))
	req.SetBasicAuth("svc", "service-password")
	w := httptest.NewRecorder()
	b.ServeHTTP(w, req)
	if w.Code != 201 {
		t.Fatal("issue failed")
	}
	var token struct {
		Data struct {
			Credential string `json:"credential"`
		}
	}
	json.Unmarshal(w.Body.Bytes(), &token)
	upstream := httptest.NewServer(b)
	defer upstream.Close()
	handler, e := NewHandler(Client{BaseURL: upstream.URL, Username: "svc", Password: "service-password"})
	if e != nil {
		t.Fatal(e)
	}
	server := httptest.NewServer(handler)
	defer server.Close()
	client := mcp.NewClient(&mcp.Implementation{Name: "fixture", Version: "1"}, nil)
	session, e := client.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: server.URL + "/mcp", HTTPClient: &http.Client{Transport: agentTransport{token.Data.Credential}}, DisableStandaloneSSE: true}, nil)
	if e != nil {
		t.Fatal("agent MCP connect failed", e)
	}
	defer session.Close()
	tools, e := session.ListTools(ctx, &mcp.ListToolsParams{})
	if e != nil || len(tools.Tools) != 3 {
		t.Fatal("agent catalog not restricted", e)
	}
	for _, tool := range tools.Tools {
		if tool.Name != "request_get" && tool.Name != "environment_get" && tool.Name != "object_get" {
			t.Fatal("mutation tool exposed")
		}
	}
	result, e := session.CallTool(ctx, &mcp.CallToolParams{Name: "request_get", Arguments: map[string]any{"requestId": r.ID}})
	if e != nil || result.IsError {
		t.Fatal("bound read failed", e)
	}
	output, _ := json.Marshal(result)
	if bytes.Contains(output, []byte("private value")) || bytes.Contains(output, []byte("unreviewed")) {
		t.Fatal("unreviewed config exposed")
	}
	r2, e := b.Requests.Create(ctx, domain.User{ID: domain.NewID()}, "second-principal", r.Definition)
	if e != nil {
		t.Fatal(e)
	}
	issue2, _ := json.Marshal(map[string]any{"attemptId": domain.NewID(), "definitionSha256": hex.EncodeToString(sum[:]), "expiresAt": time.Now().Add(5 * time.Minute)})
	req2 := httptest.NewRequest("POST", "/v1/requests/"+r2.ID+"/agent-access", bytes.NewReader(issue2))
	req2.SetBasicAuth("svc", "service-password")
	w2 := httptest.NewRecorder()
	b.ServeHTTP(w2, req2)
	if w2.Code != 201 {
		t.Fatal("second principal issuance failed")
	}
	var token2 struct {
		Data struct {
			Credential string `json:"credential"`
		}
	}
	json.Unmarshal(w2.Body.Bytes(), &token2)
	session2, e := client.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: server.URL + "/mcp", HTTPClient: &http.Client{Transport: agentTransport{token2.Data.Credential}}, DisableStandaloneSSE: true}, nil)
	if e != nil {
		t.Fatal("second principal connection failed")
	}
	defer session2.Close()
	var group sync.WaitGroup
	for i := 0; i < 4; i++ {
		for _, principal := range []struct {
			session    *mcp.ClientSession
			own, other string
		}{{session, r.ID, r2.ID}, {session2, r2.ID, r.ID}} {
			group.Add(1)
			go func(principal struct {
				session    *mcp.ClientSession
				own, other string
			}) {
				defer group.Done()
				answer, e := principal.session.CallTool(ctx, &mcp.CallToolParams{Name: "request_get", Arguments: map[string]any{"requestId": principal.own}})
				if e != nil || answer.IsError {
					t.Error("concurrent own-principal read failed")
					return
				}
				wire, _ := json.Marshal(answer)
				if !bytes.Contains(wire, []byte(principal.own)) || bytes.Contains(wire, []byte(principal.other)) {
					t.Error("concurrent principals contaminated context")
				}
			}(principal)
		}
	}
	group.Wait()
	for _, call := range []*mcp.CallToolParams{{Name: "request_get", Arguments: map[string]any{"requestId": domain.NewID()}}, {Name: "request_pause", Arguments: map[string]any{"requestId": r.ID, "reason": "waiting_review"}}} {
		result, e := session.CallTool(ctx, call)
		if e == nil && !result.IsError {
			t.Fatal("foreign or mutation call accepted")
		}
	}
	for _, path := range []string{"/v1/requests/" + r.ID + "/context", "/v1/agent-access/introspect"} {
		req, _ := http.NewRequest("GET", server.URL+path, nil)
		req.Header.Set("Authorization", "Bearer "+token.Data.Credential)
		resp, e := http.DefaultClient.Do(req)
		if e != nil {
			t.Fatal(e)
		}
		resp.Body.Close()
		if resp.StatusCode != 403 {
			t.Fatal("agent HTTP proxy escape", resp.StatusCode)
		}
	}
	if e := b.Requests.Action(ctx, r.ID, "cancel", ""); e != nil {
		t.Fatal(e)
	}
	result, e = session.CallTool(ctx, &mcp.CallToolParams{Name: "request_get", Arguments: map[string]any{"requestId": r.ID}})
	if e == nil && !result.IsError {
		t.Fatal("cancelled token still reads context")
	}
}

func TestAgentProtocolFailsClosedWithoutIntrospection(t *testing.T) {
	t.Setenv("AGENT_INTROSPECTION_USERNAME", "")
	t.Setenv("AGENT_INTROSPECTION_PASSWORD", "")
	h, e := NewHandler(Client{BaseURL: "http://127.0.0.1:1", Username: "fixture", Password: "fixture-password"})
	if e != nil {
		t.Fatal(e)
	}
	for _, method := range []string{"resources/read", "prompts/get", "tools/call"} {
		body := `{"jsonrpc":"2.0","id":1,"method":"` + method + `","params":{"name":"approval_request","arguments":{}}}`
		r := httptest.NewRequest("POST", "/mcp", strings.NewReader(body))
		r.Header.Set("Authorization", "Bearer raptor_at_"+strings.Repeat("a", 43))
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != 403 && w.Code != 503 {
			t.Fatal("unconfigured agent boundary accepted call", w.Code)
		}
	}
}

func TestPrivateIntrospectionCallerHasOnlyItsRoute(t *testing.T) {
	t.Setenv("AGENT_INTROSPECTION_USERNAME", "inspector")
	t.Setenv("AGENT_INTROSPECTION_PASSWORD", "fixture-inspector-password")
	calls := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u, p, ok := r.BasicAuth()
		if !ok || u != "inspector" || p != "fixture-inspector-password" || r.URL.Path != "/v1/agent-access/introspect" {
			t.Error("private proxy broadened credential or path")
		}
		calls++
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"data":{"active":false}}`))
	}))
	defer upstream.Close()
	h, e := NewHandler(Client{BaseURL: upstream.URL, Username: "controller", Password: "fixture-controller-password"})
	if e != nil {
		t.Fatal(e)
	}
	r := httptest.NewRequest("POST", "/v1/agent-access/introspect", strings.NewReader(`{"credential":"synthetic","audience":"infra"}`))
	r.SetBasicAuth("inspector", "fixture-inspector-password")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 200 || calls != 1 {
		t.Fatal("private introspection caller cannot reach its route", w.Code)
	}
	r = httptest.NewRequest("GET", "/v1/environments/rdev.ali", nil)
	r.SetBasicAuth("inspector", "fixture-inspector-password")
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 401 || calls != 1 {
		t.Fatal("inspector reached broader service route")
	}
}
