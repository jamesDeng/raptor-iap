package execution

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestLiveWaitRequiresMatchingCheckpointAndKeepsSlot(t *testing.T) {
	s, x, b := liveAttempt(t)
	ctx := context.Background()
	b.DefinitionSHA256 = strings.Repeat("a", 64)
	raw, _ := json.Marshal(b)
	if _, e := s.Pool.Exec(ctx, "UPDATE gateway.attempts SET binding=$2,definition_sha256=$3 WHERE id=$1", x.AttemptID, raw, b.DefinitionSHA256); e != nil {
		t.Fatal(e)
	}
	cp := SessionCheckpoint{RequestID: b.RequestID, DefinitionSHA256: b.DefinitionSHA256, SkillsCommit: b.SkillsCommit, Model: b.Model, SessionID: NewID(), SessionFile: "session.jsonl", SessionSHA256: strings.Repeat("b", 64), Archive: VerifiedCheckpoint{ArchiveKey: "request/a.tgz", ChecksumKey: "request/a.sha256", SHA256: strings.Repeat("c", 64), Bytes: 10, PiVersion: "0.99.2", Encryption: "AES256", VerifiedAt: time.Now()}}
	wait := LiveWait{Kind: "approval", ApprovalID: NewID(), ActionID: "scale-three", BindingDigest: strings.Repeat("d", 64), StartedAt: time.Now().UTC()}
	if e := s.SaveLiveWait(ctx, x.AttemptID, "stale", wait, cp); e == nil {
		t.Fatal("stale owner recorded wait")
	}
	bad := cp
	bad.RequestID = NewID()
	if e := s.SaveLiveWait(ctx, x.AttemptID, "owner", wait, bad); e == nil {
		t.Fatal("foreign checkpoint accepted")
	}
	for i := 0; i < 2; i++ {
		if e := s.SaveLiveWait(ctx, x.AttemptID, "owner", wait, cp); e != nil {
			t.Fatal(e)
		}
	}
	altered := wait
	altered.ActionID = "scale-two"
	if e := s.SaveLiveWait(ctx, x.AttemptID, "owner", altered, cp); e == nil {
		t.Fatal("wait identity overwritten")
	}
	id, owner, e := s.ActiveOwner(ctx)
	if e != nil || id != b.RequestID || owner != "owner" {
		t.Fatal("slot released before confirmed cleanup")
	}
	if e = s.ReleaseLiveWait(ctx, x.AttemptID, "owner", LiveCleanup{SandboxAbsent: false, KeyAbsent: true}, true); e == nil {
		t.Fatal("unconfirmed cleanup released wait")
	}
	id, owner, e = s.ActiveOwner(ctx)
	if e != nil || id != b.RequestID || owner != "owner" {
		t.Fatal("failed cleanup released slot")
	}
	state, e := s.Get(ctx, b.RequestID)
	if e != nil || !state.RecoveryNeeded {
		t.Fatal("cleanup failure not recoverable", e)
	}
	if e = s.ReleaseLiveWait(ctx, x.AttemptID, "owner", LiveCleanup{SandboxAbsent: true, KeyAbsent: true}, true); e != nil {
		t.Fatal(e)
	}
	id, _, e = s.ActiveOwner(ctx)
	if e != nil || id != "" {
		t.Fatal("confirmed cleanup retained slot", e)
	}
	saved, e := s.OperationSession(ctx, b)
	if e != nil || saved == nil || saved.SessionID != cp.SessionID {
		t.Fatal("released wait lost session", e)
	}
	if e = s.ReleaseLiveWait(ctx, x.AttemptID, "owner", LiveCleanup{SandboxAbsent: true, KeyAbsent: true}, true); e != nil {
		t.Fatal("idempotent release failed", e)
	}
	var count int
	if e = s.Pool.QueryRow(ctx, "SELECT count(*) FROM gateway.live_waits WHERE request_id=$1", b.RequestID).Scan(&count); e != nil || count != 1 {
		t.Fatal("duplicate wait", e)
	}
}
func TestLiveResumeRequiresReleasedWaitAndFreshDecision(t *testing.T) {
	s, x, b := liveAttempt(t)
	ctx := context.Background()
	b.DefinitionSHA256 = strings.Repeat("a", 64)
	raw, _ := json.Marshal(b)
	s.Pool.Exec(ctx, "UPDATE gateway.attempts SET binding=$2,definition_sha256=$3 WHERE id=$1", x.AttemptID, raw, b.DefinitionSHA256)
	cp := SessionCheckpoint{RequestID: b.RequestID, DefinitionSHA256: b.DefinitionSHA256, SkillsCommit: b.SkillsCommit, Model: b.Model, SessionID: NewID(), SessionFile: "session.jsonl", SessionSHA256: strings.Repeat("b", 64), Archive: VerifiedCheckpoint{ArchiveKey: "request/a.tgz", ChecksumKey: "request/a.sha256", SHA256: strings.Repeat("c", 64), Bytes: 10, PiVersion: "0.99.2", Encryption: "AES256", VerifiedAt: time.Now()}}
	wait := LiveWait{Kind: "approval", ApprovalID: NewID(), ActionID: "scale-three", BindingDigest: strings.Repeat("d", 64), StartedAt: time.Now().UTC()}
	if e := s.SaveLiveWait(ctx, x.AttemptID, "owner", wait, cp); e != nil {
		t.Fatal(e)
	}
	signal := NewID()
	payload, _ := json.Marshal(map[string]string{"approvalId": wait.ApprovalID, "decision": "approved"})
	if _, e := s.Pool.Exec(ctx, "INSERT INTO gateway.signals(id,request_id,kind,payload) VALUES($1,$2,'approval',$3)", signal, b.RequestID, payload); e != nil {
		t.Fatal(e)
	}
	current := "waiting"
	calls := 0
	s.ResolveLiveDecision = func(context.Context, string, LiveWait) (LiveContinuation, error) {
		calls++
		return LiveContinuation{Next: current}, nil
	}
	if resumed, e := s.ResumeLive(ctx, b.RequestID, signal); e != nil || resumed || calls != 0 {
		t.Fatal("resumed before cleanup", e)
	}
	if e := s.ReleaseLiveWait(ctx, x.AttemptID, "owner", LiveCleanup{SandboxAbsent: true, KeyAbsent: true}, true); e != nil {
		t.Fatal(e)
	}
	if resumed, e := s.ResumeLive(ctx, b.RequestID, signal); e != nil || resumed {
		t.Fatal("signal payload authorized pending decision", e)
	}
	current = "resume"
	if resumed, e := s.ResumeLive(ctx, b.RequestID, signal); e != nil || !resumed {
		t.Fatal("matching decision not queued", e)
	}
	before := calls
	for i := 0; i < 2; i++ {
		if resumed, e := s.ResumeLive(ctx, b.RequestID, signal); e != nil || resumed {
			t.Fatal("duplicate resume", e)
		}
	}
	if calls != before {
		t.Fatal("duplicate signal repeated resolution")
	}
	secondSignal := NewID()
	if _, e := s.Pool.Exec(ctx, "INSERT INTO gateway.signals(id,request_id,kind,payload) VALUES($1,$2,'approval',$3)", secondSignal, b.RequestID, payload); e != nil {
		t.Fatal(e)
	}
	if resumed, e := s.ResumeLive(ctx, b.RequestID, secondSignal); e != nil || resumed {
		t.Fatal("second wakeup duplicated continuation", e)
	}
	state, e := s.Get(ctx, b.RequestID)
	if e != nil || state.Status != "queued" {
		t.Fatal("request not queued", e)
	}
}

func TestResourceWaitIsBoundedAndCannotRepresentApproval(t *testing.T) {
	w := LiveWait{Kind: "resource", ActionID: "capacity-wait", BindingDigest: strings.Repeat("c", 64), StartedAt: time.Now().UTC(), WakeAfterSeconds: 60}
	if !w.Valid() {
		t.Fatal("bounded resource wait refused")
	}
	w.ApprovalID = NewID()
	if w.Valid() {
		t.Fatal("resource wait falsely carries approval")
	}
	w.ApprovalID = ""
	w.WakeAfterSeconds = 3600
	if w.Valid() {
		t.Fatal("unbounded wait accepted")
	}
}
