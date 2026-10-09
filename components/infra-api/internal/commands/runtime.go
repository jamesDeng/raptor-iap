// Package commands enforces individual operation policy before invoking narrow
// provider capabilities. A Backend must resolve complete authoritative snapshots;
// no cloud backend or replay-claim service is enabled by this package itself.
package commands

import (
	"context"
	"raptor-iap/infra-api/internal/approval"
	"raptor-iap/infra-api/internal/domain"
	"raptor-iap/infra-api/internal/policy"
	"reflect"
	"sort"
	"time"
)

type Backend interface {
	Snapshot(context.Context, domain.Environment, string) ([]policy.ProxySnapshot, error)
	Scale(context.Context, domain.Environment, string, int) (string, error)
	Protect(context.Context, domain.Environment, string, []string, bool) error
	Deregister(context.Context, domain.Environment, string, []string) error
}
type Metrics interface {
	ConnectedClients(context.Context, policy.ProxySnapshot) ([]policy.ClientObservation, error)
}
type Approval interface {
	Check(context.Context, approval.Binding) (bool, error)
}

// Claims is an atomic, durable Raptor-owned action submission boundary.
// A duplicate/unknown action must never return true. Record failure leaves the
// claimed action unknown and blocked from replay; no automatic claim release.
type Claims interface {
	Claim(context.Context, approval.Binding) (bool, error)
	Record(context.Context, approval.Binding, string, string) error
}
type Restarter interface {
	Restart(context.Context, domain.Environment, domain.RestartCommand) (domain.RestartReceipt, error)
}
type Runtime struct {
	Backend      Backend
	Metrics      Metrics
	Approval     Approval
	Claims       Claims
	Fleet        Fleet
	Restarter    Restarter
	Now          func() time.Time
	EvidenceMode string
}

