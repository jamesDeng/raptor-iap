package execution

import (
	"encoding/json"
	"testing"
	"time"
)

func TestFailedAfterDrainResumesOnlyExactVerifiedTerminalAttempt(t *testing.T) {
	for _, tool := range []string{"pgcat_connection_continuity", "deployment_read"} {
		t.Run(tool, func(t *testing.T) {
			s, x, b := failedResourcePreparation(t)
			ctx := t.Context()
			if _, err := s.Pool.Exec(ctx, `UPDATE gateway.attempts SET binding=jsonb_set(binding,'{approvalProtocol}','"request-grants-v1"') WHERE request_id=$1`, b.RequestID); err != nil {
				t.Fatal(err)
			}
			cp, _ := json.Marshal(verifiedCheckpoint())
			_, err := s.Pool.Exec(ctx, "UPDATE gateway.executions SET failure_code='MissingEvidence',checkpoint_status='verified' WHERE request_id=$1", b.RequestID)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = s.Pool.Exec(ctx, "UPDATE gateway.attempts SET runtime_id='raptor-fixture',verified_checkpoint=$2 WHERE id=$1", b.AttemptID, cp); err != nil {
				t.Fatal(err)
			}
			event, _ := json.Marshal(RuntimeEvent{Kind: "tool_result", Tool: tool, Outcome: "succeeded", RuntimeSequence: 1, OccurredAt: time.Now()})
			if _, err = s.Pool.Exec(ctx, "INSERT INTO gateway.runtime_events(attempt_id,runtime_sequence,payload) VALUES($1,1,$2)", b.AttemptID, event); err != nil {
				t.Fatal(err)
			}
			payload, _ := json.Marshal(map[string]string{"attemptId": x.AttemptID})
			err = s.DeliverSignal(ctx, Signal{ID: NewID(), RequestID: b.RequestID, Kind: "continue", Payload: payload})
			if tool != "pgcat_connection_continuity" {
				if err != ErrInvalid {
					t.Fatalf("unrelated activity reopened request: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("exact verified post-drain failure could not resume: %v", err)
			}
			next, err := s.ClaimLiveNext(ctx, "retry")
			if err != nil || next == nil || next.RequestID != b.RequestID {
				t.Fatalf("original request not queued: %+v %v", next, err)
			}
			var marker string
			if err = s.Pool.QueryRow(ctx, "SELECT resume_context->>'postDrainAttempt' FROM gateway.executions WHERE request_id=$1", b.RequestID).Scan(&marker); err != nil || marker != b.AttemptID {
				t.Fatalf("resume marker = %q, %v", marker, err)
			}
		})
	}
}
