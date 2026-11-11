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

func TestFailedPostDrainReadOnlyContinuationReusesOriginalProof(t *testing.T) {
	for _, tool := range []string{"mcp__raptor__request_grant_action_get", "mcp__infra__deployment_restart", "pgcat_connection_continuity", "resource_wait"} {
		t.Run(tool, func(t *testing.T) {
			s, _, b := failedResourcePreparation(t)
			ctx := t.Context()
			must := func(err error) {
				t.Helper()
				if err != nil {
					t.Fatal(err)
				}
			}
			_, err := s.Pool.Exec(ctx, `UPDATE gateway.attempts SET binding=jsonb_set(binding,'{approvalProtocol}','"request-grants-v1"') WHERE request_id=$1`, b.RequestID)
			must(err)
			b.ApprovalProtocol = "request-grants-v1"
			checkpoint, _ := json.Marshal(verifiedCheckpoint())
			markFailed := func(attempt, runtimeTool string) {
				_, err := s.Pool.Exec(ctx, "UPDATE gateway.executions SET failure_code='MissingEvidence',checkpoint_status='verified' WHERE request_id=$1", b.RequestID)
				must(err)
				_, err = s.Pool.Exec(ctx, "UPDATE gateway.attempts SET runtime_id='raptor-fixture',verified_checkpoint=$2 WHERE id=$1", attempt, checkpoint)
				must(err)
				event, _ := json.Marshal(RuntimeEvent{Kind: "tool_result", Tool: runtimeTool, Outcome: "succeeded", RuntimeSequence: 1, OccurredAt: time.Now()})
				_, err = s.Pool.Exec(ctx, "INSERT INTO gateway.runtime_events(attempt_id,runtime_sequence,payload) VALUES($1,1,$2)", attempt, event)
				must(err)
			}
			sendContinue := func(attempt string) error {
				payload, _ := json.Marshal(map[string]string{"attemptId": attempt})
				return s.DeliverSignal(ctx, Signal{ID: NewID(), RequestID: b.RequestID, Kind: "continue", Payload: payload})
			}
			original := b.AttemptID
			markFailed(original, "pgcat_connection_continuity")
			must(sendContinue(original))
			next, err := s.ClaimLiveNext(ctx, "retry")
			must(err)
			if next == nil {
				t.Fatal("first recovery not queued")
			}
			b.AttemptID = next.AttemptID
			must(s.BindLive(ctx, b.AttemptID, "retry", b, b.DefinitionSHA256))
			must(s.FinalizeLive(ctx, b.AttemptID, "retry", LiveOutcome{Status: "failed", FailureCode: "MissingEvidence", SandboxAbsent: true, KeyAbsent: true, AccessRevoked: true}))
			markFailed(b.AttemptID, tool)
			if tool != "mcp__raptor__request_grant_action_get" && tool != "pgcat_connection_continuity" {
				if err := sendContinue(b.AttemptID); err != ErrInvalid {
					t.Fatalf("potential side effect allowed replay: %v", err)
				}
				return
			}
			must(sendContinue(b.AttemptID))
			var marker string
			must(s.Pool.QueryRow(ctx, "SELECT resume_context->>'postDrainAttempt' FROM gateway.executions WHERE request_id=$1", b.RequestID).Scan(&marker))
			if marker != original {
				t.Fatalf("proof source changed: %s", marker)
			}
		})
	}
}
