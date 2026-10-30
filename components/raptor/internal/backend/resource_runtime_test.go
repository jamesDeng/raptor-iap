package backend

import (
	"context"
	"encoding/json"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/adapters"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/agentaccess"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/approvals"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/catalog"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/db"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/domain"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/testutil"
	"strings"
	"testing"
	"time"
)

type resourceInfraFixture struct {
	proof         adapters.ResolutionProof
	proofs, reads int
}

func (f *resourceInfraFixture) ResolutionProof(context.Context, string, string) (adapters.ResolutionProof, error) {
	f.proofs++
	return f.proof, nil
}
func (f *resourceInfraFixture) ListDeployments(_ context.Context, env domain.Environment, o domain.Object) ([]adapters.Deployment, error) {
	f.reads++
	return []adapters.Deployment{{EnvCode: env.Code, Kind: o.Kind, ObjectCode: o.Code, EvidenceMode: "live", ResourceID: "actual"}}, nil
}
func resourceRuntimeFixture(t *testing.T) (*Server, domain.Request, *resourceInfraFixture, string) {
	t.Helper()
	ctx := context.Background()
	p := testutil.Database(t)
	if e := db.Migrate(ctx, p); e != nil {
		t.Fatal(e)
	}
	h := New(p)
	h.Requests.ModelPolicy = replacementModelPolicy{}
	if e := h.Catalog.CreateGroup(ctx, "g", "Group"); e != nil {
		t.Fatal(e)
	}
	objs := map[string]domain.Object{}
	for _, kind := range []string{"db-proxy", "application", "database"} {
		o, e := h.Catalog.CreateObject(ctx, catalog.CreateObjectInput{Kind: kind, Name: kind})
		if e != nil {
			t.Fatal(e)
		}
		objs[kind] = o
	}
	proxy, app, database := objs["db-proxy"].Code, objs["application"].Code, objs["database"].Code
	if e := h.Catalog.SaveEnvironment(ctx, domain.Environment{Code: "dev", GroupCode: "g", Stage: "dev", Cloud: "aliyun", Region: "ap-southeast-1", AccountID: "123", ACKClusterID: "c", Config: map[string]any{"proxyApplications": map[string]any{proxy: []any{app}}}}, false); e != nil {
		t.Fatal(e)
	}
	h.Requests.ResolveSkills = func(context.Context, domain.SkillsVersion) (domain.SkillsVersion, error) {
		return domain.SkillsVersion{Tag: "skills-v1.0.0", CommitSHA: strings.Repeat("a", 40)}, nil
	}
	request, e := h.Requests.Create(ctx, domain.User{ID: domain.NewID()}, "resource", domain.RequestInput{Type: "agent", Object: domain.ObjectRef{Kind: "db-proxy", Code: proxy}, EnvCode: "dev", ProviderID: "codex", Model: "gpt-6-luna", Operations: []domain.Operation{{Name: "db-proxy.replace-nodes", Parameters: map[string]any{"mode": "replacement"}}}})
	if e != nil {
		t.Fatal(e)
	}
	i := adapters.ResolutionIdentity{EnvCode: "dev", AccountID: "123", Region: "ap-southeast-1", ClusterID: "c", ProxyCode: proxy, GroupID: "g", ServerGroupID: "sg", ListenerID: "l", Port: 5432, TargetDBCode: database, DatabaseResourceID: "rds", Application: adapters.ApplicationIdentity{EnvCode: "dev", AppCode: app, ClusterID: "c", Namespace: "n", Name: "app", UID: "u"}}
	f := &resourceInfraFixture{proof: adapters.ResolutionProof{Identity: i, ResolutionDigest: i.Digest()}}
	h.Catalog.Infra = f
	t.Setenv("AGENT_INTROSPECTION_USERNAME", "fixture-inspector")
	t.Setenv("AGENT_INTROSPECTION_PASSWORD", "fixture-inspector-password")
	t.Setenv("RAPTOR_AGENT_MCP_URL", "https://raptor.fixture/mcp")
	t.Setenv("INFRA_AGENT_MCP_URL", "https://infra.fixture/mcp")
	h.RegisterService("fixture-controller", "fixture-controller-password")
	hash, _ := agentaccess.Fingerprint(request.Definition)
	token := issued(t, h, request, hash, domain.NewID(), time.Now().Add(time.Minute))
	return h, request, f, token
}
func TestResourceRuntimeFreshProofOrderingContinuityAndLegacy(t *testing.T) {
	h, r, f, token := resourceRuntimeFixture(t)
	ctx := context.Background()
	original := f.proof
	selectors := map[string]string{"requestId": r.ID, "envCode": "dev", "proxyCode": r.Definition.Object.Code}
	f.proofs = 0
	selectors["proxyCode"] = "foreign"
	if active(t, introspection(h, token, "infra", "db_proxy_scale", selectors)) || f.proofs != 0 {
		t.Fatal("foreign selector reached proof", f.proofs)
	}
	selectors["proxyCode"] = r.Definition.Object.Code
	if !active(t, introspection(h, token, "infra", "db_proxy_scale", selectors)) || f.proofs != 1 {
		t.Fatal("code-only call denied or proof not fresh", f.proofs)
	}
	for _, field := range []string{"listener", "database", "uid"} {
		f.proof = original
		switch field {
		case "listener":
			f.proof.Identity.ListenerID = "new"
		case "database":
			f.proof.Identity.DatabaseResourceID = "new"
		case "uid":
			f.proof.Identity.Application.UID = "new"
		}
		f.proof.ResolutionDigest = f.proof.Identity.Digest()
		if active(t, introspection(h, token, "infra", "db_proxy_scale", selectors)) {
			t.Fatal("retarget authorized", field)
		}
		hash, _ := agentaccess.Fingerprint(r.Definition)
		if w := accessCall(h, "POST", "/v1/requests/"+r.ID+"/agent-access", "fixture-controller", "fixture-controller-password", agentaccess.IssueInput{AttemptID: domain.NewID(), DefinitionSHA256: hash, ExpiresAt: time.Now().Add(time.Minute)}); w.Code == 201 {
			t.Fatal("new attempt reset mapping", field)
		}
	}
	f.proof = original
	var saved []byte
	if e := h.Requests.Pool.QueryRow(ctx, "SELECT replacement_scope FROM raptor.agent_attempt_access WHERE request_id=$1", r.ID).Scan(&saved); e != nil {
		t.Fatal(e)
	}
	if _, e := h.Requests.Pool.Exec(ctx, "UPDATE raptor.agent_attempt_access SET replacement_scope=replacement_scope-'resolutionDigest' WHERE request_id=$1", r.ID); e != nil {
		t.Fatal(e)
	}
	f.proofs = 0
	if active(t, introspection(h, token, "infra", "db_proxy_scale", selectors)) || f.proofs != 0 {
		t.Fatal("legacy scope authorized new operation")
	}
	if _, e := h.Requests.Get(ctx, r.ID); e != nil {
		t.Fatal("legacy UI read lost", e)
	}
	hash, _ := agentaccess.Fingerprint(r.Definition)
	if w := accessCall(h, "POST", "/v1/requests/"+r.ID+"/agent-access", "fixture-controller", "fixture-controller-password", agentaccess.IssueInput{AttemptID: domain.NewID(), DefinitionSHA256: hash, ExpiresAt: time.Now().Add(time.Minute)}); w.Code == 201 {
		t.Fatal("legacy request implicitly upgraded")
	}
	if _, e := h.Requests.Pool.Exec(ctx, "UPDATE raptor.agent_attempt_access SET replacement_scope=$2 WHERE request_id=$1", r.ID, saved); e != nil {
		t.Fatal(e)
	}
	if _, e := h.Requests.Pool.Exec(ctx, "UPDATE raptor.agent_tokens SET revoked=true WHERE request_id=$1", r.ID); e != nil {
		t.Fatal(e)
	}
	f.proofs = 0
	if active(t, introspection(h, token, "infra", "db_proxy_scale", selectors)) || f.proofs != 0 {
		t.Fatal("revoked token reached proof")
	}
}
func TestResourceApprovalPrivatelyRechecksFrozenRealization(t *testing.T) {
	h, r, f, _ := resourceRuntimeFixture(t)
	ctx := context.Background()
	in := approvals.ApprovalInput{RequestID: r.ID, ActionID: domain.NewID(), Interface: "db-proxy.scale-in", EnvCode: "dev", Target: map[string]any{"proxyCode": r.Definition.Object.Code}, Parameters: map[string]any{"desiredCapacity": 2}}
	a, e := h.Approvals.Request(ctx, in)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = h.Approvals.Decide(ctx, domain.User{ID: domain.NewID(), Role: "user"}, a.ID, approvals.DecisionInput{Decision: "approve"}); e != nil {
		t.Fatal(e)
	}
	original := f.proof
	f.proof.Identity.ListenerID = "retarget"
	f.proof.ResolutionDigest = f.proof.Identity.Digest()
	if v, e := h.Approvals.Claim(ctx, in); e == nil || v.Claimed {
		t.Fatal("retarget consumed approval", v, e)
	}
	var n int
	if e = h.Requests.Pool.QueryRow(ctx, "SELECT count(*) FROM raptor.approval_submissions WHERE request_id=$1", r.ID).Scan(&n); e != nil || n != 0 {
		t.Fatal(n, e)
	}
	f.proof = original
	if v, e := h.Approvals.Claim(ctx, in); e != nil || !v.Claimed {
		t.Fatal(v, e)
	}
	if v, e := h.Approvals.Claim(ctx, in); e != nil || v.Claimed {
		t.Fatal("replay", v, e)
	}
	record := approvals.SubmissionRecordInput{Binding: in, Outcome: "submitted", ProviderRequestID: "ack"}
	if e = h.Approvals.RecordSubmission(ctx, record); e != nil {
		t.Fatal(e)
	}
	if e = h.Approvals.RecordSubmission(ctx, record); e != nil {
		t.Fatal(e)
	}
	record.ProviderRequestID = "other"
	if e = h.Approvals.RecordSubmission(ctx, record); e == nil {
		t.Fatal("immutable record rewritten")
	}
	legacy := in
	legacy.Interface = "ess.scale-in"
	legacy.Target = map[string]any{"proxyCode": r.Definition.Object.Code, "groupId": "g"}
	if v, e := h.Approvals.Claim(ctx, legacy); e == nil || v.Claimed {
		t.Fatal("historical interface authorized resource mutation")
	}
	// The persisted public approval contains no private digest/provider ID.
	raw, _ := json.Marshal(a)
	for _, secret := range []string{"resolutionDigest", "listenerId", "groupId"} {
		if strings.Contains(string(raw), secret) {
			t.Fatal("private approval target leaked", secret)
		}
	}
}

