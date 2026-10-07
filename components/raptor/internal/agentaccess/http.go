package agentaccess

import (
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/auth"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/httpx"
	"net/http"
)

func (s *Service) Register(m *http.ServeMux, controller, inspector auth.Credentials) {
	m.Handle("POST /v1/requests/{id}/agent-access", auth.BasicAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var in IssueInput
		if !httpx.Decode(w, r, &in) {
			return
		}
		out, e := s.Issue(r.Context(), r.PathValue("id"), in)
		httpx.Result(w, 201, out, e)
	}), controller))
	m.Handle("DELETE /v1/requests/{id}/agent-access/{attemptId}", auth.BasicAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		e := s.Revoke(r.Context(), r.PathValue("id"), r.PathValue("attemptId"))
		httpx.Result(w, 200, map[string]bool{"revoked": e == nil}, e)
	}), controller))
	m.Handle("POST /v1/agent-access/introspect", auth.BasicAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var in CheckInput
		if !httpx.Decode(w, r, &in) {
			return
		}
		out, e := s.Check(r.Context(), in)
		httpx.Result(w, 200, out, e)
	}), inspector))
}
