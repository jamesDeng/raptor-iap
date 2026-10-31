package backend

import (
	"context"
	"encoding/json"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/approvals"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/domain"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

func approvedHandoff(t *testing.T, h *Server, request, proxy string) approvals.ApprovalInput {
	t.Helper()
	in := approvals.ApprovalInput{RequestID: request, ActionID: domain.NewID(), Interface: "db-proxy.scale-in", EnvCode: "dev", Target: map[string]any{"proxyCode": proxy}, Parameters: map[string]any{"desiredCapacity": 2}}
	a, e := h.Approvals.Request(context.Background(), in)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = h.Approvals.Decide(context.Background(), domain.User{ID: domain.NewID(), Role: "user"}, a.ID, approvals.DecisionInput{Decision: "approve"}); e != nil {
		t.Fatal(e)
	}
	return in
}
func prepareApprovalHandoff(t *testing.T, h *Server, phase string, in approvals.ApprovalInput) string {
	t.Helper()
	w := accessCall(h, "POST", "/v1/infra/approval-prepare", "fixture-controller", "fixture-controller-password", infraApprovalOperation{phase, in})
	var p struct{ Data infraApprovalPreparation }
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &p) != nil || !p.Data.Eligible || p.Data.Prepared == "" {
		t.Fatalf("prepare %d %s", w.Code, w.Body.String())
	}
	return p.Data.Prepared
}

// Both Check and Claim must bind the complete proof and same approved operation;
// a successful preparation never survives changes before final validation.
func TestResourceApprovalPrivateHandoffChanges(t *testing.T) {
	for _, phase := range []string{"check", "claim"} {
		for _, change := range []string{"none", "listener", "database", "uid", "missing", "legacy", "digest", "duplicate", "case", "null", "unknown", "control", "fingerprint", "catalog-account", "catalog-cluster", "catalog-app", "scope", "pending", "capacity", "action", "operation", "consumed"} {
			t.Run(phase+"/"+change, func(t *testing.T) {
				h, r, f, _ := resourceRuntimeFixture(t)
				in := approvedHandoff(t, h, r.ID, r.Definition.Object.Code)
				other := approvedHandoff(t, h, r.ID, r.Definition.Object.Code)
				f.proofs = 0
				prepared := prepareApprovalHandoff(t, h, phase, in)
				if f.proofs != 0 {
					t.Fatal("prepare re-entered Infra")
				}
				proof := f.proof
				finalPhase := phase
				var e error
				switch change {
				case "listener":
					proof.Identity.ListenerID = "changed"
				case "database":
					proof.Identity.DatabaseResourceID = "changed"
				case "uid":
					proof.Identity.Application.UID = "changed"
				case "control":
					_, e = h.Requests.Pool.Exec(context.Background(), "UPDATE raptor.requests SET control_state='cancelled' WHERE id=$1", r.ID)
				case "fingerprint":
					_, e = h.Requests.Pool.Exec(context.Background(), `UPDATE raptor.requests SET definition=jsonb_set(definition,'{model}','"changed"') WHERE id=$1`, r.ID)
				case "catalog-account":
					_, e = h.Requests.Pool.Exec(context.Background(), "UPDATE raptor.environments SET account_id='changed' WHERE code='dev'")
				case "catalog-cluster":
					_, e = h.Requests.Pool.Exec(context.Background(), "UPDATE raptor.environments SET ack_cluster_id='changed' WHERE code='dev'")
				case "catalog-app":
					_, e = h.Requests.Pool.Exec(context.Background(), `UPDATE raptor.environment_settings SET value_json='{}' WHERE environment_code='dev' AND key='proxyApplications'`)
				case "scope":
					_, e = h.Requests.Pool.Exec(context.Background(), `UPDATE raptor.agent_attempt_access SET replacement_scope=replacement_scope-'resolutionDigest' WHERE request_id=$1`, r.ID)
				case "pending":
					_, e = h.Requests.Pool.Exec(context.Background(), "UPDATE raptor.approvals SET state='pending' WHERE request_id=$1", r.ID)
				case "capacity":
					in.Parameters = map[string]any{"desiredCapacity": 3}
				case "action":
					in = other
				case "operation":
					if phase == "check" {
						finalPhase = "claim"
					} else {
						finalPhase = "check"
					}
				case "consumed":
					v, err := h.Approvals.Claim(context.Background(), in)
					e = err
					if !v.Claimed {
						t.Fatal("fixture claim failed")
					}
					f.proofs = 0
				}
				if e != nil {
					t.Fatal(e)
				}
				proof.ResolutionDigest = proof.Identity.Digest()
				if change == "legacy" {
					proof.ResolutionDigest = ""
				}
				if change == "digest" {
					proof.ResolutionDigest = strings.Repeat("0", 64)
				}
				input := map[string]any{"operation": finalPhase, "binding": in, "prepared": prepared, "proof": proof}
				if change == "missing" {
					delete(input, "proof")
				}
				raw, _ := json.Marshal(input)
				body := string(raw)
				switch change {
				case "duplicate":
					body = strings.Replace(body, `"listenerId":"l"`, `"listenerId":"l","listenerId":"l"`, 1)
				case "case":
					body = strings.Replace(body, `"listenerId"`, `"ListenerId"`, 1)
				case "null":
					body = strings.Replace(body, `"port":5432`, `"port":null`, 1)
				case "unknown":
					body = strings.Replace(body, `"port":5432`, `"port":5432,"foreign":"x"`, 1)
				}
				req := httptest.NewRequest("POST", "/v1/infra/approval-validate", strings.NewReader(body))
				req.SetBasicAuth("fixture-controller", "fixture-controller-password")
				w := httptest.NewRecorder()
				h.Mux.ServeHTTP(w, req)
				var result struct {
					Data struct{ Allowed, Claimed bool }
				}
				_ = json.Unmarshal(w.Body.Bytes(), &result)
				allowed := w.Code == 200 && (result.Data.Allowed || result.Data.Claimed)
				if allowed != (change == "none") {
					t.Fatalf("%s %s: %d %s", phase, change, w.Code, w.Body.String())
				}
				if f.proofs != 0 {
					t.Fatal("final validation re-entered Infra", f.proofs)
				}
				var count int
				if e = h.Requests.Pool.QueryRow(context.Background(), "SELECT count(*) FROM raptor.approval_submissions WHERE request_id=$1", r.ID).Scan(&count); e != nil {
					t.Fatal(e)
				}
				want := 0
				if (phase == "claim" && change == "none") || change == "consumed" {
					want = 1
				}
				if count != want {
					t.Fatal("unexpected claim count", count, want)
				}
			})
		}
	}
}
func TestResourceApprovalPrivateHandoffSinglePoolExactlyOnce(t *testing.T) {
	h, r, f, _ := resourceRuntimeFixture(t)
	in := approvedHandoff(t, h, r.ID, r.Definition.Object.Code)
	config := h.Requests.Pool.Config()
	config.MaxConns = 1
	pool, e := pgxpool.NewWithConfig(context.Background(), config)
	if e != nil {
		t.Fatal(e)
	}
	defer pool.Close()
	limited := New(pool)
	limited.Catalog.Infra = f
	limited.RegisterService("fixture-controller", "fixture-controller-password")
	for _, phase := range []string{"check", "claim"} {
		prepared := prepareApprovalHandoff(t, limited, phase, in)
		input := infraApprovalValidation{Operation: phase, Binding: in, Prepared: prepared, Proof: f.proof}
		var wg sync.WaitGroup
		var allowed atomic.Int32
		for n := 0; n < 4; n++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				w := accessCall(limited, "POST", "/v1/infra/approval-validate", "fixture-controller", "fixture-controller-password", input)
				var out struct {
					Data struct{ Allowed, Claimed bool }
				}
				_ = json.Unmarshal(w.Body.Bytes(), &out)
				if w.Code == 200 && (out.Data.Allowed || out.Data.Claimed) {
					allowed.Add(1)
				}
			}()
		}
		wg.Wait()
		want := int32(4)
		if phase == "claim" {
			want = 1
		}
		if allowed.Load() != want {
			t.Fatal(phase, allowed.Load(), want)
		}
	}
}

