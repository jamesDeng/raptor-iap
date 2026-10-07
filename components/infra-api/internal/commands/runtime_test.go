package commands

import (
	"context"
	"errors"
	"raptor-iap/infra-api/internal/approval"
	"raptor-iap/infra-api/internal/domain"
	"raptor-iap/infra-api/internal/policy"
	"testing"
	"time"
)

type backendFixture struct {
	s            policy.ProxySnapshot
	calls, reads int
	change       bool
}

func (b *backendFixture) Snapshot(context.Context, domain.Environment, string) ([]policy.ProxySnapshot, error) {
	b.reads++
	s := b.s
	if b.change && b.reads > 1 {
		s.Desired++
	}
	return []policy.ProxySnapshot{s}, nil
}
func (b *backendFixture) Scale(context.Context, domain.Environment, string, int) (string, error) {
	b.calls++
	return "provider-request", nil
}
func (b *backendFixture) Protect(context.Context, domain.Environment, string, []string, bool) error {
	b.calls++
	return nil
}
func (b *backendFixture) Deregister(context.Context, domain.Environment, string, []string) error {
	b.calls++
	return nil
}

type metricsFixture struct{ clients float64 }

func (m metricsFixture) ConnectedClients(context.Context, policy.ProxySnapshot) ([]policy.ClientObservation, error) {
	return []policy.ClientObservation{{InstanceID: "i", Clients: m.clients, ObservedAt: time.Unix(1000, 0)}}, nil
}

type approvalFixture struct {
	allowed bool
	calls   int
}

func (a *approvalFixture) Check(_ context.Context, b approval.Binding) (bool, error) {
	a.calls++
	if b.GroupID != "g" || b.ProxyCode != "p" || b.DesiredCapacity != 1 {
		return false, errors.New("incorrect binding")
	}
	return a.allowed, nil
}

type claimFixture struct {
	calls   int
	allowed bool
}

func (c *claimFixture) Claim(_ context.Context, b approval.Binding) (bool, error) {
	c.calls++
	return c.allowed, nil
}
func (c *claimFixture) Record(context.Context, approval.Binding, string, string) error { return nil }
func snapshot() policy.ProxySnapshot {
	return policy.ProxySnapshot{EnvCode: "dev", ProxyCode: "p", GroupID: "g", Desired: 2, Min: 0, Max: 5, Nodes: []policy.Node{{ID: "i"}}}
}

// Removing any gate below submits an unapproved or unsafe capacity reduction.
func TestScaleInGatesAndLostReplayClaimFailClosed(t *testing.T) {
	for _, mode := range []string{"ok", "approval", "clients", "claim", "changed", "no-claim"} {
		t.Run(mode, func(t *testing.T) {
			b := &backendFixture{s: snapshot(), change: mode == "changed"}
			a := &approvalFixture{allowed: mode != "approval"}
			claim := &claimFixture{allowed: mode != "claim"}
			m := metricsFixture{}
			if mode == "clients" {
				m.clients = 1
			}
			r := Runtime{Backend: b, Metrics: m, Approval: a, Claims: claim, Now: func() time.Time { return time.Unix(1000, 0) }, EvidenceMode: "simulated"}
			if mode == "no-claim" {
				r.Claims = nil
			}
			out, e := r.Scale(context.Background(), domain.Environment{Code: "dev"}, domain.ScaleCommand{ProxyTarget: domain.ProxyTarget{RequestID: "r", EnvCode: "dev", ProxyCode: "p", GroupID: "g"}, ActionID: "action", DesiredCapacity: 1})
			if mode == "ok" {
				if e != nil || b.calls != 1 || claim.calls != 1 || out.Outcome != "accepted" {
					t.Fatal(out, e, b.calls)
				}
			} else if e == nil || b.calls != 0 {
				t.Fatalf("gate bypass: %v mutations=%d", e, b.calls)
			}
		})
	}
}
func TestScaleOutAndProtectionRequireNoApproval(t *testing.T) {
	b := &backendFixture{s: snapshot()}
	a := &approvalFixture{}
	r := Runtime{Backend: b, Approval: a, EvidenceMode: "simulated"}
	_, e := r.Scale(context.Background(), domain.Environment{Code: "dev"}, domain.ScaleCommand{ProxyTarget: domain.ProxyTarget{RequestID: "r", EnvCode: "dev", ProxyCode: "p", GroupID: "g"}, DesiredCapacity: 3})
	if e != nil || b.calls != 1 || a.calls != 0 {
		t.Fatal(e, b.calls, a.calls)
	}
	_, e = r.SetProtection(context.Background(), domain.Environment{Code: "dev"}, domain.ProtectionCommand{ProxyTarget: domain.ProxyTarget{RequestID: "r", EnvCode: "dev", ProxyCode: "p", GroupID: "g"}, InstanceIDs: []string{"foreign"}})
	if e == nil || b.calls != 1 {
		t.Fatal("foreign membership bypass")
	}
}
