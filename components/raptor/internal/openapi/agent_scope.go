package openapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/agentaccess"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/domain"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/httpx"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"io"
	"net/http"
	"net/url"
	"time"
)

type agentPrincipal struct {
	backend, inspector Client
	credential         string
}

func (p agentPrincipal) check(ctx context.Context, tool string, selectors map[string]string) (*agentaccess.Binding, error) {
	if p.inspector.Username == "" || p.inspector.Password == "" {
		return nil, errors.New("Unavailable")
	}
	v, e := p.inspector.Call(ctx, "POST", "/v1/agent-access/introspect", agentaccess.CheckInput{Credential: p.credential, Audience: "raptor", Tool: tool, Selectors: selectors})
	if e != nil {
		return nil, errors.New("Unavailable")
	}
	raw, e := json.Marshal(v["data"])
	if e != nil {
		return nil, errors.New("Unavailable")
	}
	var out agentaccess.CheckResult
	if json.Unmarshal(raw, &out) != nil {
		return nil, errors.New("Unavailable")
	}
	if !out.Active || out.Binding == nil || out.ExpiresAt == nil || !out.ExpiresAt.After(time.Now()) || !agentaccess.SelectorsAllowed(*out.Binding, "raptor", tool, selectors) {
		return nil, errors.New("Forbidden")
	}
	return out.Binding, nil
}
func agentHandler(backend, inspector Client, credential string) http.Handler {
	principal := agentPrincipal{backend: backend, inspector: inspector, credential: credential}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/mcp" {
			httpx.Error(w, 403, "Forbidden")
			return
		}
		if r.Method != "POST" {
			httpx.Error(w, 405, "MethodNotAllowed")
			return
		}
		body, e := io.ReadAll(http.MaxBytesReader(w, r.Body, 32768))
		if e != nil {
			httpx.Error(w, 400, "InvalidInput")
			return
		}
		r.Body.Close()
		var envelope struct {
			JSONRPC string          `json:"jsonrpc"`
			Method  string          `json:"method"`
			Params  json.RawMessage `json:"params"`
			ID      json.RawMessage `json:"id"`
		}
		if json.Unmarshal(body, &envelope) != nil || envelope.JSONRPC != "2.0" {
			httpx.Error(w, 400, "InvalidInput")
			return
		}
		tool := ""
		var selectors map[string]string
		switch envelope.Method {
		case "initialize", "notifications/initialized", "ping", "tools/list":
		case "tools/call":
			var params struct {
				Name      string            `json:"name"`
				Arguments map[string]string `json:"arguments"`
			}
			if json.Unmarshal(envelope.Params, &params) != nil || params.Name == "" {
				httpx.Error(w, 400, "InvalidInput")
				return
			}
			tool = params.Name
			selectors = params.Arguments
		default:
			httpx.Error(w, 403, "Forbidden")
			return
		}
		if _, e = principal.check(r.Context(), tool, selectors); e != nil {
			if e.Error() == "Forbidden" {
				httpx.Error(w, 403, "Forbidden")
			} else {
				httpx.Error(w, 503, "Unavailable")
			}
			return
		}
		r.Body = io.NopCloser(bytes.NewReader(body))
		server := newAgentMCP(principal)
		mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, &mcp.StreamableHTTPOptions{Stateless: true}).ServeHTTP(w, r)
	})
}
func newAgentMCP(p agentPrincipal) *mcp.Server {
	s := mcp.NewServer(&mcp.Implementation{Name: "raptor-agent-read-only", Version: "0.1.0"}, nil)
	mcp.AddTool(s, &mcp.Tool{Name: "request_get", Description: "Read canonical context for the credential-bound request. Catalog data is not deployment health."}, func(ctx context.Context, r *mcp.CallToolRequest, in RequestArgs) (*mcp.CallToolResult, any, error) {
		b, e := p.check(ctx, "request_get", map[string]string{"requestId": in.RequestID})
		if e != nil {
			return nil, nil, e
		}
		v, e := p.backend.Call(ctx, "GET", requestPath(b.RequestID)+"/context", nil)
		if e != nil {
			return nil, nil, e
		}
		data, ok := v["data"].(map[string]any)
		if !ok || data["requestId"] != b.RequestID {
			return nil, nil, errors.New("Unavailable")
		}
		raw, _ := json.Marshal(data["definition"])
		var def domain.RequestInput
		if json.Unmarshal(raw, &def) != nil {
			return nil, nil, errors.New("Unavailable")
		}
		fingerprint, e := agentaccess.Fingerprint(def)
		if e != nil || fingerprint != b.DefinitionSHA256 {
			return nil, nil, errors.New("Unavailable")
		}
		env, ok := data["environment"].(map[string]any)
		if !ok || env["code"] != b.EnvCode {
			return nil, nil, errors.New("Unavailable")
		}
		out := map[string]any{"definition": def, "definitionSha256": fingerprint, "environment": publicEnvironment(env), "skills": def.Skills, "model": def.Model}
		stamp(out, b)
		return nil, map[string]any{"data": out}, nil
	})
	mcp.AddTool(s, &mcp.Tool{Name: "environment_get", Description: "Read nonsecret configuration for the credential-bound environment."}, func(ctx context.Context, r *mcp.CallToolRequest, in EnvironmentArgs) (*mcp.CallToolResult, any, error) {
		b, e := p.check(ctx, "environment_get", map[string]string{"requestId": in.RequestID, "envCode": in.EnvCode})
		if e != nil {
			return nil, nil, e
		}
		v, e := p.backend.Call(ctx, "GET", "/v1/environments/"+url.PathEscape(b.EnvCode), nil)
		if e != nil {
			return nil, nil, e
		}
		env, ok := v["data"].(map[string]any)
		if !ok || env["code"] != b.EnvCode {
			return nil, nil, errors.New("Unavailable")
		}
		out := publicEnvironment(env)
		stamp(out, b)
		return nil, map[string]any{"data": out}, nil
	})
	mcp.AddTool(s, &mcp.Tool{Name: "object_get", Description: "Read the primary application catalog object bound to this credential."}, func(ctx context.Context, r *mcp.CallToolRequest, in ObjectArgs) (*mcp.CallToolResult, any, error) {
		b, e := p.check(ctx, "object_get", map[string]string{"requestId": in.RequestID, "kind": in.Kind, "code": in.Code})
		if e != nil {
			return nil, nil, e
		}
		v, e := p.backend.Call(ctx, "GET", "/v1/objects/"+url.PathEscape(b.ObjectKind)+"/"+url.PathEscape(b.ObjectCode), nil)
		if e != nil {
			return nil, nil, e
		}
		obj, ok := v["data"].(map[string]any)
		if !ok || obj["code"] != b.ObjectCode || obj["kind"] != b.ObjectKind {
			return nil, nil, errors.New("Unavailable")
		}
		out := map[string]any{}
		for _, key := range []string{"id", "kind", "code", "name", "description"} {
			if value, ok := obj[key].(string); ok {
				out[key] = value
			}
		}
		stamp(out, b)
		return nil, map[string]any{"data": out}, nil
	})
	return s
}
func publicEnvironment(env map[string]any) map[string]any {
	out := map[string]any{}
	for _, key := range []string{"code", "groupCode", "stage"} {
		if v, ok := env[key].(string); ok {
			out[key] = v
		}
	}
	config := map[string]string{}
	if raw, ok := env["config"].(map[string]any); ok {
		for _, key := range []string{"ackClusterId", "region", "namespace"} {
			if v, ok := raw[key].(string); ok {
				config[key] = v
			}
		}
	}
	out["config"] = config
	return out
}
func stamp(out map[string]any, b *agentaccess.Binding) {
	out["requestId"] = b.RequestID
	out["attemptId"] = b.AttemptID
	out["envCode"] = b.EnvCode
	out["objectCode"] = b.ObjectCode
	out["observedAt"] = time.Now().UTC()
	out["evidenceMode"] = "live"
	out["dataKind"] = "catalog"
}
