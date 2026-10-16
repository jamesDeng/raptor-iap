package execution

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestReleasedQualificationPauseAcknowledgesWithoutNewSignal(t *testing.T) {
	s, x, b := liveAttempt(t)
	ctx := context.Background()
	b.Operation = "db-proxy.replace-nodes"
	b.ObjectKind = "db-proxy"
	b.ClusterID = "cluster"
	b.ExecutionMode = "qualification"
	b.DefinitionSHA256 = strings.Repeat("a", 64)
	raw, _ := json.Marshal(b)
	if _, err := s.Pool.Exec(ctx, "UPDATE gateway.attempts SET binding=$2,definition_sha256=$3 WHERE id=$1", x.AttemptID, raw, b.DefinitionSHA256); err != nil {
		t.Fatal(err)
	}
	pause := Signal{ID: NewID(), RequestID: b.RequestID, Kind: "pause", Payload: json.RawMessage(`{"reason":"waiting_approval"}`)}
	if err := s.DeliverSignal(ctx, pause); err == nil {
		t.Fatal("pause accepted before checkpointed wait")
	}
	wait := LiveWait{Kind: "approval", ApprovalID: NewID(), ActionID: "db-proxy.replace-nodes", BindingDigest: strings.Repeat("c", 64), StartedAt: time.Now().UTC()}
	cp := SessionCheckpoint{RequestID: b.RequestID, DefinitionSHA256: b.DefinitionSHA256, SkillsCommit: b.SkillsCommit, Model: b.Model, SessionID: NewID(), SessionFile: "session.jsonl", SessionSHA256: strings.Repeat("d", 64), Archive: verifiedCheckpoint()}
	if err := s.SaveLiveWait(ctx, x.AttemptID, "owner", wait, cp); err != nil {
		t.Fatal(err)
	}
	if err := s.DeliverSignal(ctx, pause); err == nil {
		t.Fatal("pause accepted before cleanup and release")
	}
	if err := s.ReleaseLiveWait(ctx, x.AttemptID, "owner", LiveCleanup{SandboxAbsent: true, KeyAbsent: true}, true); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := s.DeliverSignal(ctx, pause); err != nil {
			t.Fatalf("released pause acknowledgement %d: %v", i, err)
		}
	}
	bad := pause
	bad.ID = NewID()
	bad.Payload = json.RawMessage(`{"reason":"waiting_review"}`)
	if err := s.DeliverSignal(ctx, bad); err == nil {
		t.Fatal("mismatched pause reason accepted")
	}
	var count int
	if err := s.Pool.QueryRow(ctx, "SELECT count(*) FROM gateway.signals WHERE request_id=$1 AND kind='pause'", b.RequestID).Scan(&count); err != nil || count != 0 {
		t.Fatal("pause acknowledgement queued a signal", err, count)
	}
	state, err := s.Get(ctx, b.RequestID)
	if err != nil || state.Status != "waiting_approval" || state.RecoveryNeeded {
		t.Fatal("pause acknowledgement changed released wait", err, state.Status)
	}
}
