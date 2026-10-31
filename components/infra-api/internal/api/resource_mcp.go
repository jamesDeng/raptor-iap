package api

import (
	"context"
	"encoding/json"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"net/http"
	"net/url"
)

func resourceMCPResult(data any, e error) (*mcp.CallToolResult, error) {
	if e != nil {
		return mcpFailure(e.Error()), nil
	}
	envelope := map[string]any{"data": data}
	raw, err := json.Marshal(envelope)
	if err != nil {
		return mcpFailure("ProviderUnavailable"), nil
	}
	return &mcp.CallToolResult{StructuredContent: envelope, Content: []mcp.Content{&mcp.TextContent{Text: string(raw)}}}, nil
}
func newResourceMCPHandler(op resourceOperations) http.Handler {
	server := mcp.NewServer(&mcp.Implementation{Name: "infra-api", Version: "0.2.0"}, nil)
	for _, spec := range []struct {
		name, path       string
		fields, optional []string
	}{{"deployments_list", "/v1/deployments", []string{"envCode", "kind", "code"}, nil}, {"deployment_status_get", "/v1/deployment-status", []string{"envCode", "appCode"}, []string{"workloadKey"}}, {"proxy_observation_get", "", []string{"requestId", "envCode", "proxyCode", "kind"}, nil}, {"proxy_pgcat_metrics_get", "", []string{"requestId", "envCode", "proxyCode", "series"}, nil}} {
		props := map[string]any{}
		for _, k := range append(append([]string{}, spec.fields...), spec.optional...) {
			props[k] = map[string]any{"type": "string", "minLength": 1, "maxLength": 256}
		}
		field, options := proxyReadOptions(spec.name)
		if field != "" {
			props[field].(map[string]any)["enum"] = options
		}
		server.AddTool(&mcp.Tool{Name: spec.name, Description: "Read a logical catalog resource workload", InputSchema: map[string]any{"type": "object", "properties": props, "required": spec.fields, "additionalProperties": false}}, func(ctx context.Context, r *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			args, e := decodeSelectors(r.Params.Arguments)
			if e != nil {
				return mcpFailure("InvalidInput"), nil
			}
			if spec.path == "" {
				data, e := op.proxyRead(ctx, spec.name, args)
				return resourceMCPResult(data, e)
			}
			q := url.Values{}
			for k, v := range args {
				q[k] = []string{v}
			}
			data, e := op.execute(ctx, spec.path, q)
			return resourceMCPResult(data, e)
		})
	}
	if op.commander != nil && op.authorizer != nil {
		for _, spec := range resourceCommandSpecs {
			if !op.visible(spec.path) {
				continue
			}
			if op.identity != nil && (op.identity.Binding == nil || op.identity.Binding.Operation != "db-proxy.replace-nodes" || op.identity.Binding.ExecutionMode == "qualification") {
				continue
			}
			server.AddTool(&mcp.Tool{Name: spec.name, Description: "Execute a request-authorized logical resource command", InputSchema: commandSchema(spec.sample)}, func(ctx context.Context, r *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
				data, e := op.command(ctx, spec.path, r.Params.Arguments)
				return resourceMCPResult(data, e)
			})
		}
	}
	return mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, &mcp.StreamableHTTPOptions{Stateless: true, JSONResponse: true})
}
