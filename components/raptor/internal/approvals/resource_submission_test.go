package approvals

import (
	"context"
	"github.com/jackc/pgx/v5"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/domain"
	"testing"
)

func TestResourceApprovalClaimRejectsRetargetBeforeConsumption(t *testing.T) {
	s, in := submissionFixture(t)
	in.ActionID = domain.NewID()
	in.Interface = "db-proxy.scale-in"
	in.Target = map[string]any{"proxyCode": "proxy"}
	calls := 0
	s.VerifyResource = func(context.Context, ApprovalInput) (ResourceVerification, error) {
		calls++
		return nil, domain.ErrConflict
	}
	if _, e := s.Request(context.Background(), in); e == nil {
		t.Fatal("retargeted approval recorded")
	}
	s.VerifyResource = func(context.Context, ApprovalInput) (ResourceVerification, error) {
		calls++
		return func(context.Context, pgx.Tx) error { return nil }, nil
	}
	a, e := s.Request(context.Background(), in)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.Decide(context.Background(), domain.User{ID: domain.NewID(), Role: "user"}, a.ID, DecisionInput{Decision: "approve"}); e != nil {
		t.Fatal(e)
	}
	s.VerifyResource = func(context.Context, ApprovalInput) (ResourceVerification, error) {
		calls++
		return nil, domain.ErrConflict
	}
	if v, e := s.Claim(context.Background(), in); e == nil && v.Claimed {
		t.Fatal("changed proof consumed approval")
	}
	var n int
	if e = s.Pool.QueryRow(context.Background(), "SELECT count(*) FROM raptor.approval_submissions WHERE request_id=$1 AND action_id=$2", in.RequestID, in.ActionID).Scan(&n); e != nil || n != 0 {
		t.Fatal("retarget recorded claim", n, e)
	}
	s.VerifyResource = func(context.Context, ApprovalInput) (ResourceVerification, error) {
		calls++
		return func(context.Context, pgx.Tx) error { return nil }, nil
	}
	if v, e := s.Claim(context.Background(), in); e != nil || !v.Claimed {
		t.Fatal(v, e)
	}
	if v, e := s.Claim(context.Background(), in); e != nil || v.Claimed {
		t.Fatal("replay", v, e)
	}
	if calls < 4 {
		t.Fatal("proof not freshly checked", calls)
	}
}

func TestLegacyScaleInApprovalRemainsReadableButCannotAuthorize(t *testing.T) {
	s, in := submissionFixture(t)
	ctx := context.Background()
	in.ActionID = domain.NewID()
	in.Interface = "ess.scale-in"
	in.Target = map[string]any{"proxyCode": "proxy", "groupId": "group"}
	if _, e := s.Request(ctx, in); e == nil {
		t.Error("old active approval request accepted")
	}
	in.ActionID = domain.NewID()
	raw, e := canonical(in)
	if e != nil {
		t.Fatal(e)
	}
	id := domain.NewID()
	if _, e = s.Pool.Exec(ctx, "INSERT INTO raptor.approvals(id,request_id,action_id,binding,state) VALUES($1,$2,$3,$4,'approved')", id, in.RequestID, in.ActionID, raw); e != nil {
		t.Fatal(e)
	}
	if a, e := s.Get(ctx, id); e != nil || a.State != "approved" {
		t.Fatal("historical approval unreadable", a, e)
	}
	if v, e := s.Check(ctx, in); e != nil || v.Allowed {
		t.Error("historical approval authorized", v, e)
	}
	if v, e := s.Claim(ctx, in); e == nil || v.Claimed {
		t.Error("legacy claim accepted", v, e)
	}
	if e = s.RecordSubmission(ctx, SubmissionRecordInput{Binding: in, Outcome: "unknown"}); e == nil {
		t.Error("legacy outcome mutation accepted")
	}
}
