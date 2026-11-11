package execution

import (
	"encoding/json"
	"testing"
	"time"
)

func TestRetirementReadFailureResumesTheExactVerifiedAttempt(t *testing.T) {
	for _, tc := range []struct {
		name, category     string
		historical, writes bool
	}{
		{"same attempt", "http-503", false, false},
		{"after a resumed wait", "http-503", true, false},
		{"reject intervening write", "http-503", true, true},
		{"forbidden", "http-403", false, false},
		{"invalid response", "invalid-response", false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
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
			failed := []RuntimeEvent{
				{Kind: "progress", Summary: "Observation cloud_read " + tc.category, Outcome: "running"},
				{Kind: "tool_result", Tool: "cloud_read", Outcome: "failed"},
				{Kind: "progress", Summary: "Observation cloud_read tool-end-failed", Outcome: "running"},
			}
			appendEvents := func(attempt string, events []RuntimeEvent) {
				for i, v := range events {
					v.RuntimeSequence = int64(i + 1)
					v.OccurredAt = time.Now()
					raw, _ := json.Marshal(v)
					_, e = s.Pool.Exec(ctx, "INSERT INTO gateway.runtime_events(attempt_id,runtime_sequence,payload) VALUES($1,$2,$3)", attempt, v.RuntimeSequence, raw)
					must(e)
				}
			}
			origin := b.AttemptID
			if tc.historical {
				origin = NewID()
				_, e = s.Pool.Exec(ctx, `INSERT INTO gateway.attempts(id,request_id,state,runtime_id,started_at,ended_at,binding,definition_sha256,verified_checkpoint)
				  SELECT $2,request_id,'failed','historical',started_at-interval '2 minutes',started_at-interval '1 minute',binding,definition_sha256,verified_checkpoint
				  FROM gateway.attempts WHERE id=$1`, b.AttemptID, origin)
				must(e)
				latest := failed
				if tc.writes {
					latest = append(append([]RuntimeEvent{}, failed...), RuntimeEvent{Kind: "tool_start", Tool: "mcp__infra__deployment_restart", Outcome: "started"})
				}
				appendEvents(b.AttemptID, latest)
			}
			appendEvents(origin, append([]RuntimeEvent{{Kind: "tool_result", Tool: "mcp__infra__db_proxy_scale", Outcome: "succeeded"}}, failed...))
			payload, _ := json.Marshal(map[string]string{"attemptId": x.AttemptID})
			e = s.DeliverSignal(ctx, Signal{ID: NewID(), RequestID: b.RequestID, Kind: "continue", Payload: payload})
			if tc.category != "http-503" || tc.writes {
				if e != ErrInvalid {
					t.Fatalf("unexpected admission %v", e)
				}
				return
			}
			must(e)
			var source string
			must(s.Pool.QueryRow(ctx, "SELECT resume_context->>'postRetirementAttempt' FROM gateway.executions WHERE request_id=$1", b.RequestID).Scan(&source))
			if source != origin {
				t.Fatalf("wrong source %q", source)
			}
		})
	}
}
