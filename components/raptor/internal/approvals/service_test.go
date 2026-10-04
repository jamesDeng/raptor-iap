package approvals

import (
	"context"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/db"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/domain"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/testutil"
	"testing"
)

func TestBoundApproval(t *testing.T) {
	ctx := context.Background()
	p := testutil.Database(t)
	if e := db.Migrate(ctx, p); e != nil {
		t.Fatal(e)
	}
	id, user := domain.NewID(), domain.User{ID: domain.NewID(), Role: "user"}
	p.Exec(ctx, "INSERT INTO raptor.requests(id,creator_id,idempotency_key,input_hash,definition,schema_hash) VALUES($1,$2,'test','hash','{}','schema')", id, user.ID)
	s := &Service{Pool: p}
	in := ApprovalInput{RequestID: id, ActionID: domain.NewID(), Interface: "ess.scale-in", EnvCode: "adev", Target: map[string]any{"groupId": "synthetic"}, Parameters: map[string]any{"desiredCapacity": float64(1)}}
	a, e := s.Request(ctx, in)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.Decide(ctx, user, a.ID, DecisionInput{Decision: "approve"}); e != nil {
		t.Fatal(e)
	}
	v, e := s.Check(ctx, in)
	if e != nil || !v.Allowed {
		t.Fatal("matching approval denied")
	}
	in.Parameters = map[string]any{"desiredCapacity": float64(0)}
	v, _ = s.Check(ctx, in)
	if v.Allowed {
		t.Fatal("approval widened")
	}
	in.Parameters = map[string]any{"desiredCapacity": float64(1)}
	in.ActionID = domain.NewID()
	v, _ = s.Check(ctx, in)
	if v.Allowed {
		t.Fatal("new action reused approval")
	}
	a, e = s.Request(ctx, in)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.Decide(ctx, user, a.ID, DecisionInput{Decision: "deny"}); e == nil {
		t.Fatal("denial missing guidance")
	}
	if _, e = s.Decide(ctx, user, a.ID, DecisionInput{Decision: "deny", Next: "block"}); e != nil {
		t.Fatal(e)
	}
}
