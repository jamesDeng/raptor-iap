package commands

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"raptor-iap/infra-api/internal/approval"
	"raptor-iap/infra-api/internal/domain"
	"raptor-iap/infra-api/internal/policy"
)

func directSnapshot() policy.ProxySnapshot {
	return policy.ProxySnapshot{EnvCode: "dev", ProxyCode: "p", GroupID: "g", ServerGroupID: "backend", Desired: 4, Min: 2, Max: 4, Nodes: []policy.Node{
		{ID: "old-a"}, {ID: "old-b"},
		{ID: "new-a", Protected: true, Registered: true, Healthy: true}, {ID: "new-b", Protected: true, Registered: true, Healthy: true},
	}}
}

// This fixture models the external durable action boundary, including refusal
// to claim an action again after an uncertain submission or receipt failure.
type directAuthority struct {
	allowed, claimed bool
	checks, claims   int
	binding          approval.Binding
	outcome          string
	recordErr        error
}

func (a *directAuthority) Check(_ context.Context, b approval.Binding) (bool, error) {
	a.checks++
	return a.allowed && b == a.binding, nil
}
func (a *directAuthority) Claim(_ context.Context, b approval.Binding) (bool, error) {
	a.claims++
	if a.claimed || b != a.binding {
		return false, nil
	}
	a.claimed = true
	return true, nil
}
func (a *directAuthority) Record(_ context.Context, b approval.Binding, outcome, _ string) error {
	if b != a.binding {
		return errors.New("foreign receipt")
	}
	a.outcome = outcome
	return a.recordErr
}

type directMetrics []policy.ClientObservation

func (m directMetrics) ConnectedClients(context.Context, policy.ProxySnapshot) ([]policy.ClientObservation, error) {
	return m, nil
}

func directCommand() domain.ScaleCommand {
	return domain.ScaleCommand{ProxyTarget: domain.ProxyTarget{RequestID: "request", EnvCode: "dev", ProxyCode: "p", GroupID: "g"}, ActionID: "scale-direct", DesiredCapacity: 2}
}
func directApproval() *directAuthority {
	return &directAuthority{allowed: true, binding: approval.Binding{RequestID: "request", ActionID: "scale-direct", EnvCode: "dev", ProxyCode: "p", GroupID: "g", DesiredCapacity: 2}}
}

// Removing the direct cohort guard permits provider-selected termination of a
// retained node, even when every currently unprotected member has zero clients.
func TestDirectReplacementScaleInRequiresRetainedPairAndWithdrawnPair(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*policy.ProxySnapshot)
		want   string
	}{
		{name: "complete drained pair"},
		{name: "registered original", change: func(s *policy.ProxySnapshot) { s.Nodes[0].Registered = true }, want: "InsufficientHealthyCapacity"},
		{name: "protected original", change: func(s *policy.ProxySnapshot) { s.Nodes[0].Protected = true }, want: "InsufficientHealthyCapacity"},
		{name: "extra eligible node", change: func(s *policy.ProxySnapshot) { s.Nodes[2].Protected = false }, want: "InsufficientHealthyCapacity"},
		{name: "unhealthy replacement", change: func(s *policy.ProxySnapshot) { s.Nodes[2].Healthy = false }, want: "InsufficientHealthyCapacity"},
		{name: "withdrawn replacement", change: func(s *policy.ProxySnapshot) { s.Nodes[2].Registered = false }, want: "InsufficientHealthyCapacity"},
		{name: "incomplete cohort", change: func(s *policy.ProxySnapshot) { s.Nodes = s.Nodes[1:] }, want: "TargetChanged"},
		{name: "extra cohort member", change: func(s *policy.ProxySnapshot) { s.Nodes = append(s.Nodes, policy.Node{ID: "extra"}) }, want: "TargetChanged"},
		{name: "duplicate member", change: func(s *policy.ProxySnapshot) { s.Nodes[0].ID = "old-b" }, want: "TargetChanged"},
		{name: "blank member", change: func(s *policy.ProxySnapshot) { s.Nodes[0].ID = "" }, want: "TargetChanged"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := directSnapshot()
			if tc.change != nil {
				tc.change(&s)
			}
			b := &backendFixture{s: s}
			a := directApproval()
			rows := directMetrics{}
			for _, n := range s.Nodes {
				rows = append(rows, policy.ClientObservation{InstanceID: n.ID, ObservedAt: time.Unix(1000, 0)})
			}
			r := Runtime{Backend: b, Metrics: rows, Approval: a, Claims: a, Now: func() time.Time { return time.Unix(1000, 0) }}
			out, err := r.Scale(context.Background(), domain.Environment{Code: "dev"}, directCommand())
			got := ""
			if err != nil {
				got = err.Error()
			}
			if got != tc.want {
				t.Fatalf("want %q, got %v, provider calls=%d", tc.want, err, b.calls)
			}
			if tc.want == "" {
				if b.calls != 1 || a.claims != 1 || a.outcome != "submitted" || out.Outcome != "accepted" || out.PreviousDesiredCapacity != 4 || out.DesiredCapacity != 2 {
					t.Fatalf("out=%+v calls=%d authority=%+v", out, b.calls, a)
				}
			} else if b.calls != 0 || a.claims != 0 {
				t.Fatalf("unsafe submission or consumed approval: calls=%d claims=%d", b.calls, a.claims)
			}
		})
	}
}

