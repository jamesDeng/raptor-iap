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
