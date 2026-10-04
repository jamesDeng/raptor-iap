package requests

import (
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/auth"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/domain"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/httpx"
	"net/http"
)

func (s *Service) Register(m *http.ServeMux, a *auth.Service) {
	m.Handle("POST /api/v1/requests/{id}/actions", a.Browser(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Action       string `json:"action"`
			Instructions string `json:"instructions"`
		}
		if !httpx.Decode(w, r, &in) {
			return
		}
		e := s.Action(r.Context(), r.PathValue("id"), in.Action, in.Instructions)
		httpx.Result(w, 200, map[string]bool{"accepted": e == nil}, e)
	})))
	m.Handle("GET /api/v1/operation-schemas", a.Browser(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { httpx.Write(w, 200, Schemas()) })))
	m.Handle("POST /api/v1/requests", a.Browser(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var in domain.RequestInput
		if !httpx.Decode(w, r, &in) {
			return
		}
		v, e := s.Create(r.Context(), auth.UserFrom(r), r.Header.Get("Idempotency-Key"), in)
		httpx.Result(w, 201, v, e)
	})))
	m.Handle("GET /api/v1/requests", a.Browser(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { v, e := s.List(r.Context()); httpx.Result(w, 200, v, e) })))
	m.Handle("GET /api/v1/requests/{id}", a.Browser(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		v, e := s.Get(r.Context(), r.PathValue("id"))
		httpx.Result(w, 200, v, e)
	})))
}