func TestResourceDeploymentRouteDeniesForeignBeforeDiscovery(t *testing.T) {
	h, r, f, _ := resourceRuntimeFixture(t)
	ctx := context.Background()
	path := "/v1/requests/" + r.ID + "/resource-deployments?envCode=dev&kind=database&code="
	f.reads = 0
	f.proofs = 0
	if w := accessCall(h, "GET", path+"foreign", "fixture-controller", "fixture-controller-password", nil); w.Code != 403 || f.reads != 0 || f.proofs != 0 {
		t.Fatal("foreign route reached discovery", w.Code, f.reads, f.proofs)
	}
	if w := accessCall(h, "GET", path+f.proof.Identity.TargetDBCode, "fixture-controller", "fixture-controller-password", nil); w.Code != 200 || f.reads != 1 || f.proofs != 1 {
		t.Fatal("bound dependency denied", w.Code, w.Body.String(), f.reads, f.proofs)
	}
	f.proof.Identity.Application.UID = "retarget"
	f.proof.ResolutionDigest = f.proof.Identity.Digest()
	f.reads = 0
	if w := accessCall(h, "GET", path+f.proof.Identity.TargetDBCode, "fixture-controller", "fixture-controller-password", nil); w.Code == 200 || f.reads != 0 {
		t.Fatal("stale request reached discovery", w.Code, f.reads)
	}
	// A conflicting canonical ACK identity must invalidate access before proof.
	if _, e := h.Requests.Pool.Exec(ctx, "UPDATE raptor.environments SET cluster_id='foreign' WHERE code='dev'"); e != nil {
		t.Fatal(e)
	}
	f.proofs = 0
	hash, _ := agentaccess.Fingerprint(r.Definition)
	if w := accessCall(h, "POST", "/v1/requests/"+r.ID+"/agent-access", "fixture-controller", "fixture-controller-password", agentaccess.IssueInput{AttemptID: domain.NewID(), DefinitionSHA256: hash, ExpiresAt: time.Now().Add(time.Minute)}); w.Code == 201 || f.proofs != 0 {
		t.Fatal("conflicting ACK identity reached proof", w.Code, f.proofs)
	}
}

func TestResourceForeignApprovalReadDeniedBeforeProof(t *testing.T) {
	h, r, f, token := resourceRuntimeFixture(t)
	f.proofs = 0
	if active(t, introspection(h, token, "raptor", "approval_get", map[string]string{"requestId": r.ID, "approvalId": domain.NewID()})) || f.proofs != 0 {
		t.Fatal("foreign approval ID reached proof", f.proofs)
	}
}
