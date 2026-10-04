package execution

import (
	"context"
	"testing"
)

func TestSkillsNextPauseAndInterrupt(t *testing.T) {
	w, x, _, runtime := active(t)
	ctx := context.Background()
	signal := Signal{RequestID: x.RequestID, Kind: "skills", Payload: []byte(`{"version":{"tag":"skills-v1.0.0","commitSha":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},"strategy":"next-pause"}`)}
	if e := w.ApplySkillsChange(ctx, x, signal); e != nil {
		t.Fatal(e)
	}
	v, _ := w.Store.Get(ctx, x.RequestID)
	if v.AppliedSkills.Tag != "" || runtime.stops != 0 {
		t.Fatal("changed skills while running")
	}
	if e := w.Pause(ctx, x, "waiting_review"); e != nil {
		t.Fatal(e)
	}
	v, _ = w.Store.Get(ctx, x.RequestID)
	if v.AppliedSkills.Tag != "skills-v1.0.0" || v.Checkpoint.RequestID != x.RequestID {
		t.Fatal("skills not applied at pause")
	}
}
func TestSkillsInterruptCleanupFailure(t *testing.T) {
	w, x, _, runtime := active(t)
	runtime.stopFails = true
	ctx := context.Background()
	e := w.ApplySkillsChange(ctx, x, Signal{RequestID: x.RequestID, Payload: []byte(`{"version":{"tag":"skills-v1.0.0","commitSha":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},"strategy":"interrupt"}`)})
	if e == nil {
		t.Fatal("failed cleanup permitted replacement")
	}
	v, _ := w.Store.Get(ctx, x.RequestID)
	if !v.RecoveryNeeded || v.Status != "blocked" {
		t.Fatal("replacement queued despite unresolved cleanup")
	}
}
func TestSkillsCompleteBeforeNextPauseDoesNotRestart(t *testing.T) {
	w, x, _, _ := active(t)
	ctx := context.Background()
	w.ApplySkillsChange(ctx, x, Signal{RequestID: x.RequestID, Payload: []byte(`{"version":{"tag":"skills-v1.0.0","commitSha":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},"strategy":"next-pause"}`)})
	w.Store.Release(ctx, x.RequestID, w.Owner, "completed", CheckpointRef{RequestID: x.RequestID, Path: "inert"})
	v, _ := w.Store.Get(ctx, x.RequestID)
	if e := w.ApplySkillsChange(ctx, v, Signal{RequestID: x.RequestID, Payload: []byte(`{"version":{"tag":"skills-v1.0.0","commitSha":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},"strategy":"next-pause"}`)}); e != nil {
		t.Fatal(e)
	}
	v, _ = w.Store.Get(ctx, x.RequestID)
	if v.Status != "completed" {
		t.Fatal("completed request restarted")
	}
}