func failure(code string) error { return &domain.CommandError{Code: code} }
func (r Runtime) evidence() string {
	if r.EvidenceMode == "" {
		return "unverified"
	}
	return r.EvidenceMode
}
func (r Runtime) Restart(ctx context.Context, env domain.Environment, in domain.RestartCommand) (domain.RestartReceipt, error) {
	if r.Restarter == nil {
		return domain.RestartReceipt{}, domain.ErrNotConfigured
	}
	return r.Restarter.Restart(ctx, env, in)
}
func (r Runtime) snapshot(ctx context.Context, env domain.Environment, in domain.ProxyTarget) (policy.ProxySnapshot, error) {
	if in.EnvCode != env.Code {
		return policy.ProxySnapshot{}, domain.ErrScope
	}
	if r.Backend == nil {
		return policy.ProxySnapshot{}, domain.ErrNotConfigured
	}
	groups, e := r.Backend.Snapshot(ctx, env, in.ProxyCode)
	if e != nil {
		return policy.ProxySnapshot{}, e
	}
	s, e := policy.ResolveProxy(groups, env.Code, in.ProxyCode, in.GroupID)
	if e != nil {
		return s, e
	}
	if s.Desired < 0 || s.Min < 0 || s.Max < s.Min || s.Desired < s.Min || s.Desired > s.Max {
		return s, failure("TargetChanged")
	}
	return s, nil
}
func canonical(s policy.ProxySnapshot) policy.ProxySnapshot {
	s.Nodes = append([]policy.Node(nil), s.Nodes...)
	sort.Slice(s.Nodes, func(i, j int) bool { return s.Nodes[i].ID < s.Nodes[j].ID })
	return s
}
func (r Runtime) unchanged(ctx context.Context, env domain.Environment, in domain.ProxyTarget, s policy.ProxySnapshot) error {
	current, e := r.snapshot(ctx, env, in)
	if e != nil {
		return e
	}
	if !reflect.DeepEqual(canonical(s), canonical(current)) {
		return failure("TargetChanged")
	}
	return nil
}
func (r Runtime) scale(ctx context.Context, env domain.Environment, in domain.ScaleCommand) (domain.ScaleReceipt, error) {
	s, e := r.snapshot(ctx, env, in.ProxyTarget)
	if e != nil {
		return domain.ScaleReceipt{}, e
	}
	direction, e := policy.ScaleDirection(s, in.DesiredCapacity)
	if e != nil {
		return domain.ScaleReceipt{}, e
	}
	out := domain.ScaleReceipt{Outcome: "no_change", GroupID: s.GroupID, PreviousDesiredCapacity: s.Desired, DesiredCapacity: in.DesiredCapacity, EvidenceMode: r.evidence()}
	if direction == "no_change" {
		return out, nil
	}
	binding := approval.Binding{RequestID: in.RequestID, ActionID: in.ActionID, EnvCode: env.Code, ProxyCode: in.ProxyCode, GroupID: s.GroupID, DesiredCapacity: in.DesiredCapacity}
	var observations []policy.ClientObservation
	if direction == "in" {
		if r.Approval == nil || r.Metrics == nil || r.Claims == nil {
			return domain.ScaleReceipt{}, domain.ErrNotConfigured
		}
		allowed, e := r.Approval.Check(ctx, binding)
		if e != nil || !allowed {
			return domain.ScaleReceipt{}, failure("ApprovalRequired")
		}
		rows, e := r.Metrics.ConnectedClients(ctx, s)
		observations = rows
		if e != nil {
			return domain.ScaleReceipt{}, e
		}
		now := time.Now()
		if r.Now != nil {
			now = r.Now()
		}
		if e = policy.ScaleInClientsAllowed(s, rows, now); e != nil {
			return domain.ScaleReceipt{}, e
		}
	}
	if e = r.unchanged(ctx, env, in.ProxyTarget, s); e != nil {
		return domain.ScaleReceipt{}, e
	}
	if direction == "in" {
		allowed, e := r.Claims.Claim(ctx, binding)
		if e != nil || !allowed {
			return domain.ScaleReceipt{}, failure("ApprovalRequired")
		}
	}
	if direction == "in" {
		now := time.Now()
		if r.Now != nil {
			now = r.Now()
		}
		if checkErr := policy.ScaleInClientsAllowed(s, observations, now); checkErr != nil {
			receiptCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
			recordErr := r.Claims.Record(receiptCtx, binding, "not_submitted", "")
			cancel()
			if recordErr != nil {
				return domain.ScaleReceipt{}, failure("SubmissionUnknown")
			}
			return domain.ScaleReceipt{}, checkErr
		}
	}
	requestID, e := r.Backend.Scale(ctx, env, s.GroupID, in.DesiredCapacity)
	if direction == "in" {
		outcome := "submitted"
		if e != nil {
			outcome = "unknown"
		}
		// Submission receipt recording must survive cancellation of the tool call.
		receiptCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		recordErr := r.Claims.Record(receiptCtx, binding, outcome, requestID)
		cancel()
		if recordErr != nil {
			return domain.ScaleReceipt{}, failure("SubmissionUnknown")
		}
	}
	if e != nil {
		return domain.ScaleReceipt{}, failure("SubmissionUnknown")
	}
	out.Outcome = "accepted"
	out.ProviderRequestID = requestID
	return out, nil
}
func (r Runtime) setProtection(ctx context.Context, env domain.Environment, in domain.ProtectionCommand) (domain.NodeReceipt, error) {
	s, e := r.snapshot(ctx, env, in.ProxyTarget)
	if e != nil {
		return domain.NodeReceipt{}, e
	}
	if e = policy.Membership(s, in.InstanceIDs); e != nil {
		return domain.NodeReceipt{}, e
	}
	if e = r.unchanged(ctx, env, in.ProxyTarget, s); e != nil {
		return domain.NodeReceipt{}, e
	}
	if e = r.Backend.Protect(ctx, env, s.GroupID, in.InstanceIDs, in.Protected); e != nil {
		return domain.NodeReceipt{}, failure("SubmissionUnknown")
	}
	return domain.NodeReceipt{Outcome: "accepted", InstanceIDs: in.InstanceIDs, EvidenceMode: r.evidence()}, nil
}
func (r Runtime) deregister(ctx context.Context, env domain.Environment, in domain.DeregisterCommand) (domain.NodeReceipt, error) {
	s, e := r.snapshot(ctx, env, in.ProxyTarget)
	if e != nil {
		return domain.NodeReceipt{}, e
	}
	if e = policy.DeregistrationAllowed(s, in.ServerGroupID, in.InstanceIDs); e != nil {
		return domain.NodeReceipt{}, e
	}
	if e = r.unchanged(ctx, env, in.ProxyTarget, s); e != nil {
		return domain.NodeReceipt{}, e
	}
	if e = r.Backend.Deregister(ctx, env, s.ServerGroupID, in.InstanceIDs); e != nil {
		return domain.NodeReceipt{}, failure("SubmissionUnknown")
	}
	return domain.NodeReceipt{Outcome: "accepted", InstanceIDs: in.InstanceIDs, EvidenceMode: r.evidence()}, nil
}
