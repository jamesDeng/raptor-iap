package agentaccess

import (
	"context"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/adapters"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/domain"
)

// ReplacementResolver combines catalog-only relationships with a trusted Infra
// proof. No file or manually maintained provider mapping can authorize execution.
type ReplacementResolver struct {
	Context func(context.Context, string, string, string) (domain.InfraResourceContext, error)
	Proof   func(context.Context, string, string) (adapters.ResolutionProof, error)
}

func (r ReplacementResolver) Resolve(ctx context.Context, b Binding) (ReplacementScope, error) {
	if r.Context == nil || r.Proof == nil {
		return ReplacementScope{}, domain.ErrUnavailable
	}
	c, err := r.Context(ctx, b.EnvCode, "db-proxy", b.ObjectCode)
	if err != nil || c.EnvCode != b.EnvCode || c.Kind != "db-proxy" || c.Code != b.ObjectCode || c.ClusterID != b.ClusterID || len(c.RelatedResources) != 1 || c.RelatedResources[0].Kind != "application" {
		return ReplacementScope{}, domain.ErrUnavailable
	}
	proof, err := r.Proof(ctx, b.EnvCode, b.ObjectCode)
	if err != nil {
		return ReplacementScope{}, domain.ErrUnavailable
	}
	return scopeFromProof(c, b, proof)
}

// Validate uses only current catalog and the fresh proof from trusted Infra;
// it never calls back into Infra while that invocation is occupied.
func (r ReplacementResolver) Validate(ctx context.Context, b Binding, proof adapters.ResolutionProof) (ReplacementScope, error) {
	if r.Context == nil {
		return ReplacementScope{}, domain.ErrUnavailable
	}
	c, err := r.Context(ctx, b.EnvCode, "db-proxy", b.ObjectCode)
	if err != nil || c.EnvCode != b.EnvCode || c.Kind != "db-proxy" || c.Code != b.ObjectCode || c.ClusterID != b.ClusterID || len(c.RelatedResources) != 1 || c.RelatedResources[0].Kind != "application" {
		return ReplacementScope{}, domain.ErrUnavailable
	}
	return scopeFromProof(c, b, proof)
}

func scopeFromProof(c domain.InfraResourceContext, b Binding, proof adapters.ResolutionProof) (ReplacementScope, error) {
	i := proof.Identity
	if i.EnvCode != c.EnvCode || i.ProxyCode != c.Code || i.AccountID != c.AccountID || i.Region != c.Region || i.ClusterID != c.ClusterID || i.Application.AppCode != c.RelatedResources[0].Code {
		return ReplacementScope{}, domain.ErrUnavailable
	}
	s := ReplacementScope{AccountID: i.AccountID, Region: i.Region, ListenerID: i.ListenerID, Port: i.Port, DatabaseResourceID: i.DatabaseResourceID, ResolutionDigest: proof.ResolutionDigest, EnvCode: i.EnvCode, ProxyCode: i.ProxyCode, GroupID: i.GroupID, ServerGroupID: i.ServerGroupID, TargetDBCode: i.TargetDBCode, Application: ApplicationScope{EnvCode: i.Application.EnvCode, AppCode: i.Application.AppCode, ClusterID: i.Application.ClusterID, Namespace: i.Application.Namespace, Name: i.Application.Name, UID: i.Application.UID}}
	if !s.ResourceValidFor(b) {
		return ReplacementScope{}, domain.ErrUnavailable
	}
	return s, nil
}
