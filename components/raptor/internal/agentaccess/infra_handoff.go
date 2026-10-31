package agentaccess

import (
	"context"
	"encoding/json"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/adapters"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/auth"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/domain"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/httpx"
	"net/http"
)

type infraPreparation struct {
	Eligible bool        `json:"eligible"`
	Prepared string      `json:"prepared"`
	Identity CheckResult `json:"identity"`
}
type infraValidation struct {
	Operation CheckInput                `json:"operation"`
	Prepared  string                    `json:"prepared"`
	Proof     *adapters.ResolutionProof `json:"proof,omitempty"`
}

func preparationDigest(in CheckInput, out CheckResult) string {
	raw, _ := json.Marshal(struct {
		Operation CheckInput
		Identity  CheckResult
	}{in, out})
	return digest(string(raw))
}
func (s *Service) prepareInfra(ctx context.Context, in CheckInput) (infraPreparation, error) {
	if in.Audience != "infra" {
		return infraPreparation{}, domain.ErrInvalid
	}
	out, e := s.checkEligibility(ctx, in)
	if e != nil || !out.Active {
		return infraPreparation{}, e
	}
	return infraPreparation{Eligible: true, Prepared: preparationDigest(in, out), Identity: out}, nil
}
func (s *Service) validateInfra(ctx context.Context, in infraValidation) (CheckResult, error) {
	before, e := s.prepareInfra(ctx, in.Operation)
	if e != nil {
		return CheckResult{}, e
	}
	if !before.Eligible || in.Prepared == "" || in.Prepared != before.Prepared {
		return CheckResult{}, nil
	}
	if before.Identity.ReplacementScope != nil {
		if in.Proof == nil || s.ValidateReplacement == nil {
			return CheckResult{}, domain.ErrInvalid
		}
		current, e := s.ValidateReplacement(ctx, *before.Identity.Binding, *in.Proof)
		if e != nil {
			return CheckResult{}, e
		}
		if current != *before.Identity.ReplacementScope {
			return CheckResult{}, nil
		}
	} else if in.Proof != nil {
		return CheckResult{}, domain.ErrInvalid
	}
	// Catalog validation may wait on the DB. Re-read eligibility after that work,
	// so revocation, deadline/control changes and definition edits cannot survive.
	final, e := s.prepareInfra(ctx, in.Operation)
	if e != nil || !final.Eligible || final.Prepared != in.Prepared {
		return CheckResult{}, e
	}
	return final.Identity, nil
}
func (s *Service) registerInfra(m *http.ServeMux, inspector auth.Credentials) {
	m.Handle("POST /v1/agent-access/prepare-infra", auth.BasicAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var in CheckInput
		if !httpx.DecodePrivate(w, r, &in) {
			return
		}
		out, e := s.prepareInfra(r.Context(), in)
		httpx.Result(w, 200, out, e)
	}), inspector))
	m.Handle("POST /v1/agent-access/validate-infra", auth.BasicAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var in infraValidation
		if !httpx.DecodePrivate(w, r, &in) {
			return
		}
		out, e := s.validateInfra(r.Context(), in)
		httpx.Result(w, 200, out, e)
	}), inspector))
}
