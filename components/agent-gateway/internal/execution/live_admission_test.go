package execution

import (
	"context"
	"encoding/json"
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
	scope := resourceTestScope(b)
	w.ReplacementEnabled = true
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
	w.ReplacementEnabled = true
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
	in := resourceReplacementInput(t)
	in.RequestID = x.RequestID
	b, _, err := ParseLiveOperation(in, NewID())
	if err != nil {
		t.Fatal(err)
	}
	scope := resourceTestScope(b)
	w.ReplacementEnabled = true
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

func TestResourceWorkerPersistsIssuedProofWithoutStaticMapping(t *testing.T) {
	w, x, driver, _ := workerFixture(t)
	ctx := context.Background()
	in := resourceReplacementInput(t)
	in.RequestID = x.RequestID
	b, _, _ := ParseLiveOperation(in, NewID())
	scope := resourceTestScope(b)
	w.ReplacementEnabled = true
	w.Access = &replacementAccessFixture{scope: scope}
	w.Raptor = questionRaptor{in}
	if err := w.RunNext(ctx); err != nil {
		t.Fatal(err)
	}
	if driver.startCount != 1 {
		t.Fatal("resource proof did not reach runtime without static provider mapping")
	}
	active, _ := w.Store.Get(ctx, x.RequestID)
	record, err := w.Store.RecoveryRecord(ctx, active.AttemptID)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(record)
	var saved struct{ ReplacementScope *ReplacementScope }
	json.Unmarshal(raw, &saved)
	if saved.ReplacementScope == nil || *saved.ReplacementScope != scope {
		t.Fatal("complete trusted proof absent from persisted recovery record")
	}
	fresh := *w
	fresh.Owner = "new-owner"
	foreign := scope
	foreign.ListenerID = "foreign"
	driver.result = &LiveResult{RequestID: record.Binding.RequestID, AttemptID: record.Binding.AttemptID, Replacement: &ReplacementConvergence{Scope: foreign}}
	if err := fresh.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	got, _ := w.Store.Get(ctx, x.RequestID)
	if got.Status == "completed" || got.Result != nil {
		t.Fatal("self-asserted recovered proof accepted")
	}
}

func TestIssuedProofJournalIsImmutableAcrossQualificationAttempts(t *testing.T) {
	w, x, driver, _ := workerFixture(t)
	ctx := context.Background()
	in := qualificationInput(t)
	in.RequestID = x.RequestID
	b, _, _ := ParseLiveOperation(in, NewID())
	scope := resourceTestScope(b)
	access := &replacementAccessFixture{scope: scope}
	w.QualificationEnabled = true
	w.Access = access
	w.Raptor = questionRaptor{in}
	if err := w.RunNext(ctx); err != nil {
		t.Fatal(err)
	}
	first, _ := w.Store.Get(ctx, x.RequestID)
	record, err := w.Store.RecoveryRecord(ctx, first.AttemptID)
	if err != nil || record.ReplacementScope == nil {
		t.Fatal("proof not persisted", err)
	}
	b = record.Binding
	if err := w.Store.SaveIssuedScope(ctx, b.AttemptID, "stale", scope); err == nil {
		t.Fatal("stale owner wrote proof")
	}
	if err := w.Store.SaveIssuedScope(ctx, b.AttemptID, w.Owner, scope); err != nil {
		t.Fatal("identical retry refused", err)
	}
	if err := w.Store.SaveIssuedScope(ctx, b.AttemptID, w.Owner, resourceTestScope(b, "retargeted")); err == nil {
		t.Fatal("valid conflicting proof overwrote first proof")
	}
	if err := w.Store.BindLive(ctx, b.AttemptID, w.Owner, b, b.DefinitionSHA256); err != nil {
		t.Fatal("scalar rebind lost private member", err)
	}
	reloaded, err := w.Store.RecoveryRecord(ctx, b.AttemptID)
	if err != nil || reloaded.ReplacementScope == nil || *reloaded.ReplacementScope != scope {
		t.Fatal("rebind stripped proof", err)
	}
	retry := func() {
		t.Helper()
		active, _ := w.Store.Get(ctx, x.RequestID)
		if err := w.Store.FinalizeLive(ctx, active.AttemptID, w.Owner, LiveOutcome{Status: "failed", FailureCode: "Interrupted", Checkpoint: verifiedCheckpoint(), SandboxAbsent: true, KeyAbsent: true, AccessRevoked: true}); err != nil {
			t.Fatal(err)
		}
		raw, _ := json.Marshal(map[string]string{"attemptId": active.AttemptID})
		if err := w.Store.DeliverSignal(ctx, Signal{ID: NewID(), RequestID: x.RequestID, Kind: "continue", Payload: raw}); err != nil {
			t.Fatal(err)
		}
	}
	retry()
	if err := w.RunNext(ctx); err != nil {
		t.Fatal(err)
	}
	second, _ := w.Store.Get(ctx, x.RequestID)
	if second.AttemptID == first.AttemptID || driver.startCount != 2 {
		t.Fatal("new attempt not started with saved proof")
	}
	saved, err := w.Store.RecoveryRecord(ctx, second.AttemptID)
	if err != nil || saved.ReplacementScope == nil || *saved.ReplacementScope != scope {
		t.Fatal("new attempt proof changed", err)
	}
	retry()
	access.scope = resourceTestScope(b, "retargeted")
	if err := w.RunNext(ctx); err != nil {
		t.Fatal(err)
	}
	if driver.startCount != 2 {
		t.Fatal("retargeted proof reached runtime on a later attempt")
	}
}

type migrationGateRuntime struct{ *liveDriver }

func (r migrationGateRuntime) Start(context.Context, LiveStart) (RuntimeHandle, error) {
	return RuntimeHandle{RequestID: r.binding.RequestID}, ErrResourceContractMigrationRequired
}
func TestResourceMigrationGatePreservesFailureAndRevokesIssuedAccess(t *testing.T) {
	w, x, driver, _ := workerFixture(t)
	in := resourceReplacementInput(t)
	in.RequestID = x.RequestID
	b, _, _ := ParseLiveOperation(in, NewID())
	access := &replacementAccessFixture{scope: resourceTestScope(b)}
	w.ReplacementEnabled = true
	w.Access = access
	w.Raptor = questionRaptor{in}
	driver.binding = b
	w.Runtime = migrationGateRuntime{driver}
	if err := w.RunNext(context.Background()); err != nil {
		t.Fatal(err)
	}
	got, err := w.Store.Get(context.Background(), x.RequestID)
	if err != nil || got.Status != "failed" || got.FailureCode != "ResourceContractMigrationRequired" || got.RecoveryNeeded || !access.revoked {
		t.Fatal("migration gate was masked or left issued access", got, err)
	}
}

func TestExplicitEnableFlagCannotBeBypassedByScopeFence(t *testing.T) {
	for _, mode := range []string{"qualification", "replacement"} {
		t.Run(mode, func(t *testing.T) {
			in := resourceReplacementInput(t)
			if mode == "qualification" {
				in = qualificationInput(t)
			}
			b, _, err := ParseLiveOperation(in, NewID())
			if err != nil {
				t.Fatal(err)
			}
			fence := ReplacementScope{EnvCode: b.EnvCode, ProxyCode: b.ObjectCode, GroupID: "group", ServerGroupID: "backend", TargetDBCode: "db", Application: ApplicationScope{EnvCode: b.EnvCode, AppCode: "client", ClusterID: b.ClusterID, Namespace: "test", Name: "client", UID: "uid"}}
			if !fence.ValidFor(b) {
				t.Fatal("fixture fence is invalid")
			}
			worker := LiveWorker{ReplacementScope: &fence, QualificationScope: &fence}
			if _, _, _, err = worker.admit(in, b.AttemptID); err == nil {
				t.Fatal("false enable flag bypassed by valid scope fence")
			}
			if mode == "qualification" {
				worker.ReplacementEnabled = true
			} else {
				worker.QualificationEnabled = true
			}
			if _, _, _, err = worker.admit(in, b.AttemptID); err == nil {
				t.Fatal("other mode's enable flag admitted request")
			}
			if mode == "qualification" {
				worker.QualificationEnabled = true
			} else {
				worker.ReplacementEnabled = true
			}
			if _, _, _, err = worker.admit(in, b.AttemptID); err != nil {
				t.Fatal("explicitly enabled matching fence refused", err)
			}
			fence.ProxyCode = "foreign"
			if _, _, _, err = worker.admit(in, b.AttemptID); err == nil {
				t.Fatal("enabled execution bypassed narrowing fence")
			}
		})
	}
}
