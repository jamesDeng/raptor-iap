package openapi

import (
	"context"
	"encoding/json"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/agentaccess"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/domain"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestResourceScopedMCPReturnsSelectedDependenciesOnly(t *testing.T) {
	def := domain.RequestInput{Type: "agent", Object: domain.ObjectRef{Kind: "db-proxy", Code: "p"}, EnvCode: "dev", Model: "gpt-6-luna", Operations: []domain.Operation{{Name: "db-proxy.replace-nodes", Parameters: map[string]any{"mode": "replacement"}}}}
	hash, _ := agentaccess.Fingerprint(def)
	b := agentaccess.Binding{RequestID: "r", AttemptID: "a", Operation: "db-proxy.replace-nodes", ObjectKind: "db-proxy", ObjectCode: "p", EnvCode: "dev", ClusterID: "c", DefinitionSHA256: hash, ExecutionMode: "replacement"}
	scope := agentaccess.ReplacementScope{EnvCode: "dev", ProxyCode: "p", GroupID: "g", ServerGroupID: "sg", TargetDBCode: "db", Application: agentaccess.ApplicationScope{EnvCode: "dev", AppCode: "app", ClusterID: "c", Namespace: "n", Name: "app", UID: "u"}, AccountID: "123", Region: "ap-southeast-1", ListenerID: "l", Port: 5432, DatabaseResourceID: "rds"}
	scope.ResolutionDigest = scope.Identity().Digest()
	backendReads := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var data any
		if r.URL.Path == "/v1/agent-access/introspect" {
			var in agentaccess.CheckInput
			if json.NewDecoder(r.Body).Decode(&in) != nil {
				t.Error("invalid introspection")
			}
			expiry := time.Now().Add(time.Minute)
			data = agentaccess.CheckResult{Active: agentaccess.ReplacementSelectorsAllowed(b, scope, in.Audience, in.Tool, in.Selectors), Binding: &b, ExpiresAt: &expiry, ReplacementScope: &scope}
		} else {
			backendReads++
			switch r.URL.Path {
			case "/v1/requests/r/context":
				data = map[string]any{"requestId": "r", "definition": def, "environment": map[string]any{"code": "dev"}}
			case "/v1/objects/application/app":
				data = map[string]any{"kind": "application", "code": "app", "name": "Client"}
			case "/v1/requests/r/resource-deployments":
				q := r.URL.Query()
				if len(q) != 3 || q.Get("envCode") != "dev" || q.Get("kind") != "database" || q.Get("code") != "db" {
					t.Error("wrong logical discovery", q)
				}
				data = map[string]any{"resource": map[string]string{"kind": "database", "code": "db"}, "envCode": "dev", "deployments": []any{}}
			default:
				t.Error("foreign backend resource", r.URL.Path)
				http.Error(w, "foreign", 403)
				return
			}
		}
		json.NewEncoder(w).Encode(map[string]any{"data": data})
	}))
	defer upstream.Close()
	c := Client{BaseURL: upstream.URL, Username: "svc", Password: "fixture"}
	server := httptest.NewServer(agentHandler(c, c, "raptor_at_"+strings.Repeat("x", 43)))
	defer server.Close()
	client := mcp.NewClient(&mcp.Implementation{Name: "resource", Version: "1"}, nil)
	session, e := client.Connect(context.Background(), &mcp.StreamableClientTransport{Endpoint: server.URL + "/mcp", DisableStandaloneSSE: true}, nil)
	if e != nil {
		t.Fatal(e)
	}
	defer session.Close()
	for _, call := range []struct {
		name     string
		args     map[string]any
		contains []string
	}{
		{"request_get", map[string]any{"requestId": "r"}, []string{"relatedResources", "app", "db"}},
		{"object_get", map[string]any{"requestId": "r", "kind": "application", "code": "app"}, []string{"Client", "app"}},
		{"deployments_list", map[string]any{"requestId": "r", "envCode": "dev", "kind": "database", "code": "db"}, []string{"database", "db"}},
	} {
		result, e := session.CallTool(context.Background(), &mcp.CallToolParams{Name: call.name, Arguments: call.args})
		if e != nil || result.IsError {
			t.Fatal(call.name, result, e)
		}
		raw, _ := json.Marshal(result)
		for _, want := range call.contains {
			if !strings.Contains(string(raw), want) {
				t.Fatal("missing selected resource", call.name, want, string(raw))
			}
		}
		for _, private := range []string{"resolutionDigest", "groupId", "listenerId", "databaseResourceId"} {
			if strings.Contains(string(raw), private) {
				t.Fatal("private proof leaked", private)
			}
		}
	}
	before := backendReads
	result, e := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "deployments_list", Arguments: map[string]any{"requestId": "r", "envCode": "dev", "kind": "database", "code": "foreign"}})
	if e == nil && !result.IsError {
		t.Fatal("foreign catalog selection allowed")
	}
	if backendReads != before {
		t.Fatal("foreign selection reached backend")
	}
}
