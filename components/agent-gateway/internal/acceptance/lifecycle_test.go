package acceptance

import (
	"context"
	"encoding/json"
	"github.com/jamesDeng/raptor-iap/components/agent-gateway/internal/db"
	"github.com/jamesDeng/raptor-iap/components/agent-gateway/internal/execution"
	runtimeadapter "github.com/jamesDeng/raptor-iap/components/agent-gateway/internal/runtime"
	"github.com/jamesDeng/raptor-iap/components/agent-gateway/internal/testutil"
	"strings"
	"testing"
)

type fixtureRaptor struct{ version execution.SkillsVersion }

func (f *fixtureRaptor) Context(ctx context.Context, id string) (execution.ExecutionInput, error) {
	return execution.ExecutionInput{RequestID: id, Definition: json.RawMessage(`{"type":"agent","operations":[]}`), Environment: json.RawMessage(`{"code":"synthetic"}`), Skills: f.version}, nil
}
func TestLocalPauseRestoreAcceptance(t *testing.T) {
	ctx := context.Background()
	p := testutil.Database(t)
	if e := db.Migrate(ctx, p); e != nil {
		t.Fatal(e)
	}
	store := &execution.Store{Pool: p}
	id := execution.NewID()
	store.Receive(ctx, id)
	runtime := &runtimeadapter.Simulated{Root: t.TempDir()}
	raptor := &fixtureRaptor{version: execution.SkillsVersion{Tag: "skills-v0.1.0", CommitSHA: strings.Repeat("a", 40)}}
	worker := &execution.Worker{Store: store, Runtime: runtime, Raptor: raptor, Owner: "fixture-worker", SimulatedFlow: true}
	if e := worker.RunNext(ctx); e != nil {
		t.Fatal(e)
	}
	x, _ := store.Get(ctx, id)
	if x.Status != "waiting_approval" {
		t.Fatal("no approval pause")
	}
	store.DeliverSignal(ctx, execution.Signal{ID: execution.NewID(), RequestID: id, Kind: "approval", Payload: []byte(`{"decision":"approved"}`)})
	if e := worker.DrainSignals(ctx); e != nil {
		t.Fatal(e)
	}
	if e := worker.RunActive(ctx); e != nil {
		t.Fatal(e)
	}
	x, _ = store.Get(ctx, id)
	if x.Status != "waiting_review" || x.Checkpoint.RequestID != id || x.Cleanup != "confirmed" {
		t.Fatal("review did not release request checkpoint")
	}
	raptor.version = execution.SkillsVersion{Tag: "skills-v0.2.0", CommitSHA: strings.Repeat("b", 40)}
	body, _ := json.Marshal(map[string]any{"version": raptor.version, "strategy": "interrupt"})
	if e := worker.ApplySkillsChange(ctx, x, execution.Signal{RequestID: id, Kind: "skills", Payload: body}); e != nil {
		t.Fatal(e)
	}
	restored := &runtimeadapter.Simulated{Root: runtime.Root}
	worker.Runtime = restored
	if e := worker.RunNext(ctx); e != nil {
		t.Fatal(e)
	}
	x, _ = store.Get(ctx, id)
	if x.AppliedSkills.Tag != "skills-v0.2.0" || x.Checkpoint.RequestID != id {
		t.Fatal("restored wrong request or skills")
	}
	store.DeliverSignal(ctx, execution.Signal{ID: execution.NewID(), RequestID: id, Kind: "merged", Payload: []byte(`{}`)})
	worker.DrainSignals(ctx)
	worker.RunNext(ctx)
	x, _ = store.Get(ctx, id)
	if x.Status != "completed" {
		t.Fatal("merge continuation did not complete fixture")
	}
	events, e := (&execution.Store{Pool: p}).Events(ctx, id, 0)
	if e != nil || len(events) == 0 {
		t.Fatal("saved progress absent")
	}
	for i, event := range events {
		if event.Sequence != int64(i+1) || event.EvidenceMode != "simulated" {
			t.Fatal("incorrect receipt")
		}
	}
	if runtime.MaxActive > 1 || restored.MaxActive > 1 {
		t.Fatal("overlapping simulated runtime")
	}
}
