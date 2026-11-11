package execution

import (
	"encoding/json"
	"testing"
	"time"
)

func TestRetirementReadFailureResumesTheExactVerifiedAttempt(t *testing.T) {
	for _, category := range []string{"http-503", "http-403", "invalid-response"} {
		t.Run(category, func(t *testing.T) {
			s, x, b := failedResourcePreparation(t)
			ctx := t.Context()
			must := func(e error) {
				t.Helper()
				if e != nil {
					t.Fatal(e)
				}
			}
			_, e := s.Pool.Exec(ctx, `UPDATE gateway.attempts SET binding=jsonb_set(binding,'{approvalProtocol}','"request-grants-v1"') WHERE request_id=$1`, b.RequestID)
			must(e)
			checkpoint, _ := json.Marshal(verifiedCheckpoint())
			_, e = s.Pool.Exec(ctx, "UPDATE gateway.attempts SET runtime_id='fixture',verified_checkpoint=$2 WHERE id=$1", b.AttemptID, checkpoint)
			must(e)
			_, e = s.Pool.Exec(ctx, "UPDATE gateway.executions SET failure_code='ToolFailed',checkpoint_status='verified' WHERE request_id=$1", b.RequestID)
			must(e)
			for i, v := range []RuntimeEvent{
				{Kind: "tool_result", Tool: "mcp__infra__db_proxy_scale", Outcome: "succeeded"},
				{Kind: "progress", Summary: "Observation cloud_read " + category, Outcome: "running"},
				{Kind: "tool_result", Tool: "cloud_read", Outcome: "failed"},
				{Kind: "progress", Summary: "Observation cloud_read tool-end-failed", Outcome: "running"},
			} {
				v.RuntimeSequence = int64(i + 1)
				v.OccurredAt = time.Now()
				raw, _ := json.Marshal(v)
				_, e = s.Pool.Exec(ctx, "INSERT INTO gateway.runtime_events(attempt_id,runtime_sequence,payload) VALUES($1,$2,$3)", b.AttemptID, v.RuntimeSequence, raw)
				must(e)
			}
			payload, _ := json.Marshal(map[string]string{"attemptId": x.AttemptID})
			e = s.DeliverSignal(ctx, Signal{ID: NewID(), RequestID: b.RequestID, Kind: "continue", Payload: payload})
			if category != "http-503" {
				if e != ErrInvalid {
					t.Fatalf("unexpected admission %v", e)
				}
				return
			}
			must(e)
			var source string
			must(s.Pool.QueryRow(ctx, "SELECT resume_context->>'postRetirementAttempt' FROM gateway.executions WHERE request_id=$1", b.RequestID).Scan(&source))
			if source != b.AttemptID {
				t.Fatalf("wrong source %q", source)
			}
		})
	}
}
