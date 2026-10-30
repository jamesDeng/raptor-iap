package catalog

import (
	"context"

	"github.com/jamesDeng/raptor-iap/components/raptor/internal/domain"
)

// InfraResourceContext projects catalog identity without resolving deployments or
// consulting Infra. Keeping this read catalog-only prevents discovery recursion.
func (s *Service) InfraResourceContext(ctx context.Context, envCode, kind, code string) (domain.InfraResourceContext, error) {
	empty := domain.InfraResourceContext{}
	if !codePattern.MatchString(envCode) || !codePattern.MatchString(code) || (kind != "application" && kind != "database" && kind != "db-proxy") {
		return empty, domain.ErrInvalid
	}
	env, err := s.GetEnvironment(ctx, envCode)
	if err != nil {
		return empty, err
	}
	object, err := s.FindObject(ctx, kind, code)
	if err != nil {
		return empty, err
	}
	clusterID := env.ACKClusterID
	if raw, exists := env.Config["clusterId"]; exists {
		legacy, ok := raw.(string)
		if !ok {
			return empty, domain.ErrInvalid
		}
		if clusterID != "" && legacy != "" && clusterID != legacy {
			return empty, domain.ErrConflict
		}
		if clusterID == "" {
			clusterID = legacy
		}
	}
	out := domain.InfraResourceContext{
		EnvCode: env.Code, Kind: object.Kind, Code: object.Code,
		Cloud: env.Cloud, Region: env.Region, AccountID: env.AccountID, ClusterID: clusterID,
		RelatedResources: []domain.ObjectRef{},
	}
	if kind != "db-proxy" {
		return out, nil
	}
	raw, exists := env.Config["proxyApplications"]
	if !exists {
		return out, nil
	}
	mappings, ok := raw.(map[string]any)
	if !ok {
		return empty, domain.ErrInvalid
	}
	raw, exists = mappings[object.Code]
	if !exists {
		return out, nil
	}
	applications, ok := raw.([]any)
	if !ok || len(applications) != 1 {
		return empty, domain.ErrInvalid
	}
	appCode, ok := applications[0].(string)
	if !ok || !codePattern.MatchString(appCode) {
		return empty, domain.ErrInvalid
	}
	app, err := s.FindObject(ctx, "application", appCode)
	if err != nil {
		return empty, err
	}
	out.RelatedResources = append(out.RelatedResources, domain.ObjectRef{Kind: app.Kind, Code: app.Code})
	return out, nil
}