// The new capacity exception must not bypass metrics, unchanged snapshots,
// exact approval, durable claims, or their freshness check after a claim.
func TestDirectReplacementScaleInPreservesExistingGates(t *testing.T) {
	for _, mode := range []string{"connected-first", "connected-second", "missing", "stale", "duplicate-metric", "approval", "foreign-action", "foreign-request", "foreign-env", "foreign-proxy", "foreign-group", "foreign-desired", "no-approval", "no-claim", "lost-claim", "changed", "expired-after-claim"} {
		t.Run(mode, func(t *testing.T) {
			b := &backendFixture{s: directSnapshot(), change: mode == "changed"}
			a := directApproval()
			rows := directMetrics{{InstanceID: "old-a", ObservedAt: time.Unix(1000, 0)}, {InstanceID: "old-b", ObservedAt: time.Unix(1000, 0)}}
			want := "ApprovalRequired"
			switch mode {
			case "connected-first":
				rows[0].Clients = 1
				want = "ConnectedClientsPresent"
			case "connected-second":
				rows[1].Clients = 1
				want = "ConnectedClientsPresent"
			case "missing":
				rows = rows[:1]
				want = "MetricsInvalid"
			case "stale":
				rows[1].ObservedAt = time.Unix(954, 0)
				want = "MetricsInvalid"
			case "duplicate-metric":
				rows = append(rows, rows[0])
				want = "MetricsInvalid"
			case "approval":
				a.allowed = false
			case "foreign-action":
				a.binding.ActionID = "other"
			case "foreign-request":
				a.binding.RequestID = "other"
			case "foreign-env":
				a.binding.EnvCode = "other"
			case "foreign-proxy":
				a.binding.ProxyCode = "other"
			case "foreign-group":
				a.binding.GroupID = "other"
			case "foreign-desired":
				a.binding.DesiredCapacity = 3
			case "no-approval", "no-claim":
				want = domain.ErrNotConfigured.Error()
			case "lost-claim":
				a.claimed = true
			case "changed":
				want = "TargetChanged"
			case "expired-after-claim":
				want = "MetricsInvalid"
			}
			clocks := 0
			r := Runtime{Backend: b, Metrics: rows, Approval: a, Claims: a, Now: func() time.Time {
				clocks++
				if mode == "expired-after-claim" && clocks > 1 {
					return time.Unix(1046, 0)
				}
				return time.Unix(1000, 0)
			}}
			if mode == "no-approval" {
				r.Approval = nil
			}
			if mode == "no-claim" {
				r.Claims = nil
			}
			_, err := r.Scale(context.Background(), domain.Environment{Code: "dev"}, directCommand())
			if err == nil || err.Error() != want || b.calls != 0 {
				t.Fatalf("want %s, got %v, mutations=%d", want, err, b.calls)
			}
			if mode == "expired-after-claim" && a.outcome != "not_submitted" {
				t.Fatalf("claimed refusal not recorded: %+v", a)
			}
		})
	}
}

func TestDirectReplacementUnknownSubmissionIsNotReplayed(t *testing.T) {
	for _, receiptFailure := range []bool{false, true} {
		t.Run(map[bool]string{false: "lost acknowledgement", true: "lost receipt"}[receiptFailure], func(t *testing.T) {
			b := &backendFixture{s: directSnapshot()}
			a := directApproval()
			if receiptFailure {
				a.recordErr = errors.New("receipt unavailable")
			} else {
				b.scaleErr = errors.New("ack unavailable")
			}
			rows := directMetrics{{InstanceID: "old-a", ObservedAt: time.Unix(1000, 0)}, {InstanceID: "old-b", ObservedAt: time.Unix(1000, 0)}}
			r := Runtime{Backend: b, Metrics: rows, Approval: a, Claims: a, Now: func() time.Time { return time.Unix(1000, 0) }}
			_, err := r.Scale(context.Background(), domain.Environment{Code: "dev"}, directCommand())
			if err == nil || err.Error() != "SubmissionUnknown" || b.calls != 1 {
				t.Fatalf("err=%v mutations=%d", err, b.calls)
			}
			if !receiptFailure && a.outcome != "unknown" {
				t.Fatalf("uncertain submission recorded as %q", a.outcome)
			}
			_, err = r.Scale(context.Background(), domain.Environment{Code: "dev"}, directCommand())
			if err == nil || err.Error() != "ApprovalRequired" || b.calls != 1 {
				t.Fatalf("replayed unknown: err=%v mutations=%d", err, b.calls)
			}
		})
	}
}

