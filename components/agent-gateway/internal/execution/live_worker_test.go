package execution

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

type fixtureLease struct{ valid bool }

func (l *fixtureLease) Valid(context.Context) bool { return l.valid }

type liveAccess struct{ revoked bool }

func (a *liveAccess) Issue(context.Context, AttemptBinding, string, time.Time) (AgentAccess, error) {
	return AgentAccess{Credential: "test-attempt-token", ExpiresAt: time.Now().Add(time.Minute)}, nil
}
func (a *liveAccess) Revoke(context.Context, AttemptBinding) error { a.revoked = true; return nil }

type questionRaptor struct{ input ExecutionInput }

func (r questionRaptor) Context(context.Context, string) (ExecutionInput, error) { return r.input, nil }

type liveDriver struct {
	startCount, polls, recoverCount int
	binding                         AttemptBinding
	cleanup                         bool
	result                          *LiveResult
}

func (r *liveDriver) Start(_ context.Context, s LiveStart) (RuntimeHandle, error) {
	r.startCount++
	r.binding = s.Binding
	return RuntimeHandle{ID: "sandbox", RequestID: s.Binding.RequestID}, nil
}
func (r *liveDriver) Poll(context.Context, RuntimeHandle, int64) (LiveObservation, error) {
	r.polls++
	if r.polls == 1 {
		return LiveObservation{Events: []RuntimeEvent{{RuntimeSequence: 1, Kind: "status", Outcome: "running", OccurredAt: time.Now().UTC()}}}, nil
	}
	result := validLiveResult(r.binding)
	return LiveObservation{Terminal: true, Result: &result}, nil
}
func (r *liveDriver) Checkpoint(context.Context, RuntimeHandle) (VerifiedCheckpoint, error) {
	return verifiedCheckpoint(), nil
}
func (r *liveDriver) Cancel(context.Context, RuntimeHandle) error { return nil }
func (r *liveDriver) Stop(context.Context, RuntimeHandle) (LiveCleanup, error) {
	return LiveCleanup{SandboxAbsent: r.cleanup, KeyAbsent: r.cleanup}, nil
}
func (r *liveDriver) Reconcile(_ context.Context, record RecoveryRecord) (RecoveredRun, error) {
	r.recoverCount++
	return RecoveredRun{Result: r.result, Checkpoint: verifiedCheckpoint(), Cleanup: LiveCleanup{SandboxAbsent: r.cleanup, KeyAbsent: r.cleanup}}, nil
}
func workerFixture(t *testing.T) (*LiveWorker, *Execution, *liveDriver, *liveAccess) {
	t.Helper()
	s := testStore(t)
	id := NewID()
	s.Receive(context.Background(), id)
	definition, _ := json.Marshal(map[string]any{"operation": "application.question", "object": map[string]string{"kind": "application", "code": "app"}, "envCode": "rdev.ali", "parameters": map[string]string{"question": "Is gateway healthy?", "model": "gpt-5.6-luna"}})
	driver := &liveDriver{cleanup: true}
	access := &liveAccess{}
	w := &LiveWorker{Store: s, Owner: "owner", Lease: &fixtureLease{valid: true}, Runtime: driver, Access: access, Raptor: questionRaptor{ExecutionInput{RequestID: id, Definition: definition, Skills: SkillsVersion{Tag: "v1", CommitSHA: "0123456789012345678901234567890123456789"}}}, Bootstrap: verifiedCheckpoint()}
	x, _ := s.Get(context.Background(), id)
	return w, &x, driver, access
}
func TestLiveWorkerDoesNotCompleteAtStart(t *testing.T) {
	w, x, r, a := workerFixture(t)
	ctx := context.Background()
	if e := w.RunNext(ctx); e != nil {
		t.Fatal(e)
	}
	got, _ := w.Store.Get(ctx, x.RequestID)
	if got.Status != "running" || got.Result != nil || r.startCount != 1 {
		t.Fatal("start claimed completion", got)
	}
	if e := w.RunActive(ctx); e != nil {
		t.Fatal(e)
	}
	events, _ := w.Store.Events(ctx, x.RequestID, 0)
	got, _ = w.Store.Get(ctx, x.RequestID)
	if len(events) == 0 || got.Status != "running" {
		t.Fatal("missing visible progress")
	}
	if e := w.RunActive(ctx); e != nil {
		t.Fatal(e)
	}
	got, _ = w.Store.Get(ctx, x.RequestID)
	if got.Status != "completed" || got.Result == nil || !a.revoked {
		t.Fatal("result/cleanup not completed", got)
	}
}
func TestLiveWorkerKeepsAnswerWhenCleanupUncertain(t *testing.T) {
	w, x, r, _ := workerFixture(t)
	r.cleanup = false
	ctx := context.Background()
	w.RunNext(ctx)
	w.RunActive(ctx)
	if e := w.RunActive(ctx); e != nil {
		t.Fatal(e)
	}
	got, _ := w.Store.Get(ctx, x.RequestID)
	if got.Status != "blocked" || got.Result == nil || !got.RecoveryNeeded {
		t.Fatal("uncertain cleanup misreported", got)
	}
}
func TestLiveWorkerRestartReconcilesWithoutReplay(t *testing.T) {
	w, x, r, _ := workerFixture(t)
	ctx := context.Background()
	if e := w.RunNext(ctx); e != nil {
		t.Fatal(e)
	}
	fresh := *w
	fresh.Owner = "new-owner"
	if e := fresh.Reconcile(ctx); e != nil {
		t.Fatal(e)
	}
	got, _ := w.Store.Get(ctx, x.RequestID)
	if r.startCount != 1 || r.recoverCount != 1 || got.Status != "failed" || got.FailureCode != "Interrupted" {
		t.Fatal("restart replayed or invented answer", got)
	}
}
func TestLiveWorkerLostLeaseCannotDispatch(t *testing.T) {
	w, _, r, _ := workerFixture(t)
	w.Lease = &fixtureLease{valid: false}
	if e := w.RunNext(context.Background()); e == nil || r.startCount != 0 {
		t.Fatal("lost lease dispatched")
	}
}
func TestLiveWorkerCancellationDoesNotReplay(t *testing.T) {
	w, x, r, _ := workerFixture(t)
	ctx := context.Background()
	w.RunNext(ctx)
	if e := w.Store.DeliverSignal(ctx, Signal{ID: "cancel", RequestID: x.RequestID, Kind: "cancel", Payload: json.RawMessage(`{}`)}); e != nil {
		t.Fatal(e)
	}
	if e := w.RunActive(ctx); e != nil {
		t.Fatal(e)
	}
	if e := w.RunActive(ctx); e != nil {
		t.Fatal(e)
	}
	got, _ := w.Store.Get(ctx, x.RequestID)
	if got.Status != "cancelled" || r.startCount != 1 {
		t.Fatal("cancellation not durable", got)
	}
}

