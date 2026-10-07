package backend

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/catalog"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/db"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/domain"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/testutil"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func accessFixture(t *testing.T) (*Server, domain.Request, string) {
	t.Helper()
	ctx := context.Background()
	p := testutil.Database(t)
	if e := db.Migrate(ctx, p); e != nil {
		t.Fatal(e)
	}
	t.Setenv("AGENT_INTROSPECTION_USERNAME", "fixture-inspector")
	t.Setenv("AGENT_INTROSPECTION_PASSWORD", "fixture-inspector-password")
	t.Setenv("RAPTOR_AGENT_MCP_URL", "https://raptor.fixture/mcp")
	t.Setenv("INFRA_AGENT_MCP_URL", "https://infra.fixture/mcp")
	s := New(p)
	s.RegisterService("fixture-controller", "fixture-controller-password")
	s.Catalog.CreateGroup(ctx, "raptor", "Raptor")
	s.Catalog.SaveEnvironment(ctx, domain.Environment{Code: "rdev.ali", GroupCode: "raptor", Stage: "dev", Config: map[string]any{"ackClusterId": "cluster", "region": "Singapore", "unreviewedConfig": "do not expose"}}, false)
	o, e := s.Catalog.CreateObject(ctx, catalog.CreateObjectInput{Kind: "application", Name: "Fixture app"})
	if e != nil {
		t.Fatal(e)
	}
	s.Requests.ResolveSkills = func(context.Context, domain.SkillsVersion) (domain.SkillsVersion, error) {
		return domain.SkillsVersion{Tag: "skills-v1.0.0", CommitSHA: strings.Repeat("a", 40)}, nil
	}
	r, e := s.Requests.Create(ctx, domain.User{ID: domain.NewID()}, "access", domain.RequestInput{Type: "agent", Object: domain.ObjectRef{Kind: "application", Code: o.Code}, EnvCode: "rdev.ali", Model: "gpt-5.6-luna", Operations: []domain.Operation{{Name: "application.question", Parameters: map[string]any{"question": "healthy?"}}}})
	if e != nil {
		t.Fatal(e)
	}
	b, _ := json.Marshal(r.Definition)
	var def map[string]any
	json.Unmarshal(b, &def)
	canonical, _ := json.Marshal(def)
	sum := sha256.Sum256(canonical)
	return s, r, hex.EncodeToString(sum[:])
}
func accessCall(s *Server, method, path, user, password string, body any) *httptest.ResponseRecorder {
	b, _ := json.Marshal(body)
	r := httptest.NewRequest(method, path, bytes.NewReader(b))
	r.SetBasicAuth(user, password)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	return w
}
func issued(t *testing.T, s *Server, r domain.Request, fingerprint, attempt string, expiry time.Time) string {
	t.Helper()
	w := accessCall(s, "POST", "/v1/requests/"+r.ID+"/agent-access", "fixture-controller", "fixture-controller-password", map[string]any{"attemptId": attempt, "definitionSha256": fingerprint, "expiresAt": expiry})
	if w.Code != 201 {
		t.Fatalf("issue status %d", w.Code)
	}
	var envelope struct {
		Data struct {
			Credential string `json:"credential"`
		}
	}
	if json.Unmarshal(w.Body.Bytes(), &envelope) != nil || len(envelope.Data.Credential) < 43 {
		t.Fatal("missing opaque credential")
	}
	return envelope.Data.Credential
}
func introspection(s *Server, token, audience, tool string, selectors map[string]string) *httptest.ResponseRecorder {
	return accessCall(s, "POST", "/v1/agent-access/introspect", "fixture-inspector", "fixture-inspector-password", map[string]any{"credential": token, "audience": audience, "tool": tool, "selectors": selectors})
}
func active(t *testing.T, w *httptest.ResponseRecorder) bool {
	t.Helper()
	if w.Code != 200 {
		t.Fatalf("introspection status %d", w.Code)
	}
	var v struct {
		Data struct {
			Active bool `json:"active"`
		}
	}
	if json.Unmarshal(w.Body.Bytes(), &v) != nil {
		t.Fatal("invalid introspection")
	}
	return v.Data.Active
}
func TestOpaqueAttemptScopeAndRevocation(t *testing.T) {
	s, r, hash := accessFixture(t)
	attempt := domain.NewID()
	token := issued(t, s, r, hash, attempt, time.Now().UTC().Add(5*time.Minute))
	if !active(t, introspection(s, token, "raptor", "request_get", map[string]string{"requestId": r.ID})) {
		t.Fatal("bound context denied")
	}
	for _, tc := range []struct {
		audience, tool string
		selectors      map[string]string
	}{{"raptor", "request_get", map[string]string{"requestId": domain.NewID()}}, {"raptor", "environment_get", map[string]string{"requestId": r.ID, "envCode": "other"}}, {"raptor", "object_get", map[string]string{"requestId": r.ID, "kind": "application", "code": "other"}}, {"raptor", "approval_request", nil}, {"other", "request_get", nil}, {"infra", "deployment_status_get", map[string]string{"envCode": r.Definition.EnvCode, "appCode": r.Definition.Object.Code, "clusterId": "other", "namespace": "ns", "name": "app", "uid": "uid"}}} {
		if active(t, introspection(s, token, tc.audience, tc.tool, tc.selectors)) {
			t.Fatal("foreign scope accepted")
		}
	}
	if !active(t, introspection(s, token, "infra", "deployments_list", map[string]string{"envCode": r.Definition.EnvCode, "kind": "application", "code": r.Definition.Object.Code})) {
		t.Fatal("bound infra discovery denied")
	}
	var leaked int
	if e := s.Requests.Pool.QueryRow(context.Background(), "SELECT count(*) FROM raptor.agent_tokens WHERE token_hash=$1", token).Scan(&leaked); e != nil || leaked != 0 {
		t.Fatal("raw token persisted or token journal unavailable")
	}
	w := accessCall(s, "DELETE", "/v1/requests/"+r.ID+"/agent-access/"+attempt, "fixture-controller", "fixture-controller-password", nil)
	if w.Code != 200 {
		t.Fatal("revoke failed")
	}
	if active(t, introspection(s, token, "raptor", "request_get", map[string]string{"requestId": r.ID})) {
		t.Fatal("revoked token accepted")
	}
	w = accessCall(s, "POST", "/v1/requests/"+r.ID+"/agent-access", "fixture-controller", "fixture-controller-password", map[string]any{"attemptId": attempt, "definitionSha256": hash, "expiresAt": time.Now().UTC().Add(time.Minute)})
	if w.Code != 409 {
		t.Fatal("revoked attempt resurrected")
	}
}
func TestOpaqueIssuanceFingerprintExpiryAndPrivateCaller(t *testing.T) {
	s, r, hash := accessFixture(t)
	for _, tc := range []struct {
		hash   string
		expiry time.Time
	}{{"wrong", time.Now().Add(time.Minute)}, {hash, time.Now().Add(-time.Minute)}, {hash, time.Now().Add(11 * time.Minute)}} {
		w := accessCall(s, "POST", "/v1/requests/"+r.ID+"/agent-access", "fixture-controller", "fixture-controller-password", map[string]any{"attemptId": domain.NewID(), "definitionSha256": tc.hash, "expiresAt": tc.expiry})
		if w.Code != 400 {
			t.Fatal("invalid issuance accepted", w.Code)
		}
	}
	token := issued(t, s, r, hash, domain.NewID(), time.Now().UTC().Add(time.Minute))
	w := accessCall(s, "POST", "/v1/agent-access/introspect", "fixture-controller", "fixture-controller-password", map[string]any{"credential": token, "audience": "raptor"})
	if w.Code != 401 {
		t.Fatal("controller used private introspection credential")
	}
	if e := s.Requests.Action(context.Background(), r.ID, "cancel", ""); e != nil {
		t.Fatal(e)
	}
	if active(t, introspection(s, token, "raptor", "request_get", map[string]string{"requestId": r.ID})) {
		t.Fatal("cancel did not revoke access")
	}
}