func TestResourceApprovalCheckRechecksAfterProof(t *testing.T) {
	for _, change := range []string{"control", "definition", "scope", "approval"} {
		t.Run(change, func(t *testing.T) {
			h, r, _, _ := resourceRuntimeFixture(t)
			in := approvedHandoff(t, h, r.ID, r.Definition.Object.Code)
			real := h.Approvals.VerifyResource
			h.Approvals.VerifyResource = func(ctx context.Context, input approvals.ApprovalInput) (approvals.ResourceVerification, error) {
				verify, e := real(ctx, input)
				if e != nil {
					return nil, e
				}
				switch change {
				case "control":
					_, e = h.Requests.Pool.Exec(ctx, "UPDATE raptor.requests SET control_state='cancelled' WHERE id=$1", r.ID)
				case "definition":
					_, e = h.Requests.Pool.Exec(ctx, `UPDATE raptor.requests SET definition=jsonb_set(definition,'{model}','"changed"') WHERE id=$1`, r.ID)
				case "scope":
					_, e = h.Requests.Pool.Exec(ctx, `UPDATE raptor.agent_attempt_access SET replacement_scope=replacement_scope-'resolutionDigest' WHERE request_id=$1`, r.ID)
				case "approval":
					_, e = h.Requests.Pool.Exec(ctx, "UPDATE raptor.approvals SET state='pending' WHERE request_id=$1", r.ID)
				}
				return verify, e
			}
			out, e := h.Approvals.Check(context.Background(), in)
			if out.Allowed {
				t.Fatal("changed eligibility authorized", change, out, e)
			}
		})
	}
}