type afterAccess struct {
	liveAccess
	after func()
}

func (a *afterAccess) Revoke(ctx context.Context, b AttemptBinding) error { a.after(); return nil }

type checkpointHook struct {
	*liveDriver
	hook func()
}

func (d checkpointHook) Checkpoint(context.Context, RuntimeHandle) (VerifiedCheckpoint, error) {
	d.hook()
	return verifiedCheckpoint(), nil
}
func TestLeaseLossDuringRevocationCannotFinalize(t *testing.T) {
	w, x, _, _ := workerFixture(t)
	ctx := context.Background()
	lease, e := w.Store.AcquireLiveWorker(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer lease.Close()
	w.Lease = lease
	w.Access = &afterAccess{after: func() { lease.conn.Conn().Close(ctx) }}
	if e = w.RunNext(ctx); e != nil {
		t.Fatal(e)
	}
	w.RunActive(ctx)
	if e = w.RunActive(ctx); e == nil {
		t.Fatal("lost lease published terminal")
	}
	got, _ := w.Store.Get(ctx, x.RequestID)
	if got.Status == "completed" {
		t.Fatal("terminal committed after lease loss")
	}
}
func TestCancellationDuringCheckpointWins(t *testing.T) {
	w, x, driver, _ := workerFixture(t)
	ctx := context.Background()
	w.Runtime = checkpointHook{driver, func() {
		if e := w.Store.DeliverSignal(ctx, Signal{ID: NewID(), RequestID: x.RequestID, Kind: "cancel", Payload: json.RawMessage(`{}`)}); e != nil {
			t.Fatal(e)
		}
	}}
	w.RunNext(ctx)
	w.RunActive(ctx)
	if e := w.RunActive(ctx); e != nil {
		t.Fatal(e)
	}
	got, _ := w.Store.Get(ctx, x.RequestID)
	if got.Status != "cancelled" {
		t.Fatal(got.Status)
	}
}
func TestRestartUnboundClaimReleasesWithoutReplay(t *testing.T) {
	w, x, driver, _ := workerFixture(t)
	ctx := context.Background()
	w.Store.RuntimeMode = "live"
	claimed, e := w.Store.ClaimLiveNext(ctx, "old-owner")
	if e != nil {
		t.Fatal(e)
	}
	if e = w.Reconcile(ctx); e != nil {
		t.Fatal(e)
	}
	got, _ := w.Store.Get(ctx, x.RequestID)
	if got.Status != "failed" || got.FailureCode != "Interrupted" || driver.startCount != 0 {
		t.Fatal(got, claimed)
	}
}

type unavailableAccess struct{ liveAccess }

func (a *unavailableAccess) Issue(context.Context, AttemptBinding, string, time.Time) (AgentAccess, error) {
	return AgentAccess{}, ErrUnavailable
}
func TestAccessServiceOutageIsNotOAuthSignIn(t *testing.T) {
	w, x, _, _ := workerFixture(t)
	w.Access = &unavailableAccess{}
	ctx := context.Background()
	if e := w.RunNext(ctx); e != nil {
		t.Fatal(e)
	}
	got, _ := w.Store.Get(ctx, x.RequestID)
	if got.FailureCode != "ProviderUnavailable" {
		t.Fatal(got.FailureCode)
	}
}
