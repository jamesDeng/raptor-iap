package backend

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/jamesDeng/raptor-iap/components/raptor/internal/approvals"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/domain"
)

func TestPgcatObservationRequiresCurrentBoundServiceAttempt(t *testing.T) {
	ctx := context.Background()
	h, request, fixture, _ := resourceRuntimeFixture(t, "request-grants-v1")
	var attempt string
	if err := h.Requests.Pool.QueryRow(ctx, "SELECT attempt_id::text FROM raptor.agent_attempt_access WHERE request_id=$1", request.ID).Scan(&attempt); err != nil {
		t.Fatal(err)
	}
	scope := fixture.proof.ResolutionDigest
	payload, _ := json.Marshal(map[string]string{"requestId": request.ID, "scopeDigest": scope, "observationId": domain.NewID()})
	observation := approvals.PgcatObservation{RequestID: request.ID, AttemptID: attempt, ScopeDigest: scope, Kind: "hold", Subject: "fleet",
		ObservedAt: time.Now().UTC().Truncate(time.Microsecond), Data: payload}
	route := "/v1/requests/" + request.ID + "/pgcat-observations"
	if w := accessCall(h, http.MethodPost, route, "", "", observation); w.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous evidence accepted: %d", w.Code)
	}
	call := func(in approvals.PgcatObservation) int {
		return accessCall(h, http.MethodPost, route, "fixture-controller", "fixture-controller-password", in).Code
	}
	if status := call(observation); status != http.StatusCreated {
		t.Fatalf("current observation returned %d", status)
	}
	changed := observation
	changed.AttemptID = domain.NewID()
	if status := call(changed); status != http.StatusForbidden {
		t.Fatalf("unknown attempt returned %d", status)
	}
	changed = observation
	changed.ObservedAt = changed.ObservedAt.Add(time.Millisecond)
	changed.Data, _ = json.Marshal(map[string]string{"requestId": request.ID, "scopeDigest": scope, "observationId": domain.NewID()})
	if status := call(changed); status != http.StatusConflict {
		t.Fatalf("new physical hold retargeted recorded evidence: %d", status)
	}
	if _, err := h.Requests.Pool.Exec(ctx, "UPDATE raptor.requests SET status='completed' WHERE id=$1", request.ID); err != nil {
		t.Fatal(err)
	}
	if status := call(observation); status != http.StatusConflict {
		t.Fatalf("closed request accepted new evidence: %d", status)
	}
	proofs, err := h.Approvals.PgcatObservations(ctx, request.ID, scope)
	if err != nil || len(proofs) != 1 || proofs[0].AttemptID != attempt {
		t.Fatalf("saved observations: %+v %v", proofs, err)
	}
}
