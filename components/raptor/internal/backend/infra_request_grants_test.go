package backend

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/approvals"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/domain"
)

func TestInfraGrantClaimRequiresServiceIdentityAndCurrentProof(t *testing.T) {
	ctx := context.Background()
	h, request, fixture, _ := resourceRuntimeFixture(t)
	if _, err := h.Requests.Pool.Exec(ctx, "UPDATE raptor.requests SET status='running' WHERE id=$1", request.ID); err != nil {
		t.Fatal(err)
	}
	h.Approvals.VerifyGrantScope = func(context.Context, approvals.RequestGrantInput) (approvals.ResourceVerification, error) {
		return func(context.Context, pgx.Tx) error { return nil }, nil
	}
	capability := approvals.GrantCapability{Capability: "db-proxy.scale-in",
		Target:      approvals.GrantTarget{Kind: "db-proxy", Code: request.Definition.Object.Code},
		Constraints: json.RawMessage(`{"desiredCapacity":2}`)}
	in := approvals.RequestGrantInput{RequestID: request.ID, EnvCode: request.Definition.EnvCode,
		ScopeDigest: fixture.proof.ResolutionDigest, Capabilities: []approvals.GrantCapability{capability}}
	pending, err := h.Approvals.RequestGrant(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	admin := domain.User{ID: domain.NewID(), Role: "admin"}
	if _, err := h.Approvals.DecideRequestGrant(ctx, admin, request.ID, pending.ID, "approve"); err != nil {
		t.Fatal(err)
	}
	if _, err := h.Requests.Pool.Exec(ctx, "UPDATE raptor.requests SET status='running' WHERE id=$1", request.ID); err != nil {
		t.Fatal(err)
	}
	action := approvals.GrantActionInput{RequestID: request.ID, EnvCode: in.EnvCode, ScopeDigest: in.ScopeDigest,
		GrantID: pending.ID, ActionID: "exact-action", Capability: capability, Arguments: capability.Constraints}
	input := map[string]any{"action": action, "proof": fixture.proof}
	const path = "/v1/infra/request-grant-claim"
	if got := accessCall(h, "POST", path, "", "", input); got.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous claim returned %d", got.Code)
	}
	changed := fixture.proof
	changed.Identity.Application.UID = "different"
	changed.ResolutionDigest = changed.Identity.Digest()
	badAction := action
	badAction.ScopeDigest = changed.ResolutionDigest
	if got := accessCall(h, "POST", path, "fixture-controller", "fixture-controller-password", map[string]any{"action": badAction, "proof": changed}); got.Code == http.StatusOK {
		t.Fatal("changed proof admitted")
	}
	got := accessCall(h, "POST", path, "fixture-controller", "fixture-controller-password", input)
	var result struct{ Data approvals.GrantActionReceipt }
	if got.Code != http.StatusOK || json.Unmarshal(got.Body.Bytes(), &result) != nil || !result.Data.Claimed {
		t.Fatalf("valid claim returned %d %s", got.Code, got.Body.String())
	}
	record := approvals.GrantActionRecord{Action: action, Outcome: "unknown"}
	if got := accessCall(h, "POST", "/v1/infra/request-grant-record", "", "", record); got.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous record returned %d", got.Code)
	}
	if got := accessCall(h, "POST", "/v1/infra/request-grant-record", "fixture-controller", "fixture-controller-password", record); got.Code != http.StatusOK {
		t.Fatalf("record returned %d %s", got.Code, got.Body.String())
	}
	got = accessCall(h, "POST", path, "fixture-controller", "fixture-controller-password", input)
	if got.Code != http.StatusOK || json.Unmarshal(got.Body.Bytes(), &result) != nil || result.Data.Claimed || result.Data.State != "unknown" {
		t.Fatalf("unknown replay returned %d %s", got.Code, got.Body.String())
	}
}

func TestVersionedRequestGrantUsesBoundPolicyAndTrustedStart(t *testing.T) {
	ctx := context.Background()
	h, request, fixture, token := resourceRuntimeFixture(t, "request-grants-v1")
	fresh, err := h.Requests.Get(ctx, request.ID)
	if err != nil || fresh.Status != "running" {
		t.Fatalf("attempt handoff without UI polling = %q %v", fresh.Status, err)
	}
	capability := approvals.GrantCapability{Capability: "db-proxy.scale-in",
		Target:      approvals.GrantTarget{Kind: "db-proxy", Code: request.Definition.Object.Code},
		Constraints: json.RawMessage(`{"desiredCapacity":2}`)}
	in := approvals.RequestGrantInput{RequestID: request.ID, EnvCode: request.Definition.EnvCode,
		ScopeDigest: fixture.proof.ResolutionDigest, Capabilities: []approvals.GrantCapability{capability}}
	if !active(t, introspection(h, token, "raptor", "request_grant_request",
		map[string]string{"requestId": in.RequestID, "envCode": in.EnvCode, "scopeDigest": in.ScopeDigest})) {
		t.Fatal("request-bound agent cannot propose the bundle")
	}
	if active(t, introspection(h, token, "raptor", "approval_request",
		map[string]string{"requestId": in.RequestID, "envCode": in.EnvCode, "proxyCode": capability.Target.Code})) {
		t.Fatal("versioned request reached legacy approval")
	}
	path := "/v1/requests/" + request.ID + "/grants"
	if got := accessCall(h, "POST", path, "", "", in); got.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous proposal returned %d", got.Code)
	}
	in.Capabilities[0].Target.Code = "foreign"
	if got := accessCall(h, "POST", path, "fixture-controller", "fixture-controller-password", in); got.Code != http.StatusForbidden {
		t.Fatalf("foreign proposal returned %d %s", got.Code, got.Body.String())
	}
	in.Capabilities[0] = capability
	got := accessCall(h, "POST", path, "fixture-controller", "fixture-controller-password", in)
	var receipt struct{ Data approvals.RequestGrant }
	if got.Code != http.StatusCreated || json.Unmarshal(got.Body.Bytes(), &receipt) != nil || receipt.Data.State != "pending" {
		t.Fatalf("verified proposal returned %d %s", got.Code, got.Body.String())
	}
	view, err := h.Requests.View(ctx, request.ID)
	if err != nil || len(view.Grants) != 1 {
		t.Fatalf("public view missed the bundle: %d %v", len(view.Grants), err)
	}
	browser := "/api/v1/requests/" + request.ID + "/grants/" + receipt.Data.ID + "/decision"
	if got := accessCall(h, "POST", browser, "fixture-controller", "fixture-controller-password", map[string]string{"decision": "approve"}); got.Code != http.StatusUnauthorized {
		t.Fatalf("private service identity reached human decision: %d", got.Code)
	}
}
