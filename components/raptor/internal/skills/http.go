package skills

import (
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/auth"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/domain"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/httpx"
	"net/http"
)

func (s *Service) Register(m *http.ServeMux, a *auth.Service) {
	m.Handle("GET /api/v1/skills/releases", a.Browser(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.Source == nil {
			httpx.Result(w, 200, nil, domain.ErrUnavailable)
			return
		}
		versions, e := s.Source.List(r.Context())
		httpx.Result(w, 200, versions, e)
	})))
	m.Handle("POST /api/v1/requests/{id}/skills", a.Browser(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var in SkillsChangeInput
		if !httpx.Decode(w, r, &in) {
			return
		}
		e := s.Change(r.Context(), auth.UserFrom(r), r.PathValue("id"), in)
		httpx.Result(w, 200, map[string]bool{"accepted": e == nil}, e)
	})))
}
