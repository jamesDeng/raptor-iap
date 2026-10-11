package execution

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestReviewLateApprovalPreservesCompletion(t *testing.T) {
	s, x, b := liveAttempt(t)
	ctx := context.Background()
	b.Operation = "db-proxy.replace-nodes"
	b.ObjectKind = "db-proxy"
	b.ClusterID = "cluster"
	b.DefinitionSHA256 = strings.Repeat("a", 64)
	raw, _ := json.Marshal(b)
	if _, e := s.Pool.Exec(ctx, "UPDATE gateway.attempts SET binding=$2 WHERE id=$1", x.AttemptID, raw); e != nil {
		t.Fatal(e)
	}
	if _, e := s.Pool.Exec(ctx, `UPDATE gateway.executions SET status='completed',cleanup_status='{"sandboxAbsent":true,"keyAbsent":true,"accessRevoked":true}',recovery_needed=false WHERE request_id=$1`, b.RequestID); e != nil {
		t.Fatal(e)
	}
	for _, kind := range []string{"approval", "continue", "block"} {
		signal := Signal{ID: NewID(), RequestID: b.RequestID, Kind: kind, Payload: []byte(`{"approvalId":"33333333-3333-4333-8333-333333333333"}`)}
		for range 2 {
			if e := s.DeliverSignal(ctx, signal); e != nil {
				t.Fatal(e)
			}
		}
	}

	got, e := s.Get(ctx, b.RequestID)
	if e != nil {
		t.Fatal(e)
	}
	if got.Status != "completed" {
		t.Fatalf("late approval changed completed request to %s", got.Status)
	}
}
