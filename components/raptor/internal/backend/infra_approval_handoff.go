package backend

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/adapters"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/agentaccess"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/approvals"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/auth"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/domain"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/httpx"
	"net/http"
)

type infraApprovalOperation struct {
	Operation string                  `json:"operation"`
	Binding   approvals.ApprovalInput `json:"binding"`
}
type infraApprovalValidation struct {
	Operation string                   `json:"operation"`
	Binding   approvals.ApprovalInput  `json:"binding"`
	Prepared  string                   `json:"prepared"`
	Proof     adapters.ResolutionProof `json:"proof"`
}
type infraApprovalPreparation struct {
	Eligible bool   `json:"eligible"`
	Prepared string `json:"prepared"`
}

func (s *Server) registerInfraApprovals(access *agentaccess.Service, credentials auth.Credentials) {
	prepare := func(ctx context.Context, in infraApprovalOperation) (infraApprovalPreparation, error) {
		snapshot, e := s.Approvals.PrepareResource(ctx, in.Binding, in.Operation)
		if e != nil || snapshot == "" {
			return infraApprovalPreparation{}, e
		}
		proxy, _ := in.Binding.Target["proxyCode"].(string)
		pins, e := access.RequestReplacementSnapshot(ctx, in.Binding.RequestID, in.Binding.EnvCode, proxy, true)
		if e != nil {
			return infraApprovalPreparation{}, e
		}
		hash := sha256.Sum256([]byte(snapshot + ":" + pins))
		return infraApprovalPreparation{Eligible: true, Prepared: hex.EncodeToString(hash[:])}, nil
	}
	s.Mux.Handle("POST /v1/infra/approval-prepare", auth.BasicAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var in infraApprovalOperation
		if !httpx.DecodePrivate(w, r, &in) {
			return
		}
		out, e := prepare(r.Context(), in)
		httpx.Result(w, 200, out, e)
	}), credentials))
	s.Mux.Handle("POST /v1/infra/approval-validate", auth.BasicAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var in infraApprovalValidation
		if !httpx.DecodePrivate(w, r, &in) {
			return
		}
		current, e := prepare(r.Context(), infraApprovalOperation{in.Operation, in.Binding})
		if e != nil {
			httpx.Result(w, 200, nil, e)
			return
		}
		if !current.Eligible || in.Prepared == "" || current.Prepared != in.Prepared {
			httpx.Result(w, 200, nil, domain.ErrConflict)
			return
		}
		// Per-call copy reuses the exact Check/Claim algorithms and locked DB checks.
		// The public request DTO never accepts proof or a validation bypass.
		service := *s.Approvals
		service.VerifyResource = func(ctx context.Context, b approvals.ApprovalInput) (approvals.ResourceVerification, error) {
			proxy, _ := b.Target["proxyCode"].(string)
			return access.PrepareRequestReplacementProof(ctx, b.RequestID, b.EnvCode, proxy, true, in.Proof)
		}
		if in.Operation == "check" {
			out, e := service.Check(r.Context(), in.Binding)
			httpx.Result(w, 200, out, e)
		} else {
			out, e := service.Claim(r.Context(), in.Binding)
			httpx.Result(w, 200, out, e)
		}
	}), credentials))
}
