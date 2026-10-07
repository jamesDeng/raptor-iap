package api

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"raptor-iap/infra-api/internal/domain"
	"strings"
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
	op := operations{reader: reader, scopes: scopes}
	mcpHandler := newMCPHandler(op)
	expected := sha256.Sum256([]byte(username + ":" + password))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		fail := func(status int, code string) {
			w.WriteHeader(status)
			json.NewEncoder(w).Encode(map[string]any{"error": map[string]string{"code": code}})
		}
		send := func(data any) { json.NewEncoder(w).Encode(map[string]any{"data": data}) }
		if r.URL.Path != "/mcp" && r.Method != "GET" {
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
		if r.URL.Path == "/mcp" {
			mcpHandler.ServeHTTP(w, r)
			return
		}
		q, e := url.ParseQuery(r.URL.RawQuery)
		if e != nil || len(r.URL.RawQuery) > 4096 {
			fail(400, "InvalidInput")
			return
		}
		data, e := op.execute(r.Context(), r.URL.Path, q)
		if e != nil {
			oe := e.(*operationError)
			fail(oe.status, oe.code)
			return
		}

		send(data)
	}), nil
}
