package runtime

import (
	"context"
	"github.com/jamesDeng/raptor-iap/components/agent-gateway/internal/execution"
	"testing"
)

func TestProviderRecordIgnoresChangedDefault(t *testing.T) {
	r := Providers{Default: "ax", Backends: map[string]execution.LiveRuntime{"aliyun": &NativeLive{}, "ax": &NativeLive{}}}
	p, e := r.provider(execution.RecoveryRecord{Intents: []execution.RuntimeIntentRecord{{Kind: "provider", Name: "aliyun", ResourceID: "aliyun"}}})
	if e != nil || p != "aliyun" {
		t.Fatal("changed default redirected existing attempt")
	}
}
func TestLegacyProviderRemainsAliyun(t *testing.T) {
	r := Providers{Default: "ax", Backends: map[string]execution.LiveRuntime{"aliyun": &NativeLive{}, "ax": &NativeLive{}}}
	p, e := r.provider(execution.RecoveryRecord{})
	if e != nil || p != "aliyun" {
		t.Fatal("legacy attempt redirected")
	}
}
func TestProviderConflictFailsClosed(t *testing.T) {
	for _, intents := range [][]execution.RuntimeIntentRecord{{{Kind: "provider", Name: "ax", ResourceID: "aliyun"}}, {{Kind: "provider", Name: "other", ResourceID: "other"}}, {{Kind: "provider", Name: "ax", ResourceID: "ax"}, {Kind: "provider", Name: "aliyun", ResourceID: "aliyun"}}} {
		r := Providers{Backends: map[string]execution.LiveRuntime{"aliyun": &NativeLive{}, "ax": &NativeLive{}}}
		if _, e := r.provider(execution.RecoveryRecord{Intents: intents}); e == nil {
			t.Fatal("ambiguous provider accepted")
		}
	}
}
func TestMissingProviderRejectsBeforeJournal(t *testing.T) {
	r := Providers{Default: "ax", Backends: map[string]execution.LiveRuntime{"aliyun": &NativeLive{}}}
	if _, e := r.Start(context.Background(), execution.LiveStart{}); e == nil {
		t.Fatal("missing backend accepted")
	}
}

type routeSpy struct {
	NativeLive
	starts, polls, reconciles int
	failure                   error
}

func (s *routeSpy) Start(_ context.Context, in execution.LiveStart) (execution.RuntimeHandle, error) {
	s.starts++
	return execution.RuntimeHandle{RequestID: in.Binding.RequestID, ID: "owned"}, s.failure
}
func (s *routeSpy) Poll(context.Context, execution.RuntimeHandle, int64) (execution.LiveObservation, error) {
	s.polls++
	return execution.LiveObservation{}, s.failure
}
func (s *routeSpy) Reconcile(context.Context, execution.RecoveryRecord) (execution.RecoveredRun, error) {
	s.reconciles++
	return execution.RecoveredRun{}, s.failure
}
func TestProviderJournalSurvivesDefaultChangeAndRestart(t *testing.T) {
	n, record := nativeJournal(t)
	a, x := &routeSpy{}, &routeSpy{}
	r := Providers{Default: "ax", Backends: map[string]execution.LiveRuntime{"aliyun": a, "ax": x}, Store: n.Store, Owner: n.Owner, Lease: n.Lease}
	in := execution.LiveStart{Binding: record.Binding}
	h, e := r.Start(context.Background(), in)
	if e != nil {
		t.Fatal(e)
	}
	r.Default = "aliyun"
	if _, e = r.Poll(context.Background(), h, 0); e != nil {
		t.Fatal(e)
	}
	saved, e := n.Store.RecoveryRecord(context.Background(), record.Binding.AttemptID)
	if e != nil {
		t.Fatal(e)
	}
	fresh := Providers{Default: "aliyun", Backends: r.Backends}
	if _, e = fresh.Reconcile(context.Background(), saved); e != nil {
		t.Fatal(e)
	}
	if a.starts != 0 || a.polls != 0 || a.reconciles != 0 || x.starts != 1 || x.polls != 1 || x.reconciles != 1 {
		t.Fatal("attempt moved between providers")
	}
	if _, e = r.Start(context.Background(), in); e == nil || x.starts != 1 {
		t.Fatal("start replayed")
	}
}
func TestProviderBackendFailureNeverFallsBack(t *testing.T) {
	n, record := nativeJournal(t)
	a, x := &routeSpy{}, &routeSpy{failure: ErrRuntimeUnavailable}
	r := Providers{Default: "ax", Backends: map[string]execution.LiveRuntime{"aliyun": a, "ax": x}, Store: n.Store, Owner: n.Owner, Lease: n.Lease}
	if _, e := r.Start(context.Background(), execution.LiveStart{Binding: record.Binding}); e == nil {
		t.Fatal("failure masked")
	}
	if a.starts != 0 || x.starts != 1 {
		t.Fatal("fallback replay")
	}
}
func TestProviderStaleOwnerCannotDispatch(t *testing.T) {
	n, record := nativeJournal(t)
	x := &routeSpy{}
	r := Providers{Default: "ax", Backends: map[string]execution.LiveRuntime{"ax": x}, Store: n.Store, Owner: "stale", Lease: n.Lease}
	h, e := r.Start(context.Background(), execution.LiveStart{Binding: record.Binding})
	if e == nil {
		t.Fatal("stale owner dispatched")
	}
	if h.RequestID != record.Binding.RequestID {
		t.Fatal("failure lost cleanup request identity")
	}
	if x.starts != 0 {
		t.Fatal("side effect before ownership fence")
	}
}
