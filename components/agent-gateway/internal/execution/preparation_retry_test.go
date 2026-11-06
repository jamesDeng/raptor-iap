package execution

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func failedResourcePreparation(t *testing.T) (*Store, *Execution, AttemptBinding) {
	t.Helper()
	s, x, b := liveAttempt(t)
	ctx := context.Background()
	b.Operation = "db-proxy.replace-nodes"
	b.ObjectKind = "db-proxy"
	b.ExecutionMode = "replacement"
	b.ClusterID = "cluster"
	b.DefinitionSHA256 = strings.Repeat("a", 64)
	raw, _ := json.Marshal(b)
	must := func(e error) {
		t.Helper()
		if e != nil {
			t.Fatal(e)
		}
	}
	_, e := s.Pool.Exec(ctx, "UPDATE gateway.attempts SET binding=$2,definition_sha256=$3 WHERE id=$1", x.AttemptID, raw, b.DefinitionSHA256)
	must(e)
	cp := SessionCheckpoint{RequestID: b.RequestID, DefinitionSHA256: b.DefinitionSHA256, SkillsCommit: b.SkillsCommit, Model: b.Model, SessionID: "session", SessionFile: "session.jsonl", SessionSHA256: strings.Repeat("b", 64), Archive: verifiedCheckpoint()}
	wait := LiveWait{Kind: "resource", ActionID: "resource-ready", BindingDigest: b.DefinitionSHA256, WakeAfterSeconds: 60, StartedAt: time.Now().Add(-time.Minute)}
	must(s.SaveLiveWait(ctx, x.AttemptID, "owner", wait, cp))
	must(s.ReleaseLiveWait(ctx, x.AttemptID, "owner", LiveCleanup{true, true}, true))
	s.ResolveLiveDecision = func(context.Context, string, LiveWait) (LiveContinuation, error) {
		return LiveContinuation{Next: "resume"}, nil
	}
	signal := NewID()
	_, e = s.Pool.Exec(ctx, `INSERT INTO gateway.signals(id,request_id,kind,payload) VALUES($1,$2,'continue','{"approvalId":""}')`, signal, b.RequestID)
	must(e)
	ok, e := s.ResumeLive(ctx, b.RequestID, signal)
	must(e)
	if !ok {
		t.Fatal("not resumed")
	}
	next, e := s.ClaimLiveNext(ctx, "next")
	must(e)
	if next == nil {
		t.Fatal("not claimed")
	}
	b.AttemptID = next.AttemptID
	must(s.BindLive(ctx, b.AttemptID, "next", b, b.DefinitionSHA256))
	must(s.RecordIntent(ctx, b.AttemptID, "next", RuntimeIntent{Kind: "access", Name: b.AttemptID}))
	must(s.FinalizeLive(ctx, b.AttemptID, "next", LiveOutcome{Status: "failed", FailureCode: "ProviderUnavailable", SandboxAbsent: true, KeyAbsent: true, AccessRevoked: true}))
	return s, next, b
}
func TestFailedResourcePreparationCanResumeExactSavedSession(t *testing.T) {
	s, x, b := failedResourcePreparation(t)
	ctx := context.Background()
	before, e := s.OperationSession(ctx, b)
	if e != nil || before == nil {
		t.Fatal(e)
	}
	payload, _ := json.Marshal(map[string]string{"attemptId": x.AttemptID, "instructions": "resume saved session; do not repeat expansion"})
	if e = s.DeliverSignal(ctx, Signal{ID: NewID(), RequestID: x.RequestID, Kind: "continue", Payload: payload}); e != nil {
		t.Fatal(e)
	}
	state, e := s.Get(ctx, x.RequestID)
	if e != nil || state.Status != "queued" {
		t.Fatal("clean pre-runtime continuation not queued", state.Status, e)
	}
	next, e := s.ClaimLiveNext(ctx, "retry")
	if e != nil || next == nil {
		t.Fatal(e)
	}
	b.AttemptID = next.AttemptID
	if e = s.BindLive(ctx, b.AttemptID, "retry", b, b.DefinitionSHA256); e != nil {
		t.Fatal(e)
	}
	after, e := s.OperationSession(ctx, b)
	if e != nil || after == nil || *before != *after {
		t.Fatal("saved session changed", e)
	}
	if _, e = s.LiveContinuationContext(ctx, b); e != nil {
		t.Fatal("continuation lost", e)
	}
	if e = s.DeliverSignal(ctx, Signal{ID: NewID(), RequestID: x.RequestID, Kind: "continue", Payload: payload}); e != nil {
		t.Fatal(e)
	}
	state, _ = s.Get(ctx, x.RequestID)
	if state.Status != "running" {
		t.Fatal("duplicate changed new attempt")
	}
}
func TestFailedResourcePreparationRejectsUnsafeRetry(t *testing.T) {
	for _, scenario := range []string{"wrong-attempt", "missing-session", "different-session", "corrupt-session", "runtime-intent", "runtime-id", "runtime-event", "cleanup", "recovery", "wrong-failure", "wait-provenance", "resume-context", "newer-wait", "missing-access-intent"} {
		t.Run(scenario, func(t *testing.T) {
			s, x, _ := failedResourcePreparation(t)
			ctx := context.Background()
			attempt := x.AttemptID
			query := ""
			switch scenario {
			case "wrong-attempt":
				attempt = NewID()
			case "missing-session":
				query = "DELETE FROM gateway.operation_sessions WHERE request_id=$1"
			case "different-session":
				query = `UPDATE gateway.operation_sessions SET checkpoint=jsonb_set(checkpoint,'{sessionId}','"other-session"') WHERE request_id=$1`
			case "corrupt-session":
				query = `UPDATE gateway.operation_sessions SET checkpoint='{}' WHERE request_id=$1`
			case "runtime-intent":
				query = `INSERT INTO gateway.runtime_intents(attempt_id,kind,name) SELECT attempt_id,'sandbox','uncertain' FROM gateway.executions WHERE request_id=$1`
			case "runtime-id":
				query = `UPDATE gateway.attempts SET runtime_id='started' WHERE id=(SELECT attempt_id FROM gateway.executions WHERE request_id=$1)`
			case "runtime-event":
				query = `INSERT INTO gateway.runtime_events(attempt_id,runtime_sequence,payload) SELECT attempt_id,1,'{}' FROM gateway.executions WHERE request_id=$1`
			case "cleanup":
				query = `UPDATE gateway.executions SET cleanup_status='{}' WHERE request_id=$1`
			case "recovery":
				query = `UPDATE gateway.executions SET recovery_needed=true WHERE request_id=$1`
			case "wrong-failure":
				query = `UPDATE gateway.executions SET failure_code='ToolFailed' WHERE request_id=$1`
			case "wait-provenance":
				query = `UPDATE gateway.operation_sessions SET attempt_id=(SELECT attempt_id FROM gateway.executions WHERE request_id=$1) WHERE request_id=$1`
			case "missing-access-intent":
				query = `DELETE FROM gateway.runtime_intents WHERE attempt_id=(SELECT attempt_id FROM gateway.executions WHERE request_id=$1)`
			case "newer-wait":
				query = `INSERT INTO gateway.live_waits(attempt_id,request_id,approval_id,action_id,wait,checkpoint,phase) SELECT e.attempt_id,e.request_id,NULL,'newer-wait',w.wait,w.checkpoint,'released' FROM gateway.executions e JOIN gateway.live_waits w ON w.request_id=e.request_id WHERE e.request_id=$1`
			case "resume-context":
				query = `UPDATE gateway.executions SET resume_context='{}' WHERE request_id=$1`
			}
			if query != "" {
				if _, e := s.Pool.Exec(ctx, query, x.RequestID); e != nil {
					t.Fatal(e)
				}
			}
			payload, _ := json.Marshal(map[string]string{"attemptId": attempt, "instructions": "retry"})
			err := s.DeliverSignal(ctx, Signal{ID: NewID(), RequestID: x.RequestID, Kind: "continue", Payload: payload})
			if scenario != "wrong-attempt" && err != ErrInvalid {
				t.Fatal("unsafe exact retry must report rejection", scenario, err)
			}
			state, e := s.Get(ctx, x.RequestID)
			if e != nil || state.Status != "failed" {
				t.Fatal("unsafe attempt reopened", scenario, state.Status, e)
			}
		})
	}
}
