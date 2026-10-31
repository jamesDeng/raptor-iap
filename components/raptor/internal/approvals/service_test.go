package approvals

import (
	"context"
	"encoding/json"
	"github.com/jackc/pgx/v5"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/db"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/domain"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/testutil"
	"testing"
)

func TestApprovalQueuesWaitingRequestAndRetiresPendingPause(t *testing.T) {
	ctx := context.Background()
	p := testutil.Database(t)
	if err := db.Migrate(ctx, p); err != nil {
		t.Fatal(err)
	}
	id, user := domain.NewID(), domain.User{ID: domain.NewID(), Role: "user"}
	if _, err := p.Exec(ctx, `INSERT INTO raptor.requests(id,creator_id,idempotency_key,input_hash,definition,schema_hash,status) VALUES($1,$2,'resume','hash','{"type":"agent","envCode":"adev"}','schema','waiting_approval')`, id, user.ID); err != nil {
		t.Fatal(err)
	}
	s := &Service{Pool: p, VerifyResource: func(context.Context, ApprovalInput) (ResourceVerification, error) {
		return func(context.Context, pgx.Tx) error { return nil }, nil
	}}
	a, err := s.Request(ctx, ApprovalInput{RequestID: id, ActionID: domain.NewID(), Interface: "runtime.qualification-resume", EnvCode: "adev", Target: map[string]any{"proxyCode": "proxy"}, Parameters: map[string]any{"phase": "resume"}})
	if err != nil {
		t.Fatal(err)
	}
	pauseID := domain.NewID()
	pause, _ := json.Marshal(map[string]any{"signalId": pauseID, "kind": "pause", "payload": map[string]string{"reason": "waiting_approval"}})
	if _, err = p.Exec(ctx, "INSERT INTO raptor.outbox(id,topic,entity_id,payload) VALUES($1,'signal',$2,$3)", pauseID, id, pause); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Decide(ctx, user, a.ID, DecisionInput{Decision: "approve"}); err != nil {
		t.Fatal(err)
	}
	var status, reason string
	if err = p.QueryRow(ctx, "SELECT status FROM raptor.requests WHERE id=$1", id).Scan(&status); err != nil || status != "queued" {
		t.Fatalf("approved request status=%q err=%v", status, err)
	}
	var pending bool
	if err = p.QueryRow(ctx, "SELECT approval_resume_pending FROM raptor.requests WHERE id=$1", id).Scan(&pending); err != nil || !pending {
		t.Fatalf("approved resume marker=%v err=%v", pending, err)
	}
	if err = p.QueryRow(ctx, "SELECT failed_reason FROM raptor.outbox WHERE id=$1", pauseID).Scan(&reason); err != nil || reason != "superseded approved pause" {
		t.Fatalf("old pause reason=%q err=%v", reason, err)
	}
}

func TestBoundApproval(t *testing.T) {
	ctx := context.Background()
	p := testutil.Database(t)
	if e := db.Migrate(ctx, p); e != nil {
		t.Fatal(e)
	}
	id, user := domain.NewID(), domain.User{ID: domain.NewID(), Role: "user"}
	p.Exec(ctx, `INSERT INTO raptor.requests(id,creator_id,idempotency_key,input_hash,definition,schema_hash) VALUES($1,$2,'test','hash','{"type":"agent","envCode":"adev"}','schema')`, id, user.ID)
	s := &Service{Pool: p, VerifyResource: func(context.Context, ApprovalInput) (ResourceVerification, error) {
		return func(context.Context, pgx.Tx) error { return nil }, nil
	}}
	in := ApprovalInput{RequestID: id, ActionID: domain.NewID(), Interface: "db-proxy.scale-in", EnvCode: "adev", Target: map[string]any{"proxyCode": "proxy"}, Parameters: map[string]any{"desiredCapacity": float64(1)}}
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
	p.Exec(ctx, "UPDATE raptor.requests SET status='blocked' WHERE id=$1", id)
	v, _ = s.Check(ctx, in)
	if v.Allowed {
		t.Fatal("blocked request retained infrastructure permission")
	}
	p.Exec(ctx, "UPDATE raptor.requests SET status='queued' WHERE id=$1", id)
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

func TestApprovalRejectsForeignRequestScope(t *testing.T) {
	ctx := context.Background()
	p := testutil.Database(t)
	if e := db.Migrate(ctx, p); e != nil {
		t.Fatal(e)
	}
	id := domain.NewID()
	p.Exec(ctx, `INSERT INTO raptor.requests(id,creator_id,idempotency_key,input_hash,definition,schema_hash) VALUES($1,$2,'scope','hash','{"type":"agent","envCode":"adev"}','schema')`, id, domain.NewID())
	s := &Service{Pool: p, VerifyResource: func(context.Context, ApprovalInput) (ResourceVerification, error) {
		return func(context.Context, pgx.Tx) error { return nil }, nil
	}}
	in := ApprovalInput{RequestID: id, ActionID: domain.NewID(), Interface: "ess.scale-in", EnvCode: "bdev", Target: map[string]any{"groupId": "synthetic"}, Parameters: map[string]any{}}
	if _, e := s.Request(ctx, in); e == nil {
		t.Fatal("foreign environment approval accepted through shared REST service")
	}
	// Even a pre-existing invalid binding must never grant permission.
	b, _ := canonical(in)
	p.Exec(ctx, `INSERT INTO raptor.approvals(id,request_id,action_id,binding,state) VALUES($1,$2,$3,$4,'approved')`, domain.NewID(), id, in.ActionID, b)
	v, e := s.Check(ctx, in)
	if e != nil || v.Allowed {
		t.Fatal("foreign persisted binding granted permission")
	}
	in.EnvCode = "adev"
	in.ActionID = domain.NewID()
	p.Exec(ctx, `UPDATE raptor.requests SET definition='{"type":"direct","envCode":"adev"}' WHERE id=$1`, id)
	if _, e := s.Request(ctx, in); e == nil {
		t.Fatal("direct request accepted agent approval")
	}
}
