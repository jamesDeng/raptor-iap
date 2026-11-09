package approvals

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/domain"
	"testing"

	"github.com/jamesDeng/raptor-iap/components/raptor/internal/db"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/testutil"
)

func TestRequestGrantMigrationSeparatesGrantFromAction(t *testing.T) {
	ctx := context.Background()
	pool := testutil.Database(t)
	if err := db.Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	var grants, actions bool
	err := pool.QueryRow(ctx, `SELECT to_regclass('raptor.request_grants') IS NOT NULL, to_regclass('raptor.request_grant_actions') IS NOT NULL`).Scan(&grants, &actions)
	if err != nil {
		t.Fatal(err)
	}
	if !grants || !actions {
		t.Fatalf("expected separate tables for reusable grants and single actions: grants=%v actions=%v", grants, actions)
	}
}

func grantFixture(t *testing.T) (*Service, domain.User, RequestGrantInput) {
	t.Helper()
	ctx := context.Background()
	pool := testutil.Database(t)
	if err := db.Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	user := domain.User{ID: domain.NewID(), Role: "user"}
	in := RequestGrantInput{RequestID: domain.NewID(), EnvCode: "rdev.ali", ScopeDigest: strings.Repeat("a", 64),
		Capabilities: []GrantCapability{
			{Capability: "db-proxy.scale-in", Target: GrantTarget{Kind: "db-proxy", Code: "DP00001"}, Constraints: []byte(`{"desiredCapacity":2}`)},
			{Capability: "db-proxy.node-protection", Target: GrantTarget{Kind: "db-proxy", Code: "DP00001"}, Constraints: []byte(`{"protected":true}`)},
		}}
	if _, err := pool.Exec(ctx, `INSERT INTO raptor.requests(id,creator_id,idempotency_key,input_hash,definition,schema_hash,status)
		VALUES($1,$2,'grant','hash','{"type":"agent","envCode":"rdev.ali"}','schema','running')`, in.RequestID, user.ID); err != nil {
		t.Fatal(err)
	}
	service := &Service{Pool: pool, VerifyGrantScope: func(_ context.Context, got RequestGrantInput) (ResourceVerification, error) {
		if got.RequestID != in.RequestID || got.EnvCode != in.EnvCode || got.ScopeDigest != in.ScopeDigest {
			return nil, domain.ErrForbidden
		}
		return func(context.Context, pgx.Tx) error { return nil }, nil
	}}
	return service, user, in
}

func approveGrant(t *testing.T, s *Service, user domain.User, in RequestGrantInput) RequestGrant {
	t.Helper()
	ctx := context.Background()
	pending, err := s.RequestGrant(ctx, in)
	if err != nil || pending.State != "pending" {
		t.Fatalf("request grant = %+v, %v", pending, err)
	}
	grant, err := s.DecideRequestGrant(ctx, user, in.RequestID, pending.ID, "approve")
	if err != nil || grant.State != "approved" {
		t.Fatalf("approve grant = %+v, %v", grant, err)
	}
	// The trusted Gateway start handoff changes queued to running.
	if _, err := s.Pool.Exec(ctx, "UPDATE raptor.requests SET status='running' WHERE id=$1", in.RequestID); err != nil {
		t.Fatal(err)
	}
	return grant
}

func grantAction(grant RequestGrant, capability GrantCapability) GrantActionInput {
	return GrantActionInput{RequestID: grant.RequestID, GrantID: grant.ID, ActionID: domain.NewID(),
		EnvCode: grant.EnvCode, ScopeDigest: grant.ScopeDigest, Capability: capability, Arguments: capability.Constraints}
}

