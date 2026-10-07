package api

import (
	"context"
	"encoding/json"
	"github.com/modelcontextprotocol/go-sdk/mcp"
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
			var args map[string]string
			if len(r.Params.Arguments) > 8192 || json.Unmarshal(r.Params.Arguments, &args) != nil {
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
	return mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, &mcp.StreamableHTTPOptions{Stateless: true, JSONResponse: true})
}
func mcpFailure(code string) *mcp.CallToolResult {
	return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: code}}}
}
