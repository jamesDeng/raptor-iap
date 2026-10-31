package backend

import (
	"context"
	"testing"

	"github.com/jamesDeng/raptor-iap/components/raptor/internal/approvals"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/domain"
)

// Use the production RegisterService verifier and catalog database. Count both
// verifier admission and the external proof, so an early private-DB rejection
// cannot accidentally make a late eligibility check look correctly ordered.
func TestResourceApprovalEligibilityPrecedesProof(t *testing.T) {
	for _, phase := range []string{"request", "claim"} {
		cases := []string{"cancelled", "completed", "blocked", "control", "wrong-kind", "wrong-env", "missing-request"}
		if phase == "claim" {
			cases = append(cases, "missing-action", "pending-action", "denied-action", "mismatched-capacity", "mismatched-proxy", "already-claimed")
		}
		for _, which := range cases {
			t.Run(phase+"/"+which, func(t *testing.T) {
				h, r, f, _ := resourceRuntimeFixture(t)
				ctx := context.Background()
				in := approvals.ApprovalInput{RequestID: r.ID, ActionID: domain.NewID(), Interface: "db-proxy.scale-in", EnvCode: "dev", Target: map[string]any{"proxyCode": r.Definition.Object.Code}, Parameters: map[string]any{"desiredCapacity": 2}}
				if phase == "claim" {
					a, e := h.Approvals.Request(ctx, in)
					if e != nil {
						t.Fatal(e)
					}
					if _, e = h.Approvals.Decide(ctx, domain.User{ID: domain.NewID(), Role: "user"}, a.ID, approvals.DecisionInput{Decision: "approve"}); e != nil {
						t.Fatal(e)
					}
				}
				var err error
				switch which {
				case "cancelled", "completed", "blocked":
					_, err = h.Requests.Pool.Exec(ctx, "UPDATE raptor.requests SET status=$2 WHERE id=$1", r.ID, which)
				case "control":
					_, err = h.Requests.Pool.Exec(ctx, "UPDATE raptor.requests SET control_state='cancelled' WHERE id=$1", r.ID)
				case "wrong-kind":
					_, err = h.Requests.Pool.Exec(ctx, `UPDATE raptor.requests SET definition=jsonb_set(definition,'{type}','"direct"') WHERE id=$1`, r.ID)
				case "wrong-env":
					in.EnvCode = "foreign"
				case "missing-request":
					in.RequestID = domain.NewID()
				case "missing-action":
					in.ActionID = domain.NewID()
				case "pending-action":
					_, err = h.Requests.Pool.Exec(ctx, "UPDATE raptor.approvals SET state='pending' WHERE request_id=$1", r.ID)
				case "denied-action":
					_, err = h.Requests.Pool.Exec(ctx, "UPDATE raptor.approvals SET state='denied' WHERE request_id=$1", r.ID)
				case "mismatched-capacity":
					in.Parameters = map[string]any{"desiredCapacity": 3}
				case "mismatched-proxy":
					in.Target = map[string]any{"proxyCode": "foreign"}
				case "already-claimed":
					var got approvals.SubmissionClaimResult
					got, err = h.Approvals.Claim(ctx, in)
					if err == nil && !got.Claimed {
						t.Fatal("fixture claim denied")
					}
				}
				if err != nil {
					t.Fatal(err)
				}
				f.proofs = 0
				verifications := 0
				real := h.Approvals.VerifyResource
				h.Approvals.VerifyResource = func(ctx context.Context, in approvals.ApprovalInput) (approvals.ResourceVerification, error) {
					verifications++
					return real(ctx, in)
				}
				if phase == "request" {
					if _, e := h.Approvals.Request(ctx, in); e == nil {
						t.Error("ineligible approval request accepted")
					}
				} else {
					if got, e := h.Approvals.Claim(ctx, in); e != nil || got.Claimed {
						t.Error("ineligible claim result", got, e)
					}
				}
				if verifications != 0 || f.proofs != 0 {
					t.Errorf("ineligible %s reached verification/provider proof: %d/%d", which, verifications, f.proofs)
				}
				if phase == "claim" && which != "already-claimed" {
					var count int
					if e := h.Requests.Pool.QueryRow(ctx, "SELECT count(*) FROM raptor.approval_submissions WHERE request_id=$1", r.ID).Scan(&count); e != nil || count != 0 {
						t.Fatal("ineligible call recorded claim", count, e)
					}
				}
			})
		}
	}
}

