package execution

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
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
	if err == nil || out.ReplacementScope != nil {
		t.Fatal("legacy scope unexpectedly authorized a new resource call")
	}
	for _, mode = range []string{"missing", "foreign"} {
		if _, err = client.Issue(context.Background(), b, b.DefinitionSHA256, expires); err == nil {
			t.Fatal("unsafe scope accepted", mode)
		}
	}
}

func TestIssuePreservesCompleteResourceProof(t *testing.T) {
	in := replacementInput(t)
	b, _, _ := ParseLiveOperation(in, NewID())
	b.ExecutionMode = "replacement"
	expires := time.Now().Add(time.Minute)
	app := map[string]any{"envCode": b.EnvCode, "appCode": "client", "clusterId": b.ClusterID, "namespace": "test", "name": "traffic", "uid": "uid"}
	scope := map[string]any{"envCode": b.EnvCode, "proxyCode": b.ObjectCode, "groupId": "group", "serverGroupId": "backend", "targetDbCode": "db", "application": app, "accountId": "123", "region": "ap-southeast-1", "listenerId": "listener", "port": 6432, "databaseResourceId": "rds"}
	// Canonical field order is the Infra resources.ResolutionIdentity contract.
	canonical := fmt.Sprintf(`{"envCode":%q,"accountId":"123","region":"ap-southeast-1","clusterId":%q,"proxyCode":%q,"groupId":"group","serverGroupId":"backend","listenerId":"listener","port":6432,"targetDbCode":"db","databaseResourceId":"rds","application":{"envCode":%q,"appCode":"client","clusterId":%q,"namespace":"test","name":"traffic","uid":"uid"}}`, b.EnvCode, b.ClusterID, b.ObjectCode, b.EnvCode, b.ClusterID)
	scope["resolutionDigest"] = fmt.Sprintf("%x", sha256.Sum256([]byte(canonical)))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(201)
		json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"credential": strings.Repeat("c", 43), "expiresAt": expires, "binding": b, "replacementScope": scope}})
	}))
	defer server.Close()
	client := HTTPRaptor{BaseURL: server.URL, Username: "service", Password: "fixture"}
	out, err := client.Issue(context.Background(), b, b.DefinitionSHA256, expires)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(out.ReplacementScope)
	var got map[string]any
	json.Unmarshal(raw, &got)
	for _, key := range []string{"accountId", "region", "listenerId", "port", "databaseResourceId", "resolutionDigest"} {
		if fmt.Sprint(got[key]) != fmt.Sprint(scope[key]) {
			t.Fatalf("proof stripped %s: %v", key, got)
		}
	}
	for _, key := range []string{"accountId", "region", "listenerId", "port", "databaseResourceId", "resolutionDigest"} {
		old := scope[key]
		delete(scope, key)
		if _, err := client.Issue(context.Background(), b, b.DefinitionSHA256, expires); err == nil {
			t.Fatalf("incomplete proof accepted: %s", key)
		}
		scope[key] = old
	}
	scope["listenerId"] = "retargeted"
	if _, err := client.Issue(context.Background(), b, b.DefinitionSHA256, expires); err == nil {
		t.Fatal("mismatched canonical digest accepted")
	}
}

func resourceTestScope(b AttemptBinding, listenerOverride ...string) ReplacementScope {
	listener := "listener"
	if len(listenerOverride) > 0 {
		listener = listenerOverride[0]
	}
	s := ReplacementScope{EnvCode: b.EnvCode, ProxyCode: b.ObjectCode, GroupID: "group", ServerGroupID: "backend", TargetDBCode: "db", AccountID: "123", Region: "ap-southeast-1", ListenerID: listener, Port: 6432, DatabaseResourceID: "rds", Application: ApplicationScope{EnvCode: b.EnvCode, AppCode: "client", ClusterID: b.ClusterID, Namespace: "test", Name: "client", UID: "uid"}}
	canonical := fmt.Sprintf(`{"envCode":%q,"accountId":"123","region":"ap-southeast-1","clusterId":%q,"proxyCode":%q,"groupId":"group","serverGroupId":"backend","listenerId":%q,"port":6432,"targetDbCode":"db","databaseResourceId":"rds","application":{"envCode":%q,"appCode":"client","clusterId":%q,"namespace":"test","name":"client","uid":"uid"}}`, b.EnvCode, b.ClusterID, b.ObjectCode, listener, b.EnvCode, b.ClusterID)
	s.ResolutionDigest = fmt.Sprintf("%x", sha256.Sum256([]byte(canonical)))
	return s
}

func resourceReplacementInput(t *testing.T) ExecutionInput {
	in := replacementInput(t)
	var d map[string]any
	json.Unmarshal(in.Definition, &d)
	d["operations"].([]any)[0].(map[string]any)["parameters"] = map[string]any{"mode": "replacement"}
	in.Definition, _ = json.Marshal(d)
	in.DefinitionSHA256 = fmt.Sprintf("%x", sha256.Sum256(in.Definition))
	return in
}
