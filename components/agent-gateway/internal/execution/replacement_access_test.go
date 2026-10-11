package execution

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestIssueReplacementRequiresResolvedScope(t *testing.T) {
	in := replacementInput(t)
	b, _, err := ParseLiveOperation(in, NewID())
	if err != nil {
		t.Fatal(err)
	}
	scope := ReplacementScope{EnvCode: b.EnvCode, ProxyCode: b.ObjectCode, GroupID: "group", ServerGroupID: "backend", TargetDBCode: "db", Application: ApplicationScope{EnvCode: b.EnvCode, AppCode: "client", ClusterID: b.ClusterID, Namespace: "test", Name: "traffic", UID: "uid"}}
	mode := "valid"
	expires := time.Now().Add(time.Minute)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var current *ReplacementScope
		copy := scope
		if mode != "missing" {
			current = &copy
		}
		if mode == "foreign" {
			copy.Application.EnvCode = "prod"
		}
		w.WriteHeader(201)
		json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"credential": strings.Repeat("c", 43), "expiresAt": expires, "binding": b, "raptorMcpUrl": "https://raptor.fixture/mcp", "infraMcpUrl": "https://infra.fixture/mcp", "replacementScope": current}})
	}))
	defer server.Close()
	client := HTTPRaptor{BaseURL: server.URL, Username: "service", Password: "fixture"}
	out, err := client.Issue(context.Background(), b, b.DefinitionSHA256, expires)
	if err != nil || out.ReplacementScope == nil || *out.ReplacementScope != scope {
		t.Fatal("valid scope rejected", err)
	}
	for _, mode = range []string{"missing", "foreign"} {
		if _, err = client.Issue(context.Background(), b, b.DefinitionSHA256, expires); err == nil {
			t.Fatal("unsafe scope accepted", mode)
		}
	}
}
