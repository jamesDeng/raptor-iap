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

type infraGrantClaim struct {
	Action approvals.GrantActionInput `json:"action"`
	Proof  adapters.ResolutionProof   `json:"proof"`
}

func (s *Server) registerInfraRequestGrants(access *agentaccess.Service, credentials auth.Credentials) {
	s.Mux.Handle("POST /v1/infra/request-grant-claim", auth.BasicAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var in infraGrantClaim
		if !httpx.DecodePrivate(w, r, &in) {
			return
		}
		identity := in.Proof.Identity
		if digest := identity.Digest(); digest == "" || digest != in.Proof.ResolutionDigest ||
			digest != in.Action.ScopeDigest || identity.EnvCode != in.Action.EnvCode {
			httpx.Result(w, 200, nil, domain.ErrForbidden)
			return
		}
		target := in.Action.Capability.Target
		if target != (approvals.GrantTarget{Kind: "db-proxy", Code: identity.ProxyCode}) &&
			target != (approvals.GrantTarget{Kind: "application", Code: identity.Application.AppCode}) {
			httpx.Result(w, 200, nil, domain.ErrForbidden)
			return
		}
		// The fresh provider proof came from Infra during the command. Validating
		// it does not call back into Infra while that command is waiting.
		service := *s.Approvals
		service.VerifyGrantScope = func(ctx context.Context, grant approvals.RequestGrantInput) (approvals.ResourceVerification, error) {
			return access.PrepareRequestReplacementProof(ctx, grant.RequestID, grant.EnvCode, identity.ProxyCode, true, in.Proof)
		}
		out, err := service.ClaimGrantAction(r.Context(), in.Action)
		httpx.Result(w, 200, out, err)
	}), credentials))

	s.Mux.Handle("POST /v1/infra/request-grant-record", auth.BasicAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var in approvals.GrantActionRecord
		if !httpx.DecodePrivate(w, r, &in) {
			return
		}
		err := s.Approvals.RecordGrantAction(r.Context(), in)
		httpx.Result(w, 200, map[string]bool{"recorded": err == nil}, err)
	}), credentials))
}
