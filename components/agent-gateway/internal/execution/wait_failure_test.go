package execution

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestCheckpointReverificationPreservesFirstReceipt(t *testing.T) {
	s, _, b := liveAttempt(t)
	ctx := context.Background()
	first := verifiedCheckpoint()
	if err := s.SaveLiveCheckpoint(ctx, b.AttemptID, "owner", first); err != nil {
		t.Fatal(err)
	}
	again := first
	again.VerifiedAt = first.VerifiedAt.Add(time.Second)
	if err := s.SaveLiveCheckpoint(ctx, b.AttemptID, "owner", again); err != nil {
		t.Fatalf("identical archive reverified: %v", err)
	}
	record, err := s.RecoveryRecord(ctx, b.AttemptID)
	if err != nil || record.Checkpoint != first {
		t.Fatal("first receipt changed", err)
	}
	for _, field := range []string{"hash", "key", "bytes", "encryption"} {
		bad := again
		switch field {
		case "hash":
			bad.SHA256 = strings.Repeat("f", 64)
		case "key":
			bad.ArchiveKey = "other/archive.tgz"
		case "bytes":
			bad.Bytes++
		case "encryption":
			bad.Encryption = "other"
		}
		if s.SaveLiveCheckpoint(ctx, b.AttemptID, "owner", bad) == nil {
			t.Fatal("changed archive accepted", field)
		}
	}
}

func TestWaitActionCollisionCleansAndFinalizes(t *testing.T) {
	s, x, b := liveAttempt(t)
	ctx := context.Background()
	b.Operation = "db-proxy.replace-nodes"
	b.ObjectKind = "db-proxy"
	b.ExecutionMode = "qualification"
	b.ClusterID = "cluster"
	b.DefinitionSHA256 = strings.Repeat("a", 64)
	raw, _ := json.Marshal(b)
	if _, err := s.Pool.Exec(ctx, "UPDATE gateway.attempts SET binding=$2,definition_sha256=$3 WHERE id=$1", b.AttemptID, raw, b.DefinitionSHA256); err != nil {
		t.Fatal(err)
	}
	cp := SessionCheckpoint{RequestID: b.RequestID, DefinitionSHA256: b.DefinitionSHA256, SkillsCommit: b.SkillsCommit, Model: b.Model, SessionID: "session", SessionFile: "session.jsonl", SessionSHA256: strings.Repeat("b", 64), Archive: verifiedCheckpoint()}
	if err := s.SaveOperationSession(ctx, b.AttemptID, "owner", cp); err != nil {
		t.Fatal(err)
	}
	prior := NewID()
	if _, err := s.Pool.Exec(ctx, "INSERT INTO gateway.attempts(id,request_id,state,ended_at) VALUES($1,$2,'waiting_resource',now())", prior, b.RequestID); err != nil {
		t.Fatal(err)
	}
	old := LiveWait{Kind: "resource", ActionID: "reused-resource-action", BindingDigest: b.DefinitionSHA256, StartedAt: time.Now().Add(-time.Minute), WakeAfterSeconds: 60}
	wr, _ := json.Marshal(old)
	cr, _ := json.Marshal(cp)
	if _, err := s.Pool.Exec(ctx, "INSERT INTO gateway.live_waits(attempt_id,request_id,approval_id,action_id,wait,checkpoint,phase) VALUES($1,$2,NULL,$3,$4,$5,'resumed')", prior, b.RequestID, old.ActionID, wr, cr); err != nil {
		t.Fatal(err)
	}
	access := &liveAccess{}
	driver := &liveDriver{cleanup: true}
	w := LiveWorker{Store: s, Owner: "owner", Lease: &fixtureLease{valid: true}, Runtime: driver, Access: access}
	next := LiveWait{Kind: "approval", ApprovalID: NewID(), ActionID: old.ActionID, BindingDigest: strings.Repeat("d", 64), StartedAt: time.Now().UTC()}
	if err := w.pause(ctx, b, RuntimeHandle{RequestID: b.RequestID}, next); err != nil {
		t.Fatalf("wait conflict stranded attempt: %v", err)
	}
	got, err := s.Get(ctx, x.RequestID)
	if err != nil || got.Status != "failed" || got.FailureCode != "InvalidApprovalPause" || got.RecoveryNeeded || !access.revoked {
		t.Fatalf("not settled safely: %#v %v", got, err)
	}
	id, _, err := s.ActiveOwner(ctx)
	if err != nil || id != "" {
		t.Fatal("slot retained after confirmed cleanup", id, err)
	}
}