// A successful nonlocking precheck is only a filter: request control and exact
// approval identity must still be current after fresh proof and row locking.
func TestResourceApprovalEligibilityIsRecheckedAfterProof(t *testing.T) {
	for _, phase := range []string{"request", "claim"} {
		changes := []string{"control"}
		if phase == "claim" {
			changes = append(changes, "pending-action", "mismatched-binding", "deleted-action")
		}
		for _, change := range changes {
			t.Run(phase+"/"+change, func(t *testing.T) {
				h, r, f, _ := resourceRuntimeFixture(t)
				ctx := context.Background()
				in := approvals.ApprovalInput{RequestID: r.ID, ActionID: domain.NewID(), Interface: "db-proxy.scale-in", EnvCode: "dev", Target: map[string]any{"proxyCode": r.Definition.Object.Code}, Parameters: map[string]any{"desiredCapacity": 2}}
				if phase == "claim" {
					a, e := h.Approvals.Request(ctx, in)
					if e != nil {
						t.Fatal(e)
					}
					if _, e = h.Approvals.Decide(ctx, domain.User{ID: domain.NewID(), Role: "user"}, a.ID, approvals.DecisionInput{Decision: "approve"}); e != nil {
						t.Fatal(e)
					}
				}
				f.proofs = 0
				real := h.Approvals.VerifyResource
				h.Approvals.VerifyResource = func(ctx context.Context, input approvals.ApprovalInput) (approvals.ResourceVerification, error) {
					verify, e := real(ctx, input)
					if e != nil {
						return nil, e
					}
					switch change {
					case "control":
						_, e = h.Requests.Pool.Exec(ctx, "UPDATE raptor.requests SET control_state='cancelled' WHERE id=$1", r.ID)
					case "pending-action":
						_, e = h.Requests.Pool.Exec(ctx, "UPDATE raptor.approvals SET state='pending' WHERE request_id=$1 AND action_id=$2", r.ID, in.ActionID)
					case "mismatched-binding":
						_, e = h.Requests.Pool.Exec(ctx, `UPDATE raptor.approvals SET binding=jsonb_set(binding,'{parameters,desiredCapacity}','3') WHERE request_id=$1 AND action_id=$2`, r.ID, in.ActionID)
					case "deleted-action":
						_, e = h.Requests.Pool.Exec(ctx, "DELETE FROM raptor.approvals WHERE request_id=$1 AND action_id=$2", r.ID, in.ActionID)
					}
					return verify, e
				}
				if phase == "request" {
					if _, e := h.Approvals.Request(ctx, in); e == nil {
						t.Error("intervening control change recorded approval")
					}
				} else {
					if got, e := h.Approvals.Claim(ctx, in); e != nil || got.Claimed {
						t.Error("intervening approval change authorized", got, e)
					}
				}
				if f.proofs != 1 {
					t.Errorf("expected initially eligible operation to perform exactly one proof, got %d", f.proofs)
				}
				var count int
				if e := h.Requests.Pool.QueryRow(ctx, "SELECT count(*) FROM raptor.approval_submissions WHERE request_id=$1", r.ID).Scan(&count); e != nil || count != 0 {
					t.Fatal("intervening change recorded claim", count, e)
				}
				if phase == "request" {
					if e := h.Requests.Pool.QueryRow(ctx, "SELECT count(*) FROM raptor.approvals WHERE request_id=$1 AND action_id=$2", r.ID, in.ActionID).Scan(&count); e != nil || count != 0 {
						t.Fatal("intervening change recorded approval", count, e)
					}
				}
			})
		}
	}
}
