package api

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"raptor-iap/infra-api/internal/domain"
	"raptor-iap/infra-api/internal/resources"
	"strings"
)

type ResourceResolver interface {
	Resolve(context.Context, string, string, string) (domain.ResolvedResource, error)
	ResolveProof(context.Context, string, string) (resources.Proof, error)
}

// NewResourceServer constructs the logical resource boundary without enabling it
// in production. A nil authorizer permits service reads, but never proof/mutations.
func NewResourceServer(reader Reader, resolver ResourceResolver, username, password, authHeader string, commander Commander, authorizer Authorizer, check AgentCheck) (http.Handler, error) {
	if reader == nil || resolver == nil || username == "agent" || username == "" || password == "" || strings.Contains(username, ":") || (authHeader != "Authorization" && authHeader != "X-Infra-Authorization") {
		return nil, errors.New("invalid server configuration")
	}
	expected := sha256.Sum256([]byte(username + ":" + password))
	base := resourceOperations{reader: reader, resolver: resolver, commander: commander, authorizer: authorizer, principal: username}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		fail := func(status int, code string) {
			w.WriteHeader(status)
			_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]string{"code": code}})
		}
		send := func(data any) { _ = json.NewEncoder(w).Encode(map[string]any{"data": data}) }
		if r.URL.Path != "/mcp" && ((commandPath(r.URL.Path) && r.Method != "POST") || (!commandPath(r.URL.Path) && r.Method != "GET")) {
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
		decoded, err := base64.StdEncoding.Strict().DecodeString(strings.TrimPrefix(values[0], "Basic "))
		sum := sha256.Sum256(decoded)
		user, credential, basic := strings.Cut(string(decoded), ":")
		op := base
		if err == nil && basic && user == "agent" {
			if check == nil || !agentToken.MatchString(credential) {
				fail(401, "Unauthenticated")
				return
			}
			op.principal = "agent"
			op.credential = credential
			op.check = check
		} else if err != nil || subtle.ConstantTimeCompare(sum[:], expected[:]) != 1 {
			fail(401, "Unauthenticated")
			return
		}
		proofRepresentation := r.URL.Path == "/v1/deployments" && r.Header.Get("Accept") == "application/vnd.raptor.resolution-proof+json"
		if proofRepresentation && len(r.Header.Values("Accept")) != 1 {
			fail(400, "InvalidInput")
			return
		}
		if op.principal == "agent" && (r.URL.Path == "/v1/resource-resolution-proof" || proofRepresentation) {
			fail(403, "ScopeMismatch")
			return
		}
		if r.URL.Path == "/mcp" {
			if r.URL.RawQuery != "" {
				fail(400, "InvalidInput")
				return
			}
			raw, e := io.ReadAll(io.LimitReader(r.Body, 16385))
			if e != nil || len(raw) > 16384 {
				fail(400, "InvalidInput")
				return
			}
			r.Body = io.NopCloser(bytes.NewReader(raw))
			var rpc struct {
				Method string `json:"method"`
			}
			_ = json.Unmarshal(raw, &rpc)
			// Typed calls authorize only after their entire operation payload is validated.
			if op.principal == "agent" && rpc.Method != "tools/call" {
				identity, e := check(r.Context(), credential, "", map[string]string{})
				if e != nil || !identity.permitsResource("", map[string]string{}) {
					fail(401, "Unauthenticated")
					return
				}
				op.identity = &identity
			}
			newResourceMCPHandler(op).ServeHTTP(w, r)
			return
		}
		var data any
		var e error
		if commandPath(r.URL.Path) {
			if r.URL.RawQuery != "" {
				fail(400, "InvalidInput")
				return
			}
			raw, err := io.ReadAll(io.LimitReader(r.Body, 8193))
			if err != nil {
				fail(400, "InvalidInput")
				return
			}
			data, e = op.command(r.Context(), r.URL.Path, raw)
		} else {
			q, err := url.ParseQuery(r.URL.RawQuery)
			if err != nil || len(r.URL.RawQuery) > 4096 {
				fail(400, "InvalidInput")
				return
			}
			if proofRepresentation {
				if !resourceInput(q, []string{"envCode", "kind", "code"}, nil) || q.Get("kind") != "db-proxy" {
					fail(400, "InvalidInput")
					return
				}
				data, e = op.execute(r.Context(), "/v1/resource-resolution-proof", url.Values{"envCode": {q.Get("envCode")}, "proxyCode": {q.Get("code")}})
			} else {
				data, e = op.execute(r.Context(), r.URL.Path, q)
			}
		}
		if e != nil {
			oe := e.(*operationError)
			fail(oe.status, oe.code)
			return
		}
		send(data)
	}), nil
}
