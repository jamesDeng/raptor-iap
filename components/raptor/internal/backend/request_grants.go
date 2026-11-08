package backend

import (
	"context"
	"net/http"

	"github.com/jamesDeng/raptor-iap/components/raptor/internal/adapters"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/agentaccess"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/approvals"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/auth"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/domain"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/httpx"
)

func (s *Server) requestApprovalPolicies(ctx context.Context, env string) (map[string]adapters.ToolApprovalPolicy, error) {
	reader, ok := s.Catalog.Infra.(interface {
		OperationApprovalPolicies(context.Context, string) (map[string]adapters.ToolApprovalPolicy, error)
	})
	if !ok {
		return nil, domain.ErrUnavailable
	}
	return reader.OperationApprovalPolicies(ctx, env)
}

func (s *Server) prepareRequestGrant(ctx context.Context, access *agentaccess.Service, in approvals.RequestGrantInput) (approvals.ResourceVerification, error) {
	request, err := s.Requests.Get(ctx, in.RequestID)
	if err != nil {
		return nil, err
	}
	def := request.Definition
	if def.Type != "agent" || def.Object.Kind != "db-proxy" || def.EnvCode != in.EnvCode ||
		agentaccess.RequestGrantProtocol(def) != "request-grants-v1" {
		return nil, domain.ErrForbidden
	}
	scope, err := access.RequestGrantReplacementScope(ctx, in.RequestID, in.EnvCode, def.Object.Code)
	if err != nil {
		return nil, err
	}
	if scope.ResolutionDigest != in.ScopeDigest {
		return nil, domain.ErrForbidden
	}
	policies, err := s.requestApprovalPolicies(ctx, in.EnvCode)
	if err != nil {
		return nil, err
	}
	targets := map[string]string{"db-proxy": scope.ProxyCode, "application": scope.Application.AppCode}
	allowed := map[string]string{}
	for _, policy := range policies {
		for _, variant := range policy.Variants {
			if variant.RequiresHumanApproval {
				allowed[variant.Capability] = variant.TargetKind
			}
		}
	}
	for _, capability := range in.Capabilities {
		kind := allowed[capability.Capability]
		if kind == "" || capability.Target.Kind != kind || capability.Target.Code != targets[kind] {
			return nil, domain.ErrForbidden
		}
	}
	return access.PrepareRequestGrantScope(ctx, in.RequestID, in.EnvCode, def.Object.Code, in.ScopeDigest)
}

func (s *Server) registerRequestGrants(access *agentaccess.Service, credentials auth.Credentials) {
	s.Approvals.VerifyGrantScope = func(ctx context.Context, in approvals.RequestGrantInput) (approvals.ResourceVerification, error) {
		return s.prepareRequestGrant(ctx, access, in)
	}
	s.Mux.Handle("POST /v1/requests/{id}/grants", auth.BasicAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var in approvals.RequestGrantInput
		if !httpx.DecodePrivate(w, r, &in) {
			return
		}
		if in.RequestID != r.PathValue("id") {
			httpx.Result(w, 200, nil, domain.ErrInvalid)
			return
		}
		out, err := s.Approvals.RequestGrant(r.Context(), in)
		httpx.Result(w, 201, out, err)
	}), credentials))
	s.Mux.Handle("GET /v1/requests/{id}/grants/{grantId}", auth.BasicAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		out, err := s.Approvals.GetRequestGrant(r.Context(), r.PathValue("id"), r.PathValue("grantId"))
		httpx.Result(w, 200, out, err)
	}), credentials))
}
