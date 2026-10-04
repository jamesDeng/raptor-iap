package execution

import (
	"context"
	"errors"
	"testing"
	"time"
)

type fakeClock struct{ now time.Time }

func (c *fakeClock) Now() time.Time { return c.now }

type fakeRuntime struct {
	checkpointFails, stopFails bool
	checkpoints, stops         int
}

func (f *fakeRuntime) Start(context.Context, ExecutionInput) (RuntimeHandle, error) {
	return RuntimeHandle{}, nil
}
func (f *fakeRuntime) Restore(context.Context, ExecutionInput, CheckpointRef) (RuntimeHandle, error) {
	return RuntimeHandle{}, nil
}
func (f *fakeRuntime) Checkpoint(ctx context.Context, h RuntimeHandle) (CheckpointRef, error) {
	f.checkpoints++
	if f.checkpointFails {
		return CheckpointRef{}, errors.New("failed")
	}
	return CheckpointRef{RequestID: h.RequestID, Path: "inert.json"}, nil
}
func (f *fakeRuntime) Stop(context.Context, RuntimeHandle) (CleanupOutcome, error) {
	f.stops++
	return CleanupOutcome{Confirmed: !f.stopFails}, nil
}
func active(t *testing.T) (*Worker, Execution, *fakeClock, *fakeRuntime) {
	s := testStore(t)
	ctx := context.Background()
	s.Receive(ctx, NewID())
	x, e := s.ClaimNext(ctx, "worker")
	if e != nil {
		t.Fatal(e)
	}
	s.Pool.Exec(ctx, "UPDATE gateway.attempts SET runtime_id='runtime' WHERE id=$1", x.AttemptID)
	clock := &fakeClock{now: time.Unix(1000, 0)}
	runtime := &fakeRuntime{}
	return &Worker{Store: s, Owner: "worker", Clock: clock, Runtime: runtime}, *x, clock, runtime
}
func TestPauseApprovalDeadline(t *testing.T) {
	w, x, clock, runtime := active(t)
	ctx := context.Background()
	if e := w.Pause(ctx, x, "waiting_approval"); e != nil {
		t.Fatal(e)
	}
	clock.now = clock.now.Add(119 * time.Second)
	if e := w.ExpireApproval(ctx, x.RequestID); e != nil {
		t.Fatal(e)
	}
	if runtime.stops != 0 {
		t.Fatal("released before deadline")
	}
	clock.now = clock.now.Add(time.Second)
	if e := w.ExpireApproval(ctx, x.RequestID); e != nil {
		t.Fatal(e)
	}
	if runtime.stops != 1 || runtime.checkpoints != 1 {
		t.Fatal("deadline failed to release")
	}
}
func TestPauseReviewAndCleanupFailure(t *testing.T) {
	w, x, _, runtime := active(t)
	ctx := context.Background()
	runtime.stopFails = true
	if e := w.Pause(ctx, x, "waiting_review"); e != nil {
		t.Fatal(e)
	}
	v, _ := w.Store.Get(ctx, x.RequestID)
	if !v.RecoveryNeeded || v.Status != "blocked" {
		t.Fatal("failed cleanup not blocked")
	}
	w.Store.Receive(ctx, NewID())
	if next, _ := w.Store.ClaimNext(ctx, "other"); next != nil {
		t.Fatal("failed cleanup released slot")
	}
}
func TestSignalDuringCleanupRetained(t *testing.T) {
	w, x, _, _ := active(t)
	ctx := context.Background()
	signal := Signal{ID: NewID(), RequestID: x.RequestID, Kind: "continue", Payload: []byte(`{"instructions":"resume"}`)}
	for i := 0; i < 2; i++ {
		if e := w.Store.DeliverSignal(ctx, signal); e != nil {
			t.Fatal(e)
		}
	}
	var n int
	w.Store.Pool.QueryRow(ctx, "SELECT count(*) FROM gateway.signals WHERE NOT consumed").Scan(&n)
	if n != 1 {
		t.Fatal("signal lost or duplicated")
	}
}
func TestCancelRetainsHistory(t *testing.T) {
	w, x, _, runtime := active(t)
	ctx := context.Background()
	signal := Signal{ID: NewID(), RequestID: x.RequestID, Kind: "cancel", Payload: []byte(`{}`)}
	if e := w.ApplySignal(ctx, x, signal); e != nil {
		t.Fatal(e)
	}
	v, _ := w.Store.Get(ctx, x.RequestID)
	if v.Status != "cancelled" || runtime.stops != 1 {
		t.Fatal("cancel failed")
	}
	if e := w.ApplySignal(ctx, v, Signal{ID: NewID(), RequestID: x.RequestID, Kind: "continue", Payload: []byte(`{"instructions":"resume"}`)}); e == nil {
		t.Fatal("cancelled request resumed")
	}
}
