package approvals

import (
	"context"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/db"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/domain"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/testutil"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func submissionFixture(t *testing.T) (*Service, ApprovalInput) {
	t.Helper()
	ctx := context.Background()
	p := testutil.Database(t)
	if e := db.Migrate(ctx, p); e != nil {
		t.Fatal(e)
	}
	id := domain.NewID()
	user := domain.User{ID: domain.NewID(), Role: "user"}
	if _, e := p.Exec(ctx, `INSERT INTO raptor.requests(id,creator_id,idempotency_key,input_hash,definition,schema_hash) VALUES($1,$2,'claim','hash','{"type":"agent","envCode":"rdev.ali"}','schema')`, id, user.ID); e != nil {
		t.Fatal(e)
	}
	s := &Service{Pool: p}
	in := ApprovalInput{RequestID: id, ActionID: domain.NewID(), Interface: "ess.scale-in", EnvCode: "rdev.ali", Target: map[string]any{"proxyCode": "proxy", "groupId": "group"}, Parameters: map[string]any{"desiredCapacity": 3}}
	a, e := s.Request(ctx, in)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.Decide(ctx, user, a.ID, DecisionInput{Decision: "approve"}); e != nil {
		t.Fatal(e)
	}
	return s, in
}
func TestSubmissionClaimConcurrentAndDurable(t *testing.T) {
	s, in := submissionFixture(t)
	ctx := context.Background()
	var winners atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			v, e := s.Claim(ctx, in)
			if e != nil {
				t.Error(e)
			}
			if v.Claimed {
				winners.Add(1)
			}
		}()
	}
	wg.Wait()
	if winners.Load() != 1 {
		t.Fatalf("provider submissions permitted: %d", winners.Load())
	}
	fresh := &Service{Pool: s.Pool}
	v, e := fresh.Claim(ctx, in)
	if e != nil || v.Claimed {
		t.Fatal("claim replay after service recreation", e)
	}
	rec := SubmissionRecordInput{Binding: in, Outcome: "unknown"}
	if e = fresh.RecordSubmission(ctx, rec); e != nil {
		t.Fatal(e)
	}
	v, e = fresh.Claim(ctx, in)
	if e != nil || v.Claimed {
		t.Fatal("unknown released claim", e)
	}
	rec.Outcome = "submitted"
	rec.ProviderRequestID = "ack"
	if e = fresh.RecordSubmission(ctx, rec); e == nil {
		t.Fatal("unknown outcome rewritten")
	}
}
func TestSubmissionExactBindingAndRequestState(t *testing.T) {
	for _, mode := range []string{"capacity", "target", "action", "env", "cancelled", "blocked", "completed", "control"} {
		t.Run(mode, func(t *testing.T) {
			s, in := submissionFixture(t)
			ctx := context.Background()
			switch mode {
			case "capacity":
				in.Parameters = map[string]any{"desiredCapacity": 2}
			case "target":
				in.Target = map[string]any{"groupId": "other"}
			case "action":
				in.ActionID = domain.NewID()
			case "env":
				in.EnvCode = "foreign"
			case "control":
				s.Pool.Exec(ctx, "UPDATE raptor.requests SET control_state='cancelled' WHERE id=$1", in.RequestID)
			default:
				s.Pool.Exec(ctx, "UPDATE raptor.requests SET status=$2 WHERE id=$1", in.RequestID, mode)
			}
			v, e := s.Claim(ctx, in)
			if e != nil || v.Claimed {
				t.Fatal("invalid claim permitted", e)
			}
		})
	}
}
func TestSubmissionRecordRequiresClaimAndIsImmutable(t *testing.T) {
	s, in := submissionFixture(t)
	ctx := context.Background()
	rec := SubmissionRecordInput{Binding: in, Outcome: "submitted", ProviderRequestID: "provider-ack"}
	if e := s.RecordSubmission(ctx, rec); e == nil {
		t.Fatal("unclaimed outcome accepted")
	}
	v, e := s.Claim(ctx, in)
	if e != nil || !v.Claimed {
		t.Fatal(e)
	}
	if e = s.RecordSubmission(ctx, rec); e != nil {
		t.Fatal(e)
	}
	if e = s.RecordSubmission(ctx, rec); e != nil {
		t.Fatal("identical retry", e)
	}
	rec.ProviderRequestID = "different"
	if e = s.RecordSubmission(ctx, rec); e == nil {
		t.Fatal("conflicting outcome accepted")
	}
	rec.ProviderRequestID = "provider-ack"
	rec.Binding.Parameters = map[string]any{"desiredCapacity": 2}
	if e = s.RecordSubmission(ctx, rec); e == nil {
		t.Fatal("wrong binding accepted")
	}
}

func TestDecisionUsesRequestFirstLockOrder(t *testing.T) {
	s, in := submissionFixture(t)
	ctx := context.Background()
	var approvalID string
	if e := s.Pool.QueryRow(ctx, "SELECT id::text FROM raptor.approvals WHERE request_id=$1 AND action_id=$2", in.RequestID, in.ActionID).Scan(&approvalID); e != nil {
		t.Fatal(e)
	}
	if _, e := s.Pool.Exec(ctx, "UPDATE raptor.approvals SET state='pending' WHERE id=$1", approvalID); e != nil {
		t.Fatal(e)
	}
	tx, e := s.Pool.Begin(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer tx.Rollback(ctx)
	var pid int
	if e = tx.QueryRow(ctx, "SELECT pg_backend_pid() FROM raptor.requests WHERE id=$1 FOR UPDATE", in.RequestID).Scan(&pid); e != nil {
		t.Fatal(e)
	}
	done := make(chan error, 1)
	go func() {
		c, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		_, e := s.Decide(c, domain.User{ID: domain.NewID(), Role: "user"}, approvalID, DecisionInput{Decision: "approve"})
		done <- e
	}()
	defer func() { tx.Rollback(ctx); <-done }()
	deadline := time.Now().Add(3 * time.Second)
	blocked := false
	for time.Now().Before(deadline) {
		if e = s.Pool.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE $1=ANY(pg_blocking_pids(pid)))", pid).Scan(&blocked); e != nil {
			t.Fatal(e)
		}
		if blocked {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !blocked {
		t.Fatal("decision did not wait on held request")
	}
	// Holding request then locking approval is the claim path. It must not cycle
	// against a decision holding approval while waiting on this request.
	if _, e = tx.Exec(ctx, "SELECT id FROM raptor.approvals WHERE id=$1 FOR UPDATE NOWAIT", approvalID); e != nil {
		t.Fatal("decision holds approval before request", e)
	}
}