func TestRequestGrantReusableAcrossActions(t *testing.T) {
	ctx := context.Background()
	service, user, in := grantFixture(t)
	grant := approveGrant(t, service, user, in)

	in.Capabilities[0], in.Capabilities[1] = in.Capabilities[1], in.Capabilities[0]
	if again, err := service.RequestGrant(ctx, in); err != nil || again.ID != grant.ID || again.State != "approved" {
		t.Fatalf("same bundle should resolve to the original approval: %+v, %v", again, err)
	}
	if _, err := service.DecideRequestGrant(ctx, user, in.RequestID, grant.ID, "approve"); err != nil {
		t.Fatal("identical decision was not idempotent", err)
	}
	for range 2 {
		action := grantAction(grant, in.Capabilities[0])
		if receipt, err := service.ClaimGrantAction(ctx, action); err != nil || !receipt.Claimed {
			t.Fatalf("distinct action not admitted: %+v, %v", receipt, err)
		}
		if err := service.DispatchGrantAction(ctx, action); err != nil {
			t.Fatal(err)
		}
		record := GrantActionRecord{Action: action, Outcome: "submitted", ProviderRequestID: "provider-ack"}
		if err := service.RecordGrantAction(ctx, record); err != nil {
			t.Fatal(err)
		}
		if receipt, err := service.ClaimGrantAction(ctx, action); err != nil || receipt.Claimed || receipt.State != "submitted" || receipt.ProviderRequestID != "provider-ack" {
			t.Fatalf("replay should return existing receipt: %+v, %v", receipt, err)
		}
		details, err := service.GetGrantAction(ctx, grant.RequestID, action.ActionID)
		if err != nil || details.State != "submitted" || details.ScopeDigest != action.ScopeDigest ||
			details.Capability.Capability != action.Capability.Capability || details.RecordedAt == nil ||
			!equalJSON(details.Arguments, action.Arguments) {
			t.Fatalf("trusted receipt lookup = %+v %v", details, err)
		}
		action.Arguments = []byte(`{"newArgument":true}`)
		if _, err := service.ClaimGrantAction(ctx, action); !errors.Is(err, domain.ErrConflict) {
			t.Fatalf("same action with modified arguments: %v", err)
		}
	}
}

func TestRequestGrantRejectsClosedOrForeignScope(t *testing.T) {
	ctx := context.Background()
	service, user, in := grantFixture(t)
	pending, err := service.RequestGrant(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.DecideRequestGrant(ctx, domain.User{ID: domain.NewID(), Role: "user"}, in.RequestID, pending.ID, "approve"); !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("foreign approver: %v", err)
	}
	action := grantAction(pending, in.Capabilities[0])
	if receipt, err := service.ClaimGrantAction(ctx, action); !errors.Is(err, domain.ErrForbidden) || receipt.Claimed {
		t.Fatalf("pending grant admitted: %+v, %v", receipt, err)
	}
	grant := approveGrant(t, service, user, in)
	action.GrantID = grant.ID
	for _, change := range []func(*GrantActionInput){
		func(a *GrantActionInput) { a.EnvCode = "foreign" },
		func(a *GrantActionInput) { a.ScopeDigest = strings.Repeat("b", 64) },
		func(a *GrantActionInput) { a.Capability.Target.Code = "DP00002" },
		func(a *GrantActionInput) { a.Capability.Constraints = []byte(`{"desiredCapacity":3}`) },
	} {
		foreign := action
		change(&foreign)
		if receipt, err := service.ClaimGrantAction(ctx, foreign); !errors.Is(err, domain.ErrForbidden) || receipt.Claimed {
			t.Fatalf("foreign action admitted: %+v, %v", receipt, err)
		}
	}
	if _, err := service.Pool.Exec(ctx, "UPDATE raptor.requests SET status='completed' WHERE id=$1", in.RequestID); err != nil {
		t.Fatal(err)
	}
	if receipt, err := service.ClaimGrantAction(ctx, action); !errors.Is(err, domain.ErrConflict) || receipt.Claimed {
		t.Fatalf("closed request admitted: %+v, %v", receipt, err)
	}
}

