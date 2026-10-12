package execution

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

func TestQualificationReportRequiresPersistedApprovalWaitAndSession(t *testing.T) {
	s, x, _ := liveAttempt(t)
	ctx := context.Background()
	b, r := qualificationResultFixture()
	b.RequestID = x.RequestID
	b.AttemptID = x.AttemptID
	r.RequestID = b.RequestID
	r.AttemptID = b.AttemptID
	r.Qualification.ApprovalID = NewID()
	raw, _ := json.Marshal(b)
	if _, e := s.Pool.Exec(ctx, "UPDATE gateway.attempts SET binding=$2,definition_sha256=$3 WHERE id=$1", x.AttemptID, raw, b.DefinitionSHA256); e != nil {
		t.Fatal(e)
	}
	c := ContinuationContext{ApprovalID: r.Qualification.ApprovalID, ActionID: r.Qualification.ActionID, BindingDigest: r.Qualification.BindingDigest, Decision: "resume"}
	raw, _ = json.Marshal(c)
	if _, e := s.Pool.Exec(ctx, "UPDATE gateway.executions SET resume_context=$2 WHERE request_id=$1", b.RequestID, raw); e != nil {
		t.Fatal(e)
	}
	if e := s.SaveLiveResult(ctx, x.AttemptID, "owner", r); e == nil {
		t.Fatal("continuation without real wait/session accepted")
	}
	cp := SessionCheckpoint{RequestID: b.RequestID, DefinitionSHA256: b.DefinitionSHA256, SkillsCommit: b.SkillsCommit, Model: b.Model, SessionID: "session", SessionFile: "session.jsonl", SessionSHA256: stringRepeat("b", 64), Archive: verifiedCheckpoint()}
	if e := s.SaveOperationSession(ctx, x.AttemptID, "owner", cp); e != nil {
		t.Fatal(e)
	}
	wait := LiveWait{Kind: "approval", ApprovalID: c.ApprovalID, ActionID: c.ActionID, BindingDigest: c.BindingDigest, StartedAt: time.Now()}
	wr, _ := json.Marshal(wait)
	cr, _ := json.Marshal(cp)
	// A fixture records the prior wait; the production continuation path is independently tested.
	if _, e := s.Pool.Exec(ctx, "INSERT INTO gateway.live_waits(attempt_id,request_id,wait,checkpoint,approval_id,action_id,phase) VALUES($1,$2,$3,$4,$5,$6,'resumed')", x.AttemptID, b.RequestID, wr, cr, c.ApprovalID, c.ActionID); e != nil {
		t.Fatal(e)
	}
	if e := s.SaveLiveResult(ctx, x.AttemptID, "stale", r); e == nil {
		t.Fatal("stale owner accepted")
	}
	if e := s.SaveLiveResult(ctx, x.AttemptID, "owner", r); e != nil {
		t.Fatal("verified qualification refused", e)
	}
}
