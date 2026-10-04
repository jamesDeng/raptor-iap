package catalog

import (
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/auth"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/domain"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/httpx"
	"net/http"
)

func (s *Service) Register(m *http.ServeMux, a *auth.Service) {
	m.Handle("GET /api/v1/objects", a.Browser(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		v, e := s.ListObjects(r.Context())
		httpx.Result(w, 200, v, e)
	})))
	m.Handle("POST /api/v1/objects", a.Browser(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var in CreateObjectInput
		if !httpx.Decode(w, r, &in) {
			return
		}
		v, e := s.CreateObject(r.Context(), in)
		httpx.Result(w, 201, v, e)
	})))
	m.Handle("GET /api/v1/objects/{id}", a.Browser(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		v, e := s.GetObject(r.Context(), r.PathValue("id"))
		httpx.Result(w, 200, v, e)
	})))
	m.Handle("PATCH /api/v1/objects/{id}", a.Browser(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var in UpdateObjectInput
		if !httpx.Decode(w, r, &in) {
			return
		}
		v, e := s.UpdateObject(r.Context(), r.PathValue("id"), in)
		httpx.Result(w, 200, v, e)
	})))
	m.Handle("GET /api/v1/objects/{id}/deployments", a.Browser(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		v, e := s.Deployments(r.Context(), r.PathValue("id"), r.URL.Query().Get("env"))
		httpx.Result(w, 200, v, e)
	})))
	m.Handle("GET /api/v1/environment-groups", a.Browser(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		v, e := s.ListGroups(r.Context())
		httpx.Result(w, 200, v, e)
	})))
	m.Handle("POST /api/v1/environment-groups", a.Admin(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Code string `json:"code"`
			Name string `json:"name"`
		}
		if !httpx.Decode(w, r, &in) {
			return
		}
		e := s.CreateGroup(r.Context(), in.Code, in.Name)
		httpx.Result(w, 201, in, e)
	})))
	m.Handle("GET /api/v1/environments", a.Browser(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		v, e := s.ListEnvironments(r.Context())
		httpx.Result(w, 200, v, e)
	})))
	m.Handle("POST /api/v1/environments", a.Admin(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var in domain.Environment
		if !httpx.Decode(w, r, &in) {
			return
		}
		e := s.SaveEnvironment(r.Context(), in, false)
		httpx.Result(w, 201, in, e)
	})))
	m.Handle("GET /api/v1/environments/{code}", a.Browser(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		v, e := s.GetEnvironment(r.Context(), r.PathValue("code"))
		httpx.Result(w, 200, v, e)
	})))
	m.Handle("PATCH /api/v1/environments/{code}", a.Admin(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var in domain.Environment
		if !httpx.Decode(w, r, &in) {
			return
		}
		if in.Code != r.PathValue("code") {
			httpx.Error(w, 400, "InvalidInput")
			return
		}
		e := s.SaveEnvironment(r.Context(), in, true)
		httpx.Result(w, 200, in, e)
	})))
}