func TestOpaqueRotationExpiryAndRevocationTombstone(t *testing.T) {
	s, r, hash := accessFixture(t)
	ctx := context.Background()
	attempt := domain.NewID()
	expiry := time.Now().UTC().Add(5 * time.Minute)
	first := issued(t, s, r, hash, attempt, expiry)
	second := issued(t, s, r, hash, attempt, expiry)
	if first == second {
		t.Fatal("rotation reused credential")
	}
	if active(t, introspection(s, first, "raptor", "request_get", map[string]string{"requestId": r.ID})) {
		t.Fatal("old credential survived rotation")
	}
	if !active(t, introspection(s, second, "raptor", "request_get", map[string]string{"requestId": r.ID})) {
		t.Fatal("rotated credential inactive")
	}
	s.Requests.Pool.Exec(ctx, "UPDATE raptor.agent_tokens SET expires_at=now()-interval '1 second'")
	if active(t, introspection(s, second, "raptor", "request_get", map[string]string{"requestId": r.ID})) {
		t.Fatal("expired credential accepted")
	}
	attempt = domain.NewID()
	w := accessCall(s, "DELETE", "/v1/requests/"+r.ID+"/agent-access/"+attempt, "fixture-controller", "fixture-controller-password", nil)
	if w.Code != 200 {
		t.Fatal("unknown attempt revoke failed")
	}
	w = accessCall(s, "POST", "/v1/requests/"+r.ID+"/agent-access", "fixture-controller", "fixture-controller-password", map[string]any{"attemptId": attempt, "definitionSha256": hash, "expiresAt": expiry})
	if w.Code != 409 {
		t.Fatal("delayed issuance bypassed revocation tombstone")
	}
}
