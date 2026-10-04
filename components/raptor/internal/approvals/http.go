package approvals

import (
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/auth"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/httpx"
	"net/http"
)

func (s *Service) RegisterBrowser(m *http.ServeMux, a *auth.Service) {
	m.Handle("POST /api/v1/requests/{requestId}/approvals/{id}/decision", a.Browser(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		existing, e := s.Get(r.Context(), r.PathValue("id"))
		if e != nil {
			httpx.Result(w, 200, nil, e)
			return
		}
		if existing.RequestID != r.PathValue("requestId") {
			httpx.Error(w, 404, "NotFound")
			return
		}
		var in DecisionInput
		if !httpx.Decode(w, r, &in) {
			return
		}
		v, e := s.Decide(r.Context(), auth.UserFrom(r), existing.ID, in)
		httpx.Result(w, 200, v, e)
	})))
}
