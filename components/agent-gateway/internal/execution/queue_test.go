package execution

import (
	"context"
	"github.com/jamesDeng/raptor-iap/components/agent-gateway/internal/db"
	"github.com/jamesDeng/raptor-iap/components/agent-gateway/internal/testutil"
	"sync"
	"testing"
)

func testStore(t *testing.T) *Store {
	p := testutil.Database(t)
	if e := db.Migrate(context.Background(), p); e != nil {
		t.Fatal(e)
	}
	return &Store{Pool: p}
}
func TestIdempotentReceive(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	id := NewID()
	for i := 0; i < 2; i++ {
		if _, e := s.Receive(ctx, id); e != nil {
			t.Fatal(e)
		}
	}
	var n int
	s.Pool.QueryRow(ctx, "SELECT count(*) FROM gateway.executions").Scan(&n)
	if n != 1 {
		t.Fatal("duplicate execution")
	}
}
func TestSingleRuntimeOwner(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	s.Receive(ctx, NewID())
	s.Receive(ctx, NewID())
	var wg sync.WaitGroup
	var mu sync.Mutex
	n := 0
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			x, e := s.ClaimNext(ctx, NewID())
			if e != nil {
				t.Error(e)
			}
			if x != nil {
				mu.Lock()
				n++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if n != 1 {
		t.Fatalf("claimed %d runtimes", n)
	}
}
func TestUnresolvedOwnerBlocksDispatch(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	s.Receive(ctx, NewID())
	s.Receive(ctx, NewID())
	s.ClaimNext(ctx, "previous-process")
	restarted := &Store{Pool: s.Pool}
	x, e := restarted.ClaimNext(ctx, "new-process")
	if e != nil || x != nil {
		t.Fatal("unresolved runtime replayed")
	}
}
func TestProgressSurvivesRestart(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	id := NewID()
	s.Receive(ctx, id)
	event := ProgressEvent{EventID: NewID(), RequestID: id, Kind: "progress", Summary: "Simulated progress", EvidenceMode: "simulated"}
	for i := 0; i < 2; i++ {
		if e := s.AppendEvent(ctx, event); e != nil {
			t.Fatal(e)
		}
	}
	x, e := (&Store{Pool: s.Pool}).Events(ctx, id, 0)
	if e != nil || len(x) != 1 || x[0].Sequence != 1 {
		t.Fatal("progress lost or duplicated")
	}
}
