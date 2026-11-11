package execution

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestContinueAfterCleanFleetReadFailure(t *testing.T) {
	for _, tc := range []struct {
		name, category string
		write          bool
		want           bool
	}{
		{"approved read-only failure", "http-503", false, true},
		{"failure with possible write", "http-503", true, false},
		{"authorization failure", "http-403", false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, x, b := failedResourcePreparation(t)
			ctx := t.Context()
			must := func(err error) {
				t.Helper()
				if err != nil {
					t.Fatal(err)
				}
			}
			_, err := s.Pool.Exec(ctx, `UPDATE gateway.attempts SET binding=jsonb_set(binding,'{approvalProtocol}','"request-grants-v1"') WHERE request_id=$1`, b.RequestID)
			must(err)
			checkpoint, _ := json.Marshal(verifiedCheckpoint())
			_, err = s.Pool.Exec(ctx, `UPDATE gateway.attempts SET runtime_id='fixture',verified_checkpoint=$2 WHERE id=$1`, b.AttemptID, checkpoint)
			must(err)
			grant := NewID()
			bundleDigest := strings.Repeat("f", 64)
			wait := LiveWait{Kind: "grant", ApprovalID: grant, ActionID: grant, BindingDigest: bundleDigest, StartedAt: time.Now().Add(-time.Minute)}
			waitRaw, _ := json.Marshal(wait)
			continuation, _ := json.Marshal(ContinuationContext{Kind: "grant", ApprovalID: grant, ActionID: grant, BindingDigest: bundleDigest, Decision: "resume"})
			_, err = s.Pool.Exec(ctx, `UPDATE gateway.attempts SET state='waiting_approval' WHERE id=(SELECT attempt_id FROM gateway.operation_sessions WHERE request_id=$1)`, b.RequestID)
			must(err)
			_, err = s.Pool.Exec(ctx, `UPDATE gateway.live_waits SET wait=$2 WHERE request_id=$1`, b.RequestID, waitRaw)
			must(err)
			_, err = s.Pool.Exec(ctx, `UPDATE gateway.executions SET failure_code='ToolFailed',checkpoint_status='verified',resume_context=$2 WHERE request_id=$1`, b.RequestID, continuation)
			must(err)
			events := []RuntimeEvent{
				{Kind: "tool_result", Tool: "mcp__raptor__request_grant_get", Outcome: "succeeded"},
				{Kind: "progress", Outcome: "running", Summary: "Observation cloud_read " + tc.category},
				{Kind: "tool_result", Tool: "cloud_read", Outcome: "failed"},
			}
			if tc.write {
				events = append(events, RuntimeEvent{Kind: "tool_start", Tool: "mcp__infra__db_proxy_scale", Outcome: "started"})
			}
			for i, row := range events {
				row.RuntimeSequence = int64(i + 1)
				row.OccurredAt = time.Now()
				raw, _ := json.Marshal(row)
				_, err = s.Pool.Exec(ctx, `INSERT INTO gateway.runtime_events(attempt_id,runtime_sequence,payload) VALUES($1,$2,$3)`, b.AttemptID, row.RuntimeSequence, raw)
				must(err)
			}
			payload, _ := json.Marshal(map[string]string{"attemptId": x.AttemptID})
			err = s.DeliverSignal(ctx, Signal{ID: NewID(), RequestID: b.RequestID, Kind: "continue", Payload: payload})
			if !tc.want {
				if err != ErrInvalid {
					t.Fatalf("unexpected admission %v", err)
				}
				return
			}
			must(err)
			var gotGrant string
			var recover bool
			must(s.Pool.QueryRow(ctx, `SELECT resume_context->>'approvalId', (resume_context->>'preExpansionRetry')::boolean FROM gateway.executions WHERE request_id=$1`, b.RequestID).Scan(&gotGrant, &recover))
			if !recover || gotGrant != grant {
				t.Fatal("recovery lost the approved grant")
			}
		})
	}
}