func TestActionClaimCloseRaceAndUnknownReplay(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	service, user, in := grantFixture(t)
	grant := approveGrant(t, service, user, in)
	action := grantAction(grant, in.Capabilities[0])
	if receipt, err := service.ClaimGrantAction(ctx, action); err != nil || !receipt.Claimed {
		t.Fatalf("first claim = %+v, %v", receipt, err)
	}
	if err := service.DispatchGrantAction(ctx, action); err != nil {
		t.Fatal(err)
	}
	record := GrantActionRecord{Action: action, Outcome: "unknown"}
	if err := service.RecordGrantAction(ctx, record); err != nil {
		t.Fatal(err)
	}
	if receipt, err := service.ClaimGrantAction(ctx, action); err != nil || receipt.Claimed || receipt.State != "unknown" {
		t.Fatalf("unknown action replay = %+v, %v", receipt, err)
	}
	if _, err := service.ClaimGrantAction(ctx, grantAction(grant, in.Capabilities[1])); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("new mutation admitted with unresolved action: %v", err)
	}
	// Use a separate request for the close race so the unresolved action
	// cannot hide a missing request lock.
	service, user, in = grantFixture(t)
	grant = approveGrant(t, service, user, in)
	tx, err := service.Pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	var locker int
	if err := tx.QueryRow(ctx, "SELECT pg_backend_pid() FROM raptor.requests WHERE id=$1 FOR UPDATE", in.RequestID).Scan(&locker); err != nil {
		t.Fatal(err)
	}
	closed := make(chan error, 1)
	go func() {
		receipt, err := service.ClaimGrantAction(ctx, grantAction(grant, in.Capabilities[0]))
		if receipt.Claimed {
			t.Error("concurrent close allowed a new action")
		}
		closed <- err
	}()
	for {
		var blocked bool
		if err := service.Pool.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE $1=ANY(pg_blocking_pids(pid)))", locker).Scan(&blocked); err != nil {
			t.Fatal(err)
		}
		if blocked {
			break
		}
		select {
		case err := <-closed:
			t.Fatalf("claim returned without waiting for request lock: %v", err)
		case <-time.After(10 * time.Millisecond):
		}
	}
	if _, err := tx.Exec(ctx, "UPDATE raptor.requests SET status='completed' WHERE id=$1", in.RequestID); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if err := <-closed; !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("claim after close returned %v", err)
	}
}

func TestRequestGrantEarlyDecisionQueuesOneWakeup(t *testing.T) {
	ctx := context.Background()
	s, user, in := grantFixture(t)
	grant, err := s.RequestGrant(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if _, err := s.DecideRequestGrant(ctx, user, in.RequestID, grant.ID, "approve"); err != nil {
			t.Fatal(err)
		}
	}
	var status string
	var pending bool
	var count int
	err = s.Pool.QueryRow(ctx, `SELECT status, approval_resume_pending,
		(SELECT count(*) FROM raptor.outbox WHERE entity_id=$1 AND topic='signal' AND payload->>'kind'='approval'
		 AND payload->'payload'->>'approvalId'=$2) FROM raptor.requests WHERE id=$1`, in.RequestID, grant.ID).
		Scan(&status, &pending, &count)
	if err != nil || status != "queued" || !pending || count != 1 {
		t.Fatalf("early approval lost or duplicated its wakeup: %q %v %d %v", status, pending, count, err)
	}
	if _, err := s.DecideRequestGrant(ctx, user, in.RequestID, grant.ID, "deny"); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("conflicting replay changed approval: %v", err)
	}
}

func TestRequestGrantDispatchOrdersAgainstClose(t *testing.T) {
	ctx := context.Background()
	for _, closedFirst := range []bool{true, false} {
		s, user, in := grantFixture(t)
		grant := approveGrant(t, s, user, in)
		action := grantAction(grant, in.Capabilities[0])
		if receipt, err := s.ClaimGrantAction(ctx, action); err != nil || !receipt.Claimed {
			t.Fatalf("claim = %+v, %v", receipt, err)
		}
		record := GrantActionRecord{Action: action, Outcome: "submitted", ProviderRequestID: "provider"}
		if err := s.RecordGrantAction(ctx, record); !errors.Is(err, domain.ErrConflict) {
			t.Fatalf("receipt before dispatch accepted: %v", err)
		}
		closeRequest := func() {
			if _, err := s.Pool.Exec(ctx, "UPDATE raptor.requests SET status='cancelled',control_state='cancelled' WHERE id=$1", in.RequestID); err != nil {
				t.Fatal(err)
			}
		}
		if closedFirst {
			closeRequest()
			if err := s.DispatchGrantAction(ctx, action); !errors.Is(err, domain.ErrConflict) {
				t.Fatalf("dispatch after cancellation: %v", err)
			}
			record.Outcome, record.ProviderRequestID = "not_submitted", ""
		} else {
			if err := s.DispatchGrantAction(ctx, action); err != nil {
				t.Fatal(err)
			}
			if err := s.DispatchGrantAction(ctx, action); !errors.Is(err, domain.ErrConflict) {
				t.Fatalf("duplicate dispatch: %v", err)
			}
			closeRequest()
		}
		if err := s.RecordGrantAction(ctx, record); err != nil {
			t.Fatalf("record after cancellation: %v", err)
		}
	}
}
