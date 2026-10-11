package execution

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

type pauseDriver struct {
	liveDriver
	store         *Store
	binding       AttemptBinding
	checkpointErr bool
}

func (r *pauseDriver) Checkpoint(ctx context.Context, h RuntimeHandle) (VerifiedCheckpoint, error) {
	if r.checkpointErr {
		return VerifiedCheckpoint{}, ErrUnavailable
	}
	cp := SessionCheckpoint{RequestID: r.binding.RequestID, DefinitionSHA256: r.binding.DefinitionSHA256, SkillsCommit: r.binding.SkillsCommit, Model: r.binding.Model, SessionID: "session", SessionFile: "session.jsonl", SessionSHA256: strings.Repeat("c", 64), Archive: verifiedCheckpoint()}
	if err := r.store.SaveOperationSession(ctx, r.binding.AttemptID, "owner", cp); err != nil {
		return VerifiedCheckpoint{}, err
	}
	return cp.Archive, nil
}
func TestWorkerPauseReleasesOnlyAfterCheckpointCleanupAndRevoke(t *testing.T) {
	for _, kind := range []string{"clean", "cleanup-failure", "checkpoint-failure"} {
		t.Run(kind, func(t *testing.T) {
			s := testStore(t)
			ctx := context.Background()
			id := NewID()
			s.Receive(ctx, id)
			x, e := s.ClaimNext(ctx, "owner")
			if e != nil {
				t.Fatal(e)
			}
			b := AttemptBinding{RequestID: id, AttemptID: x.AttemptID, Operation: "db-proxy.replace-nodes", ObjectKind: "db-proxy", ObjectCode: "proxy", EnvCode: "rdev.ali", ClusterID: "cluster", SkillsCommit: strings.Repeat("b", 40), DefinitionSHA256: strings.Repeat("a", 64), Model: "gpt-5.6-luna"}
			if e = s.BindLive(ctx, b.AttemptID, "owner", b, b.DefinitionSHA256); e != nil {
				t.Fatal(e)
			}
			r := &pauseDriver{liveDriver: liveDriver{cleanup: kind != "cleanup-failure"}, store: s, binding: b, checkpointErr: kind == "checkpoint-failure"}
			a := &liveAccess{}
			w := &LiveWorker{Store: s, Owner: "owner", Lease: &fixtureLease{valid: true}, Runtime: r, Access: a}
			wait := LiveWait{Kind: "approval", ApprovalID: NewID(), ActionID: "scale-three", BindingDigest: strings.Repeat("d", 64), StartedAt: time.Now().UTC()}
			e = w.pause(ctx, b, RuntimeHandle{RequestID: id, ID: "sandbox"}, wait)
			active, _, readErr := s.ActiveOwner(ctx)
			if readErr != nil {
				t.Fatal(readErr)
			}
			if kind == "clean" {
				if e != nil || active != "" || !a.revoked {
					t.Fatal("clean pause did not release", e, active)
				}
				state, _ := s.Get(ctx, id)
				if state.Status != "waiting_approval" {
					t.Fatal(state.Status)
				}
			} else if kind == "cleanup-failure" {
				if e == nil || active != id || !a.revoked {
					t.Fatal("unsafe cleanup released slot", e, active)
				}
			} else {
				if active != "" && active != id {
					t.Fatal("foreign slot")
				}
				if !a.revoked {
					t.Fatal("checkpoint failure retained active credential")
				}
			}
		})
	}
}
func TestWorkerRestartRecoversWaitWithoutModelReplay(t *testing.T) {
	s, x, b := liveAttempt(t)
	ctx := context.Background()
	b.DefinitionSHA256 = strings.Repeat("a", 64)
	raw, _ := json.Marshal(b)
	s.Pool.Exec(ctx, "UPDATE gateway.attempts SET binding=$2,definition_sha256=$3 WHERE id=$1", x.AttemptID, raw, b.DefinitionSHA256)
	cp := SessionCheckpoint{RequestID: b.RequestID, DefinitionSHA256: b.DefinitionSHA256, SkillsCommit: b.SkillsCommit, Model: b.Model, SessionID: "session", SessionFile: "session.jsonl", SessionSHA256: strings.Repeat("b", 64), Archive: verifiedCheckpoint()}
	wait := LiveWait{Kind: "approval", ApprovalID: NewID(), ActionID: "scale-three", BindingDigest: strings.Repeat("c", 64), StartedAt: time.Now().UTC()}
	if e := s.SaveLiveWait(ctx, x.AttemptID, "owner", wait, cp); e != nil {
		t.Fatal(e)
	}
	s.ReleaseLiveWait(ctx, x.AttemptID, "owner", LiveCleanup{false, true}, true)
	r := &liveDriver{cleanup: true}
	a := &liveAccess{}
	w := &LiveWorker{Store: s, Owner: "new-owner", Lease: &fixtureLease{valid: true}, Runtime: r, Access: a}
	if e := w.Reconcile(ctx); e != nil {
		t.Fatal(e)
	}
	state, e := s.Get(ctx, b.RequestID)
	if e != nil || state.Status != "waiting_approval" || state.RecoveryNeeded || r.startCount != 0 || !a.revoked {
		t.Fatal("wait lost or runtime replayed", e, state.Status)
	}
	saved, e := s.OperationSession(ctx, b)
	if e != nil || saved == nil || saved.SessionID != "session" {
		t.Fatal("wait session lost", e)
	}
}
