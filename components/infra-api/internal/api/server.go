package api

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"raptor-iap/infra-api/internal/domain"
	"strings"
	"time"
)

type Reader interface {
	Identity(context.Context, domain.Environment) (string, error)
	Deployments(context.Context, domain.Environment, string, string) ([]domain.Deployment, error)
	Status(context.Context, domain.Environment, domain.Target) (domain.Status, error)
}

func New(reader Reader, envs map[string]domain.Environment, username, password, authHeader string) (http.Handler, error) {
	if reader == nil || username == "" || password == "" || strings.Contains(username, ":") || (authHeader != "Authorization" && authHeader != "X-Infra-Authorization") {
		return nil, errors.New("invalid server configuration")
	}
	scopes := make(map[string]domain.Environment, len(envs))
	for k, v := range envs {
		scopes[k] = v
	}
	expected := sha256.Sum256([]byte(username + ":" + password))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		fail := func(status int, code string) {
			w.WriteHeader(status)
			json.NewEncoder(w).Encode(map[string]any{"error": map[string]string{"code": code}})
		}
		send := func(data any) { json.NewEncoder(w).Encode(map[string]any{"data": data}) }
		if r.Method != "GET" {
			fail(405, "MethodNotAllowed")
			return
		}
		if r.URL.Path == "/healthz" {
			send(map[string]bool{"ready": true})
			return
		}
		values := r.Header.Values(authHeader)
		if len(values) != 1 || !strings.HasPrefix(values[0], "Basic ") {
			fail(401, "Unauthenticated")
			return
		}
		decoded, e := base64.StdEncoding.Strict().DecodeString(strings.TrimPrefix(values[0], "Basic "))
		sum := sha256.Sum256(decoded)
		if e != nil || subtle.ConstantTimeCompare(sum[:], expected[:]) != 1 {
			fail(401, "Unauthenticated")
			return
		}
		var allowed []string
		switch r.URL.Path {
		case "/v1/cloud/identity":
			allowed = []string{"envCode"}
		case "/v1/deployments":
			allowed = []string{"envCode", "kind", "code"}
		case "/v1/deployment-status":
			allowed = []string{"envCode", "appCode", "clusterId", "namespace", "name", "uid"}
		default:
			fail(404, "NotFound")
			return
		}
		q, e := r.URL.Query(), error(nil)
		if len(r.URL.RawQuery) > 4096 {
			fail(400, "InvalidInput")
			return
		}
		for k, v := range q {
			found := false
			for _, a := range allowed {
				if k == a {
					found = true
				}
			}
			if !found || len(v) != 1 || len(v[0]) == 0 || len(v[0]) > 256 {
				fail(400, "InvalidInput")
				return
			}
		}
		for _, a := range allowed {
			if q.Get(a) == "" {
				fail(400, "InvalidInput")
				return
			}
		}
		env, ok := scopes[q.Get("envCode")]
		if !ok {
			fail(403, "ScopeMismatch")
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 25*time.Second)
		defer cancel()
		var data any
		switch r.URL.Path {
		case "/v1/cloud/identity":
			var account string
			account, e = reader.Identity(ctx, env)
			if e == nil && account != env.AccountID {
				e = domain.ErrScope
			}
			data = map[string]any{"envCode": env.Code, "accountMatches": true, "evidenceMode": "live"}
		case "/v1/deployments":
			kind := q.Get("kind")
			if kind != "application" && kind != "database" && kind != "db-proxy" {
				fail(400, "InvalidInput")
				return
			}
			var rows []domain.Deployment
			rows, e = reader.Deployments(ctx, env, kind, q.Get("code"))
			if rows == nil {
				rows = []domain.Deployment{}
			}
			data = rows
		case "/v1/deployment-status":
			data, e = reader.Status(ctx, env, domain.Target{EnvCode: env.Code, AppCode: q.Get("appCode"), ClusterID: q.Get("clusterId"), Namespace: q.Get("namespace"), Name: q.Get("name"), UID: q.Get("uid")})
		}
		if e != nil {
			switch {
			case errors.Is(e, domain.ErrScope):
				fail(403, "ScopeMismatch")
			case errors.Is(e, domain.ErrIdentity):
				fail(409, "IdentityChanged")
			case errors.Is(e, domain.ErrNotConfigured):
				fail(503, "NotConfigured")
			default:
				fail(503, "ProviderUnavailable")
			}
			return
		}
		send(data)
	}), nil
}
