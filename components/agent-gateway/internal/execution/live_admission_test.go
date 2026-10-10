package execution

import (
	"context"
	"testing"
	"time"
)

func TestReplacementAdmissionDefaultsDisabledAndBindsDedicatedScope(t *testing.T) {
	in := replacementInput(t)
	b, _, e := ParseLiveOperation(in, NewID())
	if e != nil {
		t.Fatal(e)
	}
	w := LiveWorker{}
	if _, _, _, e = w.admit(in, b.AttemptID); e == nil {
		t.Fatal("default admitted mutation")
	}
	scope := ReplacementScope{EnvCode: b.EnvCode, ProxyCode: b.ObjectCode, GroupID: "group", ServerGroupID: "backend", TargetDBCode: "db", Application: ApplicationScope{EnvCode: b.EnvCode, AppCode: "client", ClusterID: b.ClusterID, Namespace: "test", Name: "client", UID: "uid"}}
	w.ReplacementScope = &scope
	if _, _, _, e = w.admit(in, b.AttemptID); e != nil {
		t.Fatal(e)
	}
	scope.ProxyCode = "other"
	if _, _, _, e = w.admit(in, b.AttemptID); e == nil {
		t.Fatal("foreign proxy admitted")
	}
}

type replacementAccessFixture struct {
	scope   ReplacementScope
	foreign bool
	revoked bool
}

func (a *replacementAccessFixture) Issue(_ context.Context, b AttemptBinding, _ string, deadline time.Time) (AgentAccess, error) {
	if a.foreign {
		b.RequestID = "foreign"
	}
	return AgentAccess{Binding: b, ReplacementScope: &a.scope, Credential: "synthetic-attempt", ExpiresAt: deadline.Add(-time.Second)}, nil
}
func (a *replacementAccessFixture) Revoke(context.Context, AttemptBinding) error {
	a.revoked = true
	return nil
}
func TestReplacementWorkerNeverStartsWithForeignAttemptAccess(t *testing.T) {
	w, x, driver, _ := workerFixture(t)
	in := replacementInput(t)
	in.RequestID = x.RequestID
	b, _, e := ParseLiveOperation(in, NewID())
	if e != nil {
		t.Fatal(e)
	}
	scope := ReplacementScope{EnvCode: b.EnvCode, ProxyCode: b.ObjectCode, GroupID: "group", ServerGroupID: "backend", TargetDBCode: "db", Application: ApplicationScope{EnvCode: b.EnvCode, AppCode: "client", ClusterID: b.ClusterID, Namespace: "test", Name: "client", UID: "uid"}}
	access := &replacementAccessFixture{scope: scope, foreign: true}
	w.Access = access
	w.ReplacementScope = &scope
	w.Raptor = questionRaptor{in}
	if e = w.RunNext(context.Background()); e != nil {
		t.Fatal(e)
	}
	if driver.startCount != 0 {
		t.Fatal("foreign access reached runtime")
	}
	if !access.revoked {
		t.Fatal("foreign issued access not revoked")
	}
}

func TestRecoveredReplacementRejectsForeignDedicatedScope(t *testing.T) {
	w, x, driver, _ := workerFixture(t)
	ctx := context.Background()
	in := replacementInput(t)
	in.RequestID = x.RequestID
	b, _, err := ParseLiveOperation(in, NewID())
	if err != nil {
		t.Fatal(err)
	}
	scope := ReplacementScope{EnvCode: b.EnvCode, ProxyCode: b.ObjectCode, GroupID: "group", ServerGroupID: "backend", TargetDBCode: "db", Application: ApplicationScope{EnvCode: b.EnvCode, AppCode: "client", ClusterID: b.ClusterID, Namespace: "test", Name: "client", UID: "uid"}}
	w.ReplacementScope = &scope
	w.Access = &replacementAccessFixture{scope: scope}
	w.Raptor = questionRaptor{in}
	if err = w.RunNext(ctx); err != nil {
		t.Fatal(err)
	}
	active, err := w.Store.Get(ctx, x.RequestID)
	if err != nil {
		t.Fatal(err)
	}
	record, err := w.Store.RecoveryRecord(ctx, active.AttemptID)
	if err != nil {
		t.Fatal(err)
	}
	b = record.Binding
	foreign := scope
	foreign.ServerGroupID = "foreign"
	driver.result = &LiveResult{RequestID: b.RequestID, AttemptID: b.AttemptID, SelectedModel: b.Model, ActualModel: b.Model, Answer: ReplacementConvergenceAnswer, GeneratedAt: time.Now(), Replacement: &ReplacementConvergence{Version: 1, Converged: true, Acceptance: "pending", Scope: foreign, OldInstanceIDs: []string{"old-a", "old-b"}, NewInstanceIDs: []string{"new-a", "new-b"}, DesiredCapacity: 2, ObservedAt: time.Now(), Actions: []ReplacementScaleAction{{ActionID: "expand", Previous: 2, Desired: 4, Outcome: "submitted"}, {ActionID: "shrink-a", Previous: 4, Desired: 3, Outcome: "submitted"}, {ActionID: "shrink-b", Previous: 3, Desired: 2, Outcome: "submitted"}}}}
	fresh := *w
	fresh.Owner = "new-owner"
	if err = fresh.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	got, err := w.Store.Get(ctx, x.RequestID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status == "completed" || got.Result != nil {
		t.Fatal("foreign recovered result completed request", got)
	}
	if driver.startCount != 1 {
		t.Fatal("recovery replayed mutation")
	}
}
