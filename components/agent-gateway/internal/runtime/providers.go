package runtime

import (
	"context"
	"github.com/jamesDeng/raptor-iap/components/agent-gateway/internal/execution"
)

// Providers pins each attempt to its selected backend before any remote side effect.
// Operator default changes affect only new attempts. Errors never trigger fallback.
type Providers struct {
	Default  string
	Backends map[string]execution.LiveRuntime
	Store    *execution.Store
	Owner    string
	Lease    execution.WorkerLease
}

func validProvider(p string) bool { return p == "aliyun" || p == "ax" }
func (r *Providers) provider(record execution.RecoveryRecord) (string, error) {
	p := "aliyun"
	seen := false
	for _, v := range record.Intents {
		if v.Kind != "provider" {
			continue
		}
		if seen || !validProvider(v.Name) || (v.ResourceID != "" && v.ResourceID != v.Name) {
			return "", ErrConfiguration
		}
		seen = true
		p = v.Name
	}
	if r.Backends[p] == nil {
		return "", ErrConfiguration
	}
	return p, nil
}
func (r *Providers) record(ctx context.Context, h execution.RuntimeHandle) (execution.RecoveryRecord, error) {
	if r.Store == nil {
		return execution.RecoveryRecord{}, ErrConfiguration
	}
	x, e := r.Store.Get(ctx, h.RequestID)
	if e != nil {
		return execution.RecoveryRecord{}, e
	}
	return r.Store.RecoveryRecord(ctx, x.AttemptID)
}
func (r *Providers) Start(ctx context.Context, in execution.LiveStart) (execution.RuntimeHandle, error) {
	p := r.Default
	if p == "" {
		p = "aliyun"
	}
	if !validProvider(p) || r.Backends[p] == nil || r.Store == nil || r.Lease == nil || !r.Lease.Valid(ctx) {
		return execution.RuntimeHandle{RequestID: in.Binding.RequestID}, ErrConfiguration
	}
	record, e := r.Store.RecoveryRecord(ctx, in.Binding.AttemptID)
	if e != nil {
		return execution.RuntimeHandle{RequestID: in.Binding.RequestID}, e
	}
	if record.Binding != in.Binding {
		return execution.RuntimeHandle{RequestID: in.Binding.RequestID}, ErrConfiguration
	}
	// Start must never be replayed, even when the provider matches.
	for _, v := range record.Intents {
		if v.Kind == "provider" {
			return execution.RuntimeHandle{RequestID: in.Binding.RequestID}, ErrRuntimeUnavailable
		}
	}
	if e = r.Store.RecordIntent(ctx, in.Binding.AttemptID, r.Owner, execution.RuntimeIntent{Kind: "provider", Name: p}); e != nil {
		return execution.RuntimeHandle{RequestID: in.Binding.RequestID}, e
	}
	if e = r.Store.RecordResource(ctx, in.Binding.AttemptID, r.Owner, execution.RuntimeResource{Kind: "provider", ID: p}); e != nil {
		return execution.RuntimeHandle{RequestID: in.Binding.RequestID}, e
	}
	return r.Backends[p].Start(ctx, in)
}
func (r *Providers) backend(ctx context.Context, h execution.RuntimeHandle) (execution.LiveRuntime, error) {
	v, e := r.record(ctx, h)
	if e != nil {
		return nil, e
	}
	p, e := r.provider(v)
	if e != nil {
		return nil, e
	}
	return r.Backends[p], nil
}
func (r *Providers) Poll(ctx context.Context, h execution.RuntimeHandle, cursor int64) (execution.LiveObservation, error) {
	b, e := r.backend(ctx, h)
	if e != nil {
		return execution.LiveObservation{}, e
	}
	return b.Poll(ctx, h, cursor)
}
func (r *Providers) Checkpoint(ctx context.Context, h execution.RuntimeHandle) (execution.VerifiedCheckpoint, error) {
	b, e := r.backend(ctx, h)
	if e != nil {
		return execution.VerifiedCheckpoint{}, e
	}
	return b.Checkpoint(ctx, h)
}
func (r *Providers) Cancel(ctx context.Context, h execution.RuntimeHandle) error {
	b, e := r.backend(ctx, h)
	if e != nil {
		return e
	}
	return b.Cancel(ctx, h)
}
func (r *Providers) Stop(ctx context.Context, h execution.RuntimeHandle) (execution.LiveCleanup, error) {
	b, e := r.backend(ctx, h)
	if e != nil {
		return execution.LiveCleanup{}, e
	}
	return b.Stop(ctx, h)
}
func (r *Providers) Reconcile(ctx context.Context, v execution.RecoveryRecord) (execution.RecoveredRun, error) {
	p, e := r.provider(v)
	if e != nil {
		return execution.RecoveredRun{}, e
	}
	return r.Backends[p].Reconcile(ctx, v)
}

var _ execution.LiveRuntime = (*Providers)(nil)
