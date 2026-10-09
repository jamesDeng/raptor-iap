package backend

import (
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/agentaccess"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/approvals"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/auth"
	githubservice "github.com/jamesDeng/raptor-iap/components/raptor/internal/github"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/httpx"
	"net/http"
	"os"
)

func (s *Server) RegisterService(user, password string) {
	access := &agentaccess.Service{Pool: s.Requests.Pool, RaptorMCPURL: os.Getenv("RAPTOR_AGENT_MCP_URL"), InfraMCPURL: os.Getenv("INFRA_AGENT_MCP_URL")}
	access.Register(s.Mux, auth.Credentials{Username: user, Password: password}, auth.Credentials{Username: os.Getenv("AGENT_INTROSPECTION_USERNAME"), Password: os.Getenv("AGENT_INTROSPECTION_PASSWORD")})
	wrap := func(pattern string, h http.HandlerFunc) {
		s.Mux.Handle(pattern, auth.BasicAuth(h, auth.Credentials{Username: user, Password: password}))
	}
	wrap("POST /v1/requests/{id}/pull-requests", func(w http.ResponseWriter, r *http.Request) {
		var in githubservice.PRInput
		if !httpx.Decode(w, r, &in) {
			return
		}
		e := s.GitHub.AttachPR(r.Context(), r.PathValue("id"), in)
		httpx.Result(w, 201, map[string]bool{"attached": e == nil}, e)
	})
	wrap("POST /v1/requests/{id}/pause", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Reason string `json:"reason"`
		}
		if !httpx.Decode(w, r, &in) {
			return
		}
		e := s.Requests.Pause(r.Context(), r.PathValue("id"), in.Reason)
		httpx.Result(w, 200, map[string]bool{"accepted": e == nil}, e)
	})
	wrap("GET /v1/requests/{id}/context", func(w http.ResponseWriter, r *http.Request) {
		v, e := s.Requests.Get(r.Context(), r.PathValue("id"))
		if e != nil {
			httpx.Result(w, 200, nil, e)
			return
		}
		fingerprint, hashError := agentaccess.Fingerprint(v.Definition)
		if hashError != nil {
			httpx.Error(w, 503, "Unavailable")
			return
		}
		env, e := s.Catalog.GetEnvironment(r.Context(), v.Definition.EnvCode)
		httpx.Result(w, 200, map[string]any{"requestId": v.ID, "definition": v.Definition, "environment": env, "skills": v.Definition.Skills, "definitionSha256": fingerprint}, e)
	})
	wrap("GET /v1/environments/{code}", func(w http.ResponseWriter, r *http.Request) {
		v, e := s.Catalog.GetEnvironment(r.Context(), r.PathValue("code"))
		httpx.Result(w, 200, v, e)
	})
	wrap("GET /v1/objects/{kind}/{code}", func(w http.ResponseWriter, r *http.Request) {
		v, e := s.Catalog.FindObject(r.Context(), r.PathValue("kind"), r.PathValue("code"))
		httpx.Result(w, 200, v, e)
	})
	wrap("GET /v1/requests/{id}/deployments", func(w http.ResponseWriter, r *http.Request) {
		v, e := s.Requests.Get(r.Context(), r.PathValue("id"))
		if e != nil {
			httpx.Result(w, 200, nil, e)
			return
		}
		if r.URL.Query().Get("env") != v.Definition.EnvCode {
			httpx.Error(w, 400, "InvalidInput")
			return
		}
		o, e := s.Catalog.FindObject(r.Context(), v.Definition.Object.Kind, v.Definition.Object.Code)
		if e != nil {
			httpx.Result(w, 200, nil, e)
			return
		}
		d, e := s.Catalog.Deployments(r.Context(), o.ID, v.Definition.EnvCode)
		httpx.Result(w, 200, d, e)
	})
	wrap("POST /v1/requests/{id}/approvals", func(w http.ResponseWriter, r *http.Request) {
		var in approvals.ApprovalInput
		if !httpx.Decode(w, r, &in) {
			return
		}
		if in.RequestID != r.PathValue("id") {
			httpx.Error(w, 400, "InvalidInput")
			return
		}
		v, e := s.Approvals.Request(r.Context(), in)
		httpx.Result(w, 201, v, e)
	})
	wrap("GET /v1/requests/{id}/approvals/{approvalId}", func(w http.ResponseWriter, r *http.Request) {
		v, e := s.Approvals.Get(r.Context(), r.PathValue("approvalId"))
		if e == nil && v.RequestID != r.PathValue("id") {
			httpx.Error(w, 404, "NotFound")
			return
		}
		httpx.Result(w, 200, v, e)
	})
	wrap("POST /v1/approval-check", func(w http.ResponseWriter, r *http.Request) {
		var in approvals.ApprovalCheckInput
		if !httpx.Decode(w, r, &in) {
			return
		}
		v, e := s.Approvals.Check(r.Context(), in)
		httpx.Result(w, 200, v, e)
	})
	wrap("POST /v1/approval-claim", func(w http.ResponseWriter, r *http.Request) {
		var in approvals.ApprovalInput
		if !httpx.Decode(w, r, &in) {
			return
		}
		v, e := s.Approvals.Claim(r.Context(), in)
		httpx.Result(w, 200, v, e)
	})
	wrap("POST /v1/approval-record", func(w http.ResponseWriter, r *http.Request) {
		var in approvals.SubmissionRecordInput
		if !httpx.Decode(w, r, &in) {
			return
		}
		e := s.Approvals.RecordSubmission(r.Context(), in)
		httpx.Result(w, 200, map[string]bool{"recorded": e == nil}, e)
	})

}
