package backend

import (
	"context"
	"encoding/json"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/agentaccess"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/catalog"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/domain"
	"testing"
	"time"
)

func TestQualificationCredentialIsDefinitionBoundAndNeverAuthorizesScale(t *testing.T) {
	s, _, _ := accessFixture(t)
	ctx := context.Background()
	o, e := s.Catalog.CreateObject(ctx, catalog.CreateObjectInput{Kind: "db-proxy", Name: "Qualification proxy"})
	if e != nil {
		t.Fatal(e)
	}
	d := domain.RequestInput{Type: "agent", Object: domain.ObjectRef{Kind: "db-proxy", Code: o.Code}, EnvCode: "rdev.ali", Model: "gpt-5.6-luna", Operations: []domain.Operation{{Name: "db-proxy.replace-nodes", Parameters: map[string]any{"mode": "qualification"}}}}
	r, e := s.Requests.Create(ctx, domain.User{ID: domain.NewID()}, "qualification", d)
	if e != nil {
		t.Fatal(e)
	}
	h, _ := agentaccess.Fingerprint(r.Definition)
	scope := agentaccess.ReplacementScope{EnvCode: "rdev.ali", ProxyCode: o.Code, GroupID: "group", ServerGroupID: "backend", TargetDBCode: "db", Application: agentaccess.ApplicationScope{EnvCode: "rdev.ali", AppCode: "client", ClusterID: "cluster", Namespace: "test", Name: "traffic", UID: "uid"}}
	scope.AccountID = "123"
	scope.Region = "ap-southeast-1"
	scope.ListenerID = "listener"
	scope.Port = 5432
	scope.DatabaseResourceID = "rds"
	scope.ResolutionDigest = scope.Identity().Digest()
	a := agentaccess.Service{Pool: s.Requests.Pool, RaptorMCPURL: "https://raptor.fixture/mcp", InfraMCPURL: "https://infra.fixture/mcp", ResolveReplacement: func(context.Context, agentaccess.Binding) (agentaccess.ReplacementScope, error) { return scope, nil }}
	issued, e := a.Issue(ctx, r.ID, agentaccess.IssueInput{AttemptID: domain.NewID(), DefinitionSHA256: h, ExpiresAt: time.Now().Add(time.Minute)})
	if e != nil || issued.Binding.ExecutionMode != "qualification" {
		t.Fatal("issue", e)
	}
	scale := agentaccess.CheckInput{Credential: issued.Credential, Audience: "infra", Tool: "db_proxy_scale", Selectors: map[string]string{"requestId": r.ID, "envCode": "rdev.ali", "proxyCode": o.Code, "groupId": "group"}}
	if v, e := a.Check(ctx, scale); e != nil || v.Active {
		t.Fatal("qualification scale grant", e)
	}
	read := agentaccess.CheckInput{Credential: issued.Credential, Audience: "infra", Tool: "deployments_list", Selectors: map[string]string{"envCode": "rdev.ali", "kind": "db-proxy", "code": o.Code}}
	if v, e := a.Check(ctx, read); e != nil || !v.Active {
		t.Fatal("read denied", e)
	}
	// Binding corruption must not promote a qualification credential.
	if _, e = s.Requests.Pool.Exec(ctx, `UPDATE raptor.agent_attempt_access SET binding=jsonb_set(binding,'{executionMode}','"replacement"') WHERE request_id=$1`, r.ID); e != nil {
		t.Fatal(e)
	}
	if v, e := a.Check(ctx, read); e != nil || v.Active {
		t.Fatal("mode mismatch accepted", e)
	}
	// Even a definition changed to normal replacement invalidates the prior token.
	r.Definition.Operations[0].Parameters = map[string]any{"mode": "replacement"}
	raw, _ := json.Marshal(r.Definition)
	if _, e = s.Requests.Pool.Exec(ctx, "UPDATE raptor.requests SET definition=$2 WHERE id=$1", r.ID, raw); e != nil {
		t.Fatal(e)
	}
	if v, e := a.Check(ctx, scale); e != nil || v.Active {
		t.Fatal("request mode upgraded existing token", e)
	}
}
