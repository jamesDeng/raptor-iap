package agentaccess

import (
	"context"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/adapters"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/domain"
)

// Scope is a controller-owned POC dependency binding, never a request parameter.
// Revalidate discovered identities on issue and introspection; a changed UID
// requires a new reviewed binding rather than silently retargeting execution.
type ReplacementResolver struct {
	Scope ReplacementScope
	Read  func(context.Context, string, string, string) ([]adapters.Deployment, error)
}

func (r ReplacementResolver) Resolve(ctx context.Context, b Binding) (ReplacementScope, error) {
	s := r.Scope
	if r.Read == nil || !s.ValidFor(b) {
		return ReplacementScope{}, domain.ErrUnavailable
	}
	read := func(kind, code string) ([]adapters.Deployment, error) {
		rows, err := r.Read(ctx, kind, code, s.EnvCode)
		if err != nil || len(rows) == 0 || len(rows) > 10 {
			return nil, domain.ErrUnavailable
		}
		for _, row := range rows {
			if row.Kind != kind || row.ObjectCode != code || row.EnvCode != s.EnvCode || row.EvidenceMode != "live" {
				return nil, domain.ErrUnavailable
			}
		}
		return rows, nil
	}
	proxies, err := read("db-proxy", s.ProxyCode)
	if err != nil || len(proxies) != 1 || proxies[0].ResourceID != s.GroupID || proxies[0].TargetDBCode != s.TargetDBCode {
		return ReplacementScope{}, domain.ErrUnavailable
	}
	databases, err := read("db", s.TargetDBCode)
	if err != nil || len(databases) != 1 || databases[0].ResourceID == "" {
		return ReplacementScope{}, domain.ErrUnavailable
	}
	apps, err := read("application", s.Application.AppCode)
	if err != nil {
		return ReplacementScope{}, domain.ErrUnavailable
	}
	matches := 0
	for _, app := range apps {
		if app.ClusterID == s.Application.ClusterID && app.Namespace == s.Application.Namespace && app.Name == s.Application.Name && app.UID == s.Application.UID {
			matches++
		}
	}
	if matches != 1 {
		return ReplacementScope{}, domain.ErrUnavailable
	}
	return s, nil
}
