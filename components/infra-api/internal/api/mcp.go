package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"io"
	"net/http"
	"net/url"
)

// MCP is a transport over the same read dispatcher as the HTTP endpoints.
// Raw handlers validate JSON explicitly; SDK tool visibility is not authorization.
func newMCPHandler(op operations) http.Handler {
	server := mcp.NewServer(&mcp.Implementation{Name: "infra-api", Version: "0.1.0"}, nil)
	for _, spec := range []struct {
		name, path, description string
		fields                  []string
	}{
		{"cloud_identity_get", "/v1/cloud/identity", "Check the configured cloud account for an environment", []string{"envCode"}},
		{"deployments_list", "/v1/deployments", "Discover deployments by environment, catalog kind and code", []string{"envCode", "kind", "code"}},
		{"deployment_status_get", "/v1/deployment-status", "Read application deployment status bound to cluster, namespace, name and UID", []string{"envCode", "appCode", "clusterId", "namespace", "name", "uid"}},
	} {
		props := map[string]any{}
		for _, field := range spec.fields {
			props[field] = map[string]any{"type": "string", "minLength": 1, "maxLength": 256}
		}
		server.AddTool(&mcp.Tool{Name: spec.name, Description: spec.description, InputSchema: map[string]any{"type": "object", "properties": props, "required": spec.fields, "additionalProperties": false}}, func(ctx context.Context, r *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			args, decodeErr := decodeSelectors(r.Params.Arguments)
			if decodeErr != nil {
				return mcpFailure("InvalidInput"), nil
			}
			q := url.Values{}
			for k, v := range args {
				q.Set(k, v)
			}
			data, e := op.execute(ctx, spec.path, q)
			if e != nil {
				return mcpFailure(e.Error()), nil
			}
			envelope := map[string]any{"data": data}
			b, e := json.Marshal(envelope)
			if e != nil {
				return mcpFailure("ProviderUnavailable"), nil
			}
			return &mcp.CallToolResult{StructuredContent: envelope, Content: []mcp.Content{&mcp.TextContent{Text: string(b)}}}, nil
		})
	}
	// Only explicitly configured command servers expose mutation tools.
	visibility, canListCommands := op.authorizer.(CommandToolVisibility)
	if op.commander != nil && canListCommands && visibility.ExposeCommands() {
		for _, spec := range commandSpecs {
			server.AddTool(&mcp.Tool{Name: spec.name, Description: "Execute one request-authorized infrastructure command", InputSchema: commandSchema(spec.sample)}, func(ctx context.Context, r *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
				data, e := op.command(ctx, spec.path, r.Params.Arguments)
				if e != nil {
					return mcpFailure(e.Error()), nil
				}
				envelope := map[string]any{"data": data}
				b, e := json.Marshal(envelope)
				if e != nil {
					return mcpFailure("ProviderUnavailable"), nil
				}
				return &mcp.CallToolResult{StructuredContent: envelope, Content: []mcp.Content{&mcp.TextContent{Text: string(b)}}}, nil
			})
		}
	}
	return mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, &mcp.StreamableHTTPOptions{Stateless: true, JSONResponse: true})
}
func mcpFailure(code string) *mcp.CallToolResult {
	return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: code}}}
}

// Decode incrementally: encoding/json's map decoder silently accepts duplicates.
func decodeSelectors(raw json.RawMessage) (map[string]string, error) {
	if len(raw) > 8192 {
		return nil, errors.New("InvalidInput")
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	token, e := d.Token()
	if e != nil || token != json.Delim('{') {
		return nil, errors.New("InvalidInput")
	}
	args := map[string]string{}
	for d.More() {
		token, e = d.Token()
		if e != nil {
			return nil, e
		}
		key, ok := token.(string)
		if !ok {
			return nil, errors.New("InvalidInput")
		}
		if _, exists := args[key]; exists {
			return nil, errors.New("InvalidInput")
		}
		var value any
		if e = d.Decode(&value); e != nil {
			return nil, e
		}
		text, ok := value.(string)
		if !ok {
			return nil, errors.New("InvalidInput")
		}
		args[key] = text
	}
	if _, e = d.Token(); e != nil {
		return nil, e
	}
	var trailing any
	if e = d.Decode(&trailing); e != io.EOF {
		return nil, errors.New("InvalidInput")
	}
	return args, nil
}
