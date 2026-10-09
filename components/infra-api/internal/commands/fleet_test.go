package commands

import (
	"context"
	"errors"
	"raptor-iap/infra-api/internal/approval"
	"raptor-iap/infra-api/internal/domain"
	"raptor-iap/infra-api/internal/policy"
	"sync"
	"sync/atomic"
	"testing"
)

type fleetFixture struct {
	mu      sync.Mutex
	pending *approval.FleetResult
}

func (f *fleetFixture) FleetClaim(_ context.Context, in approval.FleetIntent, token string) (approval.FleetResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.pending != nil {
		out := *f.pending
		out.Claimed = false
		return out, nil
	}
	out := approval.FleetResult{Claimed: true, Token: token, Intent: in}
	f.pending = &out
	return out, nil
}
func (f *fleetFixture) FleetResolve(_ context.Context, _ approval.FleetIntent, token string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.pending == nil || f.pending.Token != token {
		return errors.New("foreign token")
	}
	f.pending = nil
	return nil
}

type fleetBackend struct {
	mu      sync.Mutex
	s       policy.ProxySnapshot
	calls   atomic.Int32
	unknown bool
}

func (b *fleetBackend) Snapshot(context.Context, domain.Environment, string) ([]policy.ProxySnapshot, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	s := b.s
	s.Nodes = append([]policy.Node(nil), s.Nodes...)
	return []policy.ProxySnapshot{s}, nil
}
func (b *fleetBackend) Scale(context.Context, domain.Environment, string, int) (string, error) {
	b.calls.Add(1)
	return "ack", nil
}
func (b *fleetBackend) Protect(context.Context, domain.Environment, string, []string, bool) error {
	b.calls.Add(1)
	return nil
}
func (b *fleetBackend) Deregister(context.Context, domain.Environment, string, []string) error {
	b.calls.Add(1)
	if b.unknown {
		return errors.New("lost ack")
	}
	return nil
}
func TestConcurrentDistinctDeregistrationsHoldDurablePendingGuard(t *testing.T) {
	for _, unknown := range []bool{false, true} {
		t.Run(map[bool]string{false: "acknowledged pending", true: "lost acknowledgement"}[unknown], func(t *testing.T) {
			b := &fleetBackend{s: policy.ProxySnapshot{EnvCode: "dev", ProxyCode: "p", GroupID: "g", ServerGroupID: "sgp", Desired: 4, Min: 1, Max: 4}, unknown: unknown}
			for _, id := range []string{"a", "b", "c", "d"} {
				b.s.Nodes = append(b.s.Nodes, policy.Node{ID: id, Healthy: true, Registered: true})
			}
			fleet := &fleetFixture{}
			r := Runtime{Backend: b, Fleet: fleet, EvidenceMode: "live"}
			env := domain.Environment{Code: "dev"}
			var wg sync.WaitGroup
			for _, id := range []string{"a", "b"} {
				wg.Add(1)
				go func(id string) {
					defer wg.Done()
					_, _ = r.Deregister(context.Background(), env, domain.DeregisterCommand{ProxyTarget: domain.ProxyTarget{EnvCode: "dev", ProxyCode: "p", GroupID: "g"}, ServerGroupID: "sgp", InstanceIDs: []string{id}})
				}(id)
			}
			wg.Wait()
			if b.calls.Load() != 1 {
				t.Fatalf("concurrent submissions=%d", b.calls.Load())
			}
			// A fresh runtime cannot submit while the async effect is unobserved.
			r = Runtime{Backend: b, Fleet: fleet, EvidenceMode: "live"}
			_, e := r.Deregister(context.Background(), env, domain.DeregisterCommand{ProxyTarget: domain.ProxyTarget{EnvCode: "dev", ProxyCode: "p", GroupID: "g"}, ServerGroupID: "sgp", InstanceIDs: []string{"c"}})
			if e == nil || b.calls.Load() != 1 {
				t.Fatal("pending guard bypassed")
			}
			// Once provider observation proves removal, the next gate uses remaining 3.
			fleet.mu.Lock()
			removed := fleet.pending.Intent.InstanceIDs[0]
			fleet.mu.Unlock()
			b.mu.Lock()
			for i := range b.s.Nodes {
				if b.s.Nodes[i].ID == removed {
					b.s.Nodes[i].Registered = false
				}
			}
			b.mu.Unlock()
			_, e = r.Deregister(context.Background(), env, domain.DeregisterCommand{ProxyTarget: domain.ProxyTarget{EnvCode: "dev", ProxyCode: "p", GroupID: "g"}, ServerGroupID: "sgp", InstanceIDs: []string{"c"}})
			if e == nil || b.calls.Load() != 1 {
				t.Fatal("reconciled majority gate bypassed")
			}
		})
	}
}

func (f *fleetFixture) FleetRecord(_ context.Context, _ approval.FleetIntent, token string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.pending == nil || f.pending.Token != token {
		return errors.New("foreign token")
	}
	f.pending.Recorded = true
	return nil
}

func TestUnrecordedFleetGuardNeverReconcilesBeforeSubmission(t *testing.T) {
	in := approval.FleetIntent{EnvCode: "dev", ProxyCode: "p", GroupID: "g", Kind: "protect", InstanceIDs: []string{"a"}, Protected: true}
	f := &fleetFixture{pending: &approval.FleetResult{Token: "first", Intent: in}}
	b := &fleetBackend{s: policy.ProxySnapshot{EnvCode: "dev", ProxyCode: "p", GroupID: "g", Desired: 1, Min: 1, Max: 4, Nodes: []policy.Node{{ID: "a", Protected: true}}}}
	r := Runtime{Backend: b, Fleet: f, EvidenceMode: "live"}
	called := false
	e := r.guarded(context.Background(), domain.Environment{Code: "dev"}, in, func(Runtime) error { called = true; return nil })
	if e == nil || called || f.pending.Token != "first" {
		t.Fatal("active unsubmitted claim was reconciled")
	}
}