type directChangingBackend struct{ backendFixture }

func (b *directChangingBackend) Snapshot(ctx context.Context, env domain.Environment, proxy string) ([]policy.ProxySnapshot, error) {
	groups, err := b.backendFixture.Snapshot(ctx, env, proxy)
	if b.reads > 1 {
		groups[0].Nodes = append([]policy.Node(nil), groups[0].Nodes...)
		groups[0].Nodes[2].Protected = false
	}
	return groups, err
}

func TestDirectReplacementRejectsChangedProtectionBeforeClaim(t *testing.T) {
	b := &directChangingBackend{backendFixture: backendFixture{s: directSnapshot()}}
	a := directApproval()
	r := Runtime{Backend: b, Metrics: directMetrics{{InstanceID: "old-a", ObservedAt: time.Unix(1000, 0)}, {InstanceID: "old-b", ObservedAt: time.Unix(1000, 0)}}, Approval: a, Claims: a, Now: func() time.Time { return time.Unix(1000, 0) }}
	_, err := r.Scale(context.Background(), domain.Environment{Code: "dev"}, directCommand())
	if err == nil || err.Error() != "TargetChanged" || b.calls != 0 || a.claims != 0 {
		t.Fatalf("changed protection submitted: err=%v mutations=%d claims=%d", err, b.calls, a.claims)
	}
}

func TestDirectReplacementScaleInPreservesFleetPendingGuard(t *testing.T) {
	for _, unknown := range []bool{false, true} {
		t.Run(map[bool]string{false: "acknowledged pending", true: "lost acknowledgement"}[unknown], func(t *testing.T) {
			b := &backendFixture{s: directSnapshot()}
			if unknown {
				b.scaleErr = errors.New("ack unavailable")
			}
			a := directApproval()
			fleet := &fleetFixture{}
			r := Runtime{Backend: b, Metrics: directMetrics{{InstanceID: "old-a", ObservedAt: time.Unix(1000, 0)}, {InstanceID: "old-b", ObservedAt: time.Unix(1000, 0)}}, Approval: a, Claims: a, Fleet: fleet, EvidenceMode: "live", Now: func() time.Time { return time.Unix(1000, 0) }}
			_, err := r.Scale(context.Background(), domain.Environment{Code: "dev"}, directCommand())
			if (unknown && (err == nil || err.Error() != "SubmissionUnknown")) || (!unknown && err != nil) || b.calls != 1 {
				t.Fatalf("err=%v mutations=%d", err, b.calls)
			}
			if fleet.pending == nil || !fleet.pending.Recorded {
				t.Fatal("submission left no durable fleet guard")
			}
			// A new action and runtime cannot bypass an unobserved fleet effect.
			in := directCommand()
			in.ActionID = "different-action"
			a.binding.ActionID = in.ActionID
			r = Runtime{Backend: b, Fleet: fleet, Approval: a, Claims: a, EvidenceMode: "live"}
			_, err = r.Scale(context.Background(), domain.Environment{Code: "dev"}, in)
			if err == nil || err.Error() != "FleetBusy" || b.calls != 1 || a.checks != 1 {
				t.Fatalf("pending scale bypassed: err=%v mutations=%d checks=%d", err, b.calls, a.checks)
			}
		})
	}
}

// Fleet ownership still serializes the new batch deregistration exception and
// only permits the second original once the first withdrawal is observed.
func TestDirectReplacementDeregistrationPreservesFleetSerialization(t *testing.T) {
	b := &fleetBackend{s: directSnapshot()}
	b.s.Nodes[0].Registered, b.s.Nodes[1].Registered = true, true
	b.s.Nodes[0].Healthy, b.s.Nodes[1].Healthy = true, true
	f := &fleetFixture{}
	r := Runtime{Backend: b, Fleet: f, EvidenceMode: "live"}
	in := domain.DeregisterCommand{ProxyTarget: directCommand().ProxyTarget, ServerGroupID: "backend", InstanceIDs: []string{"old-a"}}
	if _, err := r.Deregister(context.Background(), domain.Environment{Code: "dev"}, in); err != nil {
		t.Fatal(err)
	}
	in.InstanceIDs = []string{"old-b"}
	if _, err := r.Deregister(context.Background(), domain.Environment{Code: "dev"}, in); err == nil || err.Error() != "FleetBusy" || b.calls.Load() != 1 {
		t.Fatalf("pending withdrawal bypass: %v", err)
	}
	b.s.Nodes[0].Registered = false
	if _, err := r.Deregister(context.Background(), domain.Environment{Code: "dev"}, in); err != nil || b.calls.Load() != 2 {
		t.Fatalf("second withdrawal rejected: %v calls=%d", err, b.calls.Load())
	}
	if f.pending == nil || !reflect.DeepEqual(f.pending.Intent.InstanceIDs, []string{"old-b"}) {
		t.Fatalf("second withdrawal unguarded: %+v", f.pending)
	}
}
