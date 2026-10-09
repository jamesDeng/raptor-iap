package commands

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"raptor-iap/infra-api/internal/approval"
	"raptor-iap/infra-api/internal/domain"
	"raptor-iap/infra-api/internal/policy"
	"time"
)

type Fleet interface {
	FleetRecord(context.Context, approval.FleetIntent, string) error
	FleetClaim(context.Context, approval.FleetIntent, string) (approval.FleetResult, error)
	FleetResolve(context.Context, approval.FleetIntent, string) error
}

// A guard persists before submitting and stays pending after acknowledgement or
// uncertainty. Another call can resolve only a complete provider-observed effect.
func (r Runtime) guarded(ctx context.Context, env domain.Environment, intent approval.FleetIntent, run func(Runtime) error) error {
	if r.Fleet == nil {
		if r.evidence() == "live" {
			return domain.ErrNotConfigured
		}
		return run(r)
	}
	if intent.EnvCode != env.Code {
		return domain.ErrScope
	}
	tokenBytes := make([]byte, 16)
	if _, e := rand.Read(tokenBytes); e != nil {
		return domain.ErrNotConfigured
	}
	token := hex.EncodeToString(tokenBytes)
	acquired := false
	for attempt := 0; attempt < 2; attempt++ {
		claim, e := r.Fleet.FleetClaim(ctx, intent, token)
		if e != nil {
			return failure("FleetBusy")
		}
		if claim.Claimed {
			if claim.Token != token {
				return failure("FleetBusy")
			}
			acquired = true
			break
		}
		if !claim.Recorded {
			return failure("FleetBusy")
		}
		pending := claim.Intent
		if pending.EnvCode != intent.EnvCode || pending.GroupID != intent.GroupID || pending.ProxyCode != intent.ProxyCode {
			return failure("FleetBusy")
		}
		s, e := r.snapshot(ctx, env, domain.ProxyTarget{EnvCode: pending.EnvCode, ProxyCode: pending.ProxyCode, GroupID: pending.GroupID})
		if e != nil || !effectObserved(s, pending) {
			return failure("FleetBusy")
		}
		if e = r.Fleet.FleetResolve(ctx, pending, claim.Token); e != nil {
			return failure("FleetBusy")
		}
	}
	if !acquired {
		return failure("FleetBusy")
	}
	b := &submissionBackend{Backend: r.Backend}
	r.Backend = b
	e := run(r)
	if b.submitted {
		receipt, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		if err := r.Fleet.FleetRecord(receipt, intent, token); err != nil {
			return failure("SubmissionUnknown")
		}
	}
	if !b.submitted {
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		if err := r.Fleet.FleetResolve(cleanup, intent, token); err != nil {
			return failure("FleetBusy")
		}
	}
	return e
}
func effectObserved(s policy.ProxySnapshot, in approval.FleetIntent) bool {
	switch in.Kind {
	case "scale":
		return s.Desired == in.Desired && len(s.Nodes) == in.Desired
	case "protect", "deregister":
		if len(in.InstanceIDs) == 0 {
			return false
		}
		nodes := map[string]policy.Node{}
		for _, n := range s.Nodes {
			nodes[n.ID] = n
		}
		for _, id := range in.InstanceIDs {
			n, exists := nodes[id]
			if in.Kind == "protect" {
				if !exists || n.Protected != in.Protected {
					return false
				}
			} else if exists && n.Registered {
				return false
			}
		}
		return true
	}
	return false
}

type submissionBackend struct {
	Backend
	submitted bool
}

func (b *submissionBackend) Scale(c context.Context, e domain.Environment, g string, d int) (string, error) {
	b.submitted = true
	return b.Backend.Scale(c, e, g, d)
}
func (b *submissionBackend) Protect(c context.Context, e domain.Environment, g string, ids []string, p bool) error {
	b.submitted = true
	return b.Backend.Protect(c, e, g, ids, p)
}
func (b *submissionBackend) Deregister(c context.Context, e domain.Environment, g string, ids []string) error {
	b.submitted = true
	return b.Backend.Deregister(c, e, g, ids)
}
func (r Runtime) Scale(ctx context.Context, env domain.Environment, in domain.ScaleCommand) (out domain.ScaleReceipt, e error) {
	e = r.guarded(ctx, env, approval.FleetIntent{EnvCode: in.EnvCode, ProxyCode: in.ProxyCode, GroupID: in.GroupID, Kind: "scale", Desired: in.DesiredCapacity}, func(inner Runtime) error { var err error; out, err = inner.scale(ctx, env, in); return err })
	return
}
func (r Runtime) SetProtection(ctx context.Context, env domain.Environment, in domain.ProtectionCommand) (out domain.NodeReceipt, e error) {
	e = r.guarded(ctx, env, approval.FleetIntent{EnvCode: in.EnvCode, ProxyCode: in.ProxyCode, GroupID: in.GroupID, Kind: "protect", InstanceIDs: in.InstanceIDs, Protected: in.Protected}, func(inner Runtime) error { var err error; out, err = inner.setProtection(ctx, env, in); return err })
	return
}
func (r Runtime) Deregister(ctx context.Context, env domain.Environment, in domain.DeregisterCommand) (out domain.NodeReceipt, e error) {
	e = r.guarded(ctx, env, approval.FleetIntent{EnvCode: in.EnvCode, ProxyCode: in.ProxyCode, GroupID: in.GroupID, Kind: "deregister", InstanceIDs: in.InstanceIDs}, func(inner Runtime) error { var err error; out, err = inner.deregister(ctx, env, in); return err })
	return
}
