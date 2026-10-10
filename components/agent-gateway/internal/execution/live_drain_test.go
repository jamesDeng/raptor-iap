package execution

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestLiveDrainRetainsEarlyEventAndQueuesExactlyOnce(t *testing.T) {
	s, x, b := liveAttempt(t)
	ctx := context.Background()
	b.Operation = "db-proxy.replace-nodes"
	b.ObjectKind = "db-proxy"
	b.ClusterID = "cluster"
	b.DefinitionSHA256 = strings.Repeat("a", 64)
	raw, _ := json.Marshal(b)
	s.Pool.Exec(ctx, "UPDATE gateway.attempts SET binding=$2,definition_sha256=$3 WHERE id=$1", x.AttemptID, raw, b.DefinitionSHA256)
	wait := LiveWait{Kind: "approval", ApprovalID: NewID(), ActionID: "scale-three", BindingDigest: strings.Repeat("c", 64), StartedAt: time.Now().UTC()}
	payload, _ := json.Marshal(map[string]string{"approvalId": wait.ApprovalID})
	// Unrelated old approval events must not exhaust the bounded wakeup batch.
	for j := 0; j < 51; j++ {
		foreign, _ := json.Marshal(map[string]string{"approvalId": NewID()})
		if _, err := s.Pool.Exec(ctx, "INSERT INTO gateway.signals(id,request_id,kind,payload) VALUES($1,$2,'approval',$3)", NewID(), b.RequestID, foreign); err != nil {
			t.Fatal(err)
		}
	}
	sid := NewID()
	if e := s.DeliverSignal(ctx, Signal{ID: sid, RequestID: b.RequestID, Kind: "approval", Payload: payload}); e != nil {
		t.Fatal(e)
	}
	calls := 0
	next := "waiting"
	s.ResolveLiveDecision = func(context.Context, string, LiveWait) (LiveContinuation, error) {
		calls++
		return LiveContinuation{Next: next}, nil
	}
	w := &LiveWorker{Store: s, Owner: "owner", Lease: &fixtureLease{valid: true}}
	if e := w.DrainSignals(ctx); e != nil || calls != 0 {
		t.Fatal("early wakeup resolved", e)
	}
	cp := SessionCheckpoint{RequestID: b.RequestID, DefinitionSHA256: b.DefinitionSHA256, SkillsCommit: b.SkillsCommit, Model: b.Model, SessionID: "session", SessionFile: "session.jsonl", SessionSHA256: strings.Repeat("d", 64), Archive: verifiedCheckpoint()}
	if e := s.SaveLiveWait(ctx, x.AttemptID, "owner", wait, cp); e != nil {
		t.Fatal(e)
	}
	if e := w.DrainSignals(ctx); e != nil || calls != 0 {
		t.Fatal("checkpointing event resolved", e)
	}
	if e := s.ReleaseLiveWait(ctx, x.AttemptID, "owner", LiveCleanup{true, true}, true); e != nil {
		t.Fatal(e)
	}
	if e := w.DrainSignals(ctx); e != nil || calls != 1 {
		t.Fatal(e, calls)
	}
	next = "resume"
	if e := w.DrainSignals(ctx); e != nil || calls != 2 {
		t.Fatal(e, calls)
	}
	if e := w.DrainSignals(ctx); e != nil || calls != 2 {
		t.Fatal("duplicate inference scheduled", e, calls)
	}
	state, e := s.Get(ctx, b.RequestID)
	if e != nil || state.Status != "queued" {
		t.Fatal(e, state.Status)
	}
}

func TestResourceWaitResumesAutomaticallyWithoutApprovalAndOnlyAfterCleanup(t *testing.T) {
	s, x, b := liveAttempt(t)
	ctx := context.Background()
	b.Operation = "db-proxy.replace-nodes"
	b.ObjectKind = "db-proxy"
	b.ClusterID = "cluster"
	b.DefinitionSHA256 = strings.Repeat("a", 64)
	raw, _ := json.Marshal(b)
	s.Pool.Exec(ctx, "UPDATE gateway.attempts SET binding=$2,definition_sha256=$3 WHERE id=$1", x.AttemptID, raw, b.DefinitionSHA256)
	wait := LiveWait{Kind: "resource", ActionID: "capacity-wait", BindingDigest: b.DefinitionSHA256, StartedAt: time.Now().Add(-61 * time.Second).UTC(), WakeAfterSeconds: 60}
	cp := SessionCheckpoint{RequestID: b.RequestID, DefinitionSHA256: b.DefinitionSHA256, SkillsCommit: b.SkillsCommit, Model: b.Model, SessionID: "session", SessionFile: "session.jsonl", SessionSHA256: strings.Repeat("d", 64), Archive: verifiedCheckpoint()}
	if e := s.SaveLiveWait(ctx, x.AttemptID, "owner", wait, cp); e != nil {
		t.Fatal(e)
	}
	calls := 0
	s.ResolveLiveDecision = func(_ context.Context, _ string, w LiveWait) (LiveContinuation, error) {
		calls++
		if w.Kind != "resource" || w.ApprovalID != "" {
			t.Fatal("readiness became approval")
		}
		return LiveContinuation{Next: "resume"}, nil
	}
	w := &LiveWorker{Store: s, Owner: "owner", Lease: &fixtureLease{valid: true}}
	if e := w.DrainSignals(ctx); e != nil || calls != 0 {
		t.Fatal("resumed before cleanup", e)
	}
	if e := s.ReleaseLiveWait(ctx, x.AttemptID, "owner", LiveCleanup{true, true}, true); e != nil {
		t.Fatal(e)
	}
	if e := w.DrainSignals(ctx); e != nil || calls != 1 {
		t.Fatal("automatic wake failed", e, calls)
	}
	state, e := s.Get(ctx, b.RequestID)
	if e != nil || state.Status != "queued" {
		t.Fatal("request not queued", e, state.Status)
	}
	if e := w.DrainSignals(ctx); e != nil || calls != 1 {
		t.Fatal("duplicate model segment", e, calls)
	}
}
