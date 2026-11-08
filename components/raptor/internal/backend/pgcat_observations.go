package backend

import (
	"net/http"

	"github.com/jamesDeng/raptor-iap/components/raptor/internal/approvals"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/auth"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/domain"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/httpx"
)

func (s *Server) registerPgcatObservations(credentials auth.Credentials) {
	s.Mux.Handle("POST /v1/requests/{id}/pgcat-observations", auth.BasicAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var in approvals.PgcatObservation
		if !httpx.DecodePrivate(w, r, &in) {
			return
		}
		if in.RequestID != r.PathValue("id") {
			httpx.Result(w, 200, nil, domain.ErrInvalid)
			return
		}
		err := s.Approvals.RecordPgcatObservation(r.Context(), in)
		httpx.Result(w, 201, map[string]bool{"recorded": err == nil}, err)
	}), credentials))
	s.Mux.Handle("GET /v1/requests/{id}/pgcat-request-state", auth.BasicAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		out, err := s.Approvals.ReadPgcatRequestState(r.Context(), r.PathValue("id"), r.URL.Query().Get("scopeDigest"))
		httpx.Result(w, 200, out, err)
	}), credentials))
	s.Mux.Handle("POST /v1/infra/pgcat-request-state", auth.BasicAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			RequestID   string `json:"requestId"`
			ScopeDigest string `json:"scopeDigest"`
		}
		if !httpx.DecodePrivate(w, r, &in) {
			return
		}
		out, err := s.Approvals.ReadPgcatRequestState(r.Context(), in.RequestID, in.ScopeDigest)
		httpx.Result(w, 200, out, err)
	}), credentials))
}
