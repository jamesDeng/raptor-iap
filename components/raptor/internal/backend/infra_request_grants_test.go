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
