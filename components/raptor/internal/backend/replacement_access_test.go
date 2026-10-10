package backend

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/jamesDeng/raptor-iap/components/raptor/internal/agentaccess"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/catalog"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/domain"
)

func TestReplacementAccessRequiresAuthoritativeScope(t *testing.T) {
	s, _, _ := accessFixture(t)
	ctx := context.Background()
	proxy, err := s.Catalog.CreateObject(ctx, catalog.CreateObjectInput{Kind: "db-proxy", Name: "Proxy"})
	if err != nil {
		t.Fatal(err)
	}
	request, err := s.Requests.Create(ctx, domain.User{ID: domain.NewID()}, "replace", domain.RequestInput{Type: "agent", Object: domain.ObjectRef{Kind: "db-proxy", Code: proxy.Code}, EnvCode: "rdev.ali", Model: "gpt-5.6-luna", Operations: []domain.Operation{{Name: "db-proxy.replace-nodes", Parameters: map[string]any{}}}})
	if err != nil {
		t.Fatal(err)
	}
	fingerprint, err := agentaccess.Fingerprint(request.Definition)
	if err != nil {
		t.Fatal(err)
	}
	service := agentaccess.Service{Pool: s.Requests.Pool, RaptorMCPURL: "https://raptor.fixture/mcp", InfraMCPURL: "https://infra.fixture/mcp"}
	input := agentaccess.IssueInput{AttemptID: domain.NewID(), DefinitionSHA256: fingerprint, ExpiresAt: time.Now().Add(time.Minute)}
	if _, err = service.Issue(ctx, request.ID, input); err == nil {
		t.Fatal("replacement enabled without resolver")
	}
	scope := agentaccess.ReplacementScope{EnvCode: "rdev.ali", ProxyCode: proxy.Code, GroupID: "group", ServerGroupID: "backend", TargetDBCode: "db", Application: agentaccess.ApplicationScope{EnvCode: "rdev.ali", AppCode: "client", ClusterID: "cluster", Namespace: "test", Name: "traffic", UID: "uid"}}
	service.ResolveReplacement = func(context.Context, agentaccess.Binding) (agentaccess.ReplacementScope, error) { return scope, nil }
	out, err := service.Issue(ctx, request.ID, input)
	if err != nil || out.ReplacementScope == nil || *out.ReplacementScope != scope || out.Binding.Operation != "db-proxy.replace-nodes" {
		t.Fatal("bound issuance failed", err)
	}
	check := agentaccess.CheckInput{Credential: out.Credential, Audience: "infra", Tool: "db_proxy_scale", Selectors: map[string]string{"requestId": request.ID, "envCode": "rdev.ali", "proxyCode": proxy.Code, "groupId": "group"}}
	got, err := service.Check(ctx, check)
	if err != nil || !got.Active {
		t.Fatal("exact proxy denied", err)
	}
	check.Selectors["groupId"] = "foreign"
	got, err = service.Check(ctx, check)
	if err != nil || got.Active {
		t.Fatal("foreign proxy authorized", err)
	}
	check.Selectors["groupId"] = "group"
	scope.Application.UID = "changed"
	got, err = service.Check(ctx, check)
	if err != nil || got.Active {
		t.Fatal("changed authoritative scope remained active", err)
	}
	scope.Application.UID = "uid"
	if err = service.Revoke(ctx, request.ID, input.AttemptID); err != nil {
		t.Fatal(err)
	}
	got, err = service.Check(ctx, check)
	if err != nil || got.Active {
		t.Fatal("revoked replacement credential accepted", err)
	}
	input.AttemptID = domain.NewID()
	out, err = service.Issue(ctx, request.ID, input)
	if err != nil {
		t.Fatal(err)
	}
	check.Credential = out.Credential
	if _, err = s.Requests.Pool.Exec(ctx, "UPDATE raptor.agent_tokens SET expires_at=now()-interval '1 second' WHERE request_id=$1", request.ID); err != nil {
		t.Fatal(err)
	}
	got, err = service.Check(ctx, check)
	if err != nil || got.Active {
		t.Fatal("expired replacement credential accepted", err)
	}
	input.AttemptID = domain.NewID()
	out, err = service.Issue(ctx, request.ID, input)
	if err != nil {
		t.Fatal(err)
	}
	check.Credential = out.Credential
	if err = s.Requests.Action(ctx, request.ID, "cancel", ""); err != nil {
		t.Fatal(err)
	}
	got, err = service.Check(ctx, check)
	if err != nil || got.Active {
		t.Fatal("cancelled replacement credential accepted", err)
	}
	// Persisted scope must not contain the raw token.
	var raw []byte
	if err = s.Requests.Pool.QueryRow(ctx, "SELECT replacement_scope FROM raptor.agent_attempt_access WHERE request_id=$1 AND attempt_id=$2", request.ID, input.AttemptID).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	var saved agentaccess.ReplacementScope
	if json.Unmarshal(raw, &saved) != nil || saved.Application.UID != "uid" {
		t.Fatal("scope not retained")
	}
}
