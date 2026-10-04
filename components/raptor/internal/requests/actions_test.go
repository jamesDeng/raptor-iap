package requests

import (
	"context"
	"testing"
)

func TestActionsPersistSignal(t *testing.T) {
	s, in, u := setup(t)
	ctx := context.Background()
	r, e := s.Create(ctx, u, "request", in)
	if e != nil {
		t.Fatal(e)
	}
	if e = s.Action(ctx, r.ID, "continue", ""); e == nil {
		t.Fatal("continue without guidance")
	}
	if e = s.Action(ctx, r.ID, "block", ""); e != nil {
		t.Fatal(e)
	}
	var n int
	s.Pool.QueryRow(ctx, "SELECT count(*) FROM raptor.outbox WHERE topic='signal'").Scan(&n)
	if n != 1 {
		t.Fatal("action signal missing")
	}
}
func TestDirectContinueRetriesFailedTargetsLocally(t *testing.T) {
	s, id := restartRequest(t)
	ctx := context.Background()
	client := &fakeRestart{calls: map[string]int{}, failure: "b"}
	s.RunRestartBatch(ctx, id, client)
	if e := s.Action(ctx, id, "continue", "Retry failed target"); e != nil {
		t.Fatal(e)
	}
	r, _ := s.Get(ctx, id)
	if r.Status != "queued" {
		t.Fatal("direct retry did not queue local runner")
	}
	var signals int
	s.Pool.QueryRow(ctx, "SELECT count(*) FROM raptor.outbox WHERE entity_id=$1 AND topic='signal'", id).Scan(&signals)
	if signals != 0 {
		t.Fatal("direct restart sent to agent Gateway")
	}
}
