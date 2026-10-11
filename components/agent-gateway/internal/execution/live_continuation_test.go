package execution

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestContinuationRequiresSameSessionAndFreshDecision(t *testing.T) {
	s, x, b := liveAttempt(t)
	ctx := context.Background()
	b.DefinitionSHA256 = strings.Repeat("a", 64)
	raw, _ := json.Marshal(b)
	s.Pool.Exec(ctx, "UPDATE gateway.attempts SET binding=$2,definition_sha256=$3 WHERE id=$1", x.AttemptID, raw, b.DefinitionSHA256)
	cp := SessionCheckpoint{RequestID: b.RequestID, DefinitionSHA256: b.DefinitionSHA256, SkillsCommit: b.SkillsCommit, Model: b.Model, SessionID: "session", SessionFile: "session.jsonl", SessionSHA256: strings.Repeat("b", 64), Archive: verifiedCheckpoint()}
	wait := LiveWait{Kind: "approval", ApprovalID: NewID(), ActionID: "scale-three", BindingDigest: strings.Repeat("c", 64), StartedAt: time.Now().UTC()}
	s.SaveLiveWait(ctx, x.AttemptID, "owner", wait, cp)
	s.ReleaseLiveWait(ctx, x.AttemptID, "owner", LiveCleanup{true, true}, true)
	payload, _ := json.Marshal(map[string]string{"approvalId": wait.ApprovalID})
	id := NewID()
	s.Pool.Exec(ctx, "INSERT INTO gateway.signals(id,request_id,kind,payload) VALUES($1,$2,'approval',$3)", id, b.RequestID, payload)
	current := "resume"
	s.ResolveLiveDecision = func(context.Context, string, LiveWait) (LiveContinuation, error) {
		return LiveContinuation{Next: current}, nil
	}
	s.ResumeLive(ctx, b.RequestID, id)
	got, e := s.LiveContinuationContext(ctx, b)
	if e != nil || got == nil || got.ApprovalID != wait.ApprovalID || got.ActionID != wait.ActionID || got.BindingDigest != wait.BindingDigest {
		t.Fatal("missing exact continuation", got, e)
	}
	current = "waiting"
	if _, e = s.LiveContinuationContext(ctx, b); e == nil {
		t.Fatal("stale approval resumed")
	}
	current = "resume"
	foreign := b
	foreign.DefinitionSHA256 = strings.Repeat("d", 64)
	if _, e = s.LiveContinuationContext(ctx, foreign); e == nil {
		t.Fatal("foreign session resumed")
	}
	s.Pool.Exec(ctx, `UPDATE gateway.executions SET resume_context='{"approvalId":"foreign","actionId":"scale-three","decision":"resume","guidance":""}' WHERE request_id=$1`, b.RequestID)
	if _, e = s.LiveContinuationContext(ctx, b); e == nil {
		t.Fatal("tampered continuation resumed")
	}
}
