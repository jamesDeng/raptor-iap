package resources

import (
	"context"
	"raptor-iap/infra-api/internal/domain"
	"time"
)

type Resolver interface {
	Resolve(context.Context, string, string, string) (domain.ResolvedResource, error)
}

// ResourceBackend only depends on domain; providers never depend on this package.
type ResourceBackend interface {
	ResolveResource(context.Context, domain.ResourceContext) (domain.ResolvedResource, error)
}
type Service struct {
	catalog Catalog
	backend ResourceBackend
}

func NewResolver(c Catalog, b ResourceBackend) *Service { return &Service{catalog: c, backend: b} }
func (s *Service) Resolve(ctx context.Context, env, kind, code string) (domain.ResolvedResource, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	return s.resolve(ctx, env, kind, code)
}
func (s *Service) resolve(ctx context.Context, env, kind, code string) (domain.ResolvedResource, error) {
	if ctx.Err() != nil {
		return domain.ResolvedResource{}, ctx.Err()
	}
	if s == nil || s.catalog == nil || s.backend == nil {
		return domain.ResolvedResource{}, domain.ErrNotConfigured
	}
	if !selectors(env, kind, code) {
		return domain.ResolvedResource{}, domain.ErrScope
	}
	c, e := s.catalog.Context(ctx, env, kind, code)
	if e != nil {
		return domain.ResolvedResource{}, e
	}
	if e = validateContext(c, env, kind); e != nil {
		return domain.ResolvedResource{}, e
	}
	r, e := s.backend.ResolveResource(ctx, c)
	if e != nil {
		return domain.ResolvedResource{}, e
	}
	r.Context = c
	r.ResolutionDigest = ""
	if kind == "db-proxy" {
		if r.Proxy == nil || !logicalCode.MatchString(r.Proxy.TargetDBCode) {
			return domain.ResolvedResource{}, domain.ErrScope
		}
		db, e := s.catalog.Context(ctx, env, "database", r.Proxy.TargetDBCode)
		if e != nil {
			return domain.ResolvedResource{}, e
		}
		if e = validateContext(db, env, "database"); e != nil || !sameEnvironment(c, db) {
			return domain.ResolvedResource{}, domain.ErrScope
		}
		// The association is a canonical catalog code, never a provider ID or alias.
		if db.Code != r.Proxy.TargetDBCode {
			return domain.ResolvedResource{}, domain.ErrScope
		}
		resolved, e := s.backend.ResolveResource(ctx, db)
		if e != nil {
			return domain.ResolvedResource{}, e
		}
		if len(resolved.DatabaseWorkloads) != 1 || resolved.DatabaseWorkloads[0].ResourceID == "" {
			return domain.ResolvedResource{}, domain.ErrIdentity
		}
		r.DatabaseWorkloads = resolved.DatabaseWorkloads
	}
	return r, nil
}
func sameEnvironment(a, b domain.ResourceContext) bool {
	return a.EnvCode == b.EnvCode && a.AccountID == b.AccountID && a.Cloud == b.Cloud && a.Region == b.Region && a.ClusterID == b.ClusterID
}
