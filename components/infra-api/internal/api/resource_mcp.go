package api

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"io"
	"net/http"
	"net/url"
	"time"
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
		field, options := resourceProxyReadOptions(spec.name)
		if field != "" {
			props[field].(map[string]any)["enum"] = options
		}
		server.AddTool(&mcp.Tool{Name: spec.name, Description: "Read a logical catalog resource workload", InputSchema: map[string]any{"type": "object", "properties": props, "required": spec.fields, "additionalProperties": false}}, func(ctx context.Context, r *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			args, e := decodeSelectors(r.Params.Arguments)
			if e != nil {
				return mcpFailure("InvalidInput"), nil
			}
			if spec.name == "proxy_pgcat_metrics_get" && args["series"] == "clients" {
				// The SDK detaches legacy-protocol handler deadlines. Preserve the
				// original HTTP budget as well as explicit SDK cancellation.
				parent, ok := ctx.Value(clientsHTTPContextKey{}).(context.Context)
				if !ok || parent.Err() != nil || ctx.Err() != nil {
					return mcpFailure("ProviderUnavailable"), nil
				}
				readContext, cancel := context.WithCancel(parent)
				stop := context.AfterFunc(ctx, cancel)
				defer stop()
				defer cancel()
				ctx = readContext
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
	handler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, &mcp.StreamableHTTPOptions{Stateless: true, JSONResponse: true})
	return boundClientsMCPResponse(handler)
}

const clientsMCPResponseLimit = 262144

type clientsHTTPContextKey struct{}

// Buffer only the additive complete read. Count the actual SDK-encoded reply,
// including its RPC id, escaping and structured/text duplication, before any
// header or result byte is exported. Other tools keep their existing transport.
func boundClientsMCPResponse(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.Body == nil {
			next.ServeHTTP(w, r)
			return
		}
		raw, err := io.ReadAll(io.LimitReader(r.Body, 16385))
		r.Body = io.NopCloser(bytes.NewReader(raw))
		var rpc struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
			Params struct {
				Name      string `json:"name"`
				Arguments struct {
					Series string `json:"series"`
				} `json:"arguments"`
			} `json:"params"`
		}
		if err != nil || len(raw) > 16384 || json.Unmarshal(raw, &rpc) != nil || rpc.Method != "tools/call" || rpc.Params.Name != "proxy_pgcat_metrics_get" || rpc.Params.Arguments.Series != "clients" {
			next.ServeHTTP(w, r)
			return
		}
		buffer := &clientsMCPResponse{header: w.Header().Clone()}
		// Keep one overall budget even when the incoming server request has no
		// deadline. A shorter caller deadline/cancellation always wins.
		callerContext := r.Context()
		requestContext, cancel := context.WithTimeout(callerContext, 20*time.Second)
		defer cancel()
		r = r.WithContext(context.WithValue(requestContext, clientsHTTPContextKey{}, requestContext))
		next.ServeHTTP(buffer, r)
		// Check the caller directly in case cancellation has not reached the child.
		if callerContext.Err() != nil || requestContext.Err() != nil {
			return
		}
		body := buffer.body.Bytes()
		if buffer.overflow {
			body, _ = json.Marshal(map[string]any{"jsonrpc": "2.0", "id": rpc.ID, "result": mcpFailure("ProviderUnavailable")})
			buffer.status = http.StatusOK
		}
		for key := range w.Header() {
			w.Header().Del(key)
		}
		for key, values := range buffer.header {
			w.Header()[key] = values
		}
		w.Header().Del("Content-Length")
		if buffer.status != 0 {
			w.WriteHeader(buffer.status)
		}
		_, _ = w.Write(body)
	})
}

type clientsMCPResponse struct {
	header   http.Header
	status   int
	body     bytes.Buffer
	overflow bool
}

func (w *clientsMCPResponse) Header() http.Header { return w.header }
func (w *clientsMCPResponse) WriteHeader(status int) {
	if w.status == 0 {
		w.status = status
	}
}
func (w *clientsMCPResponse) Write(data []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	if !w.overflow && len(data) <= clientsMCPResponseLimit-w.body.Len() {
		_, _ = w.body.Write(data)
	} else {
		w.overflow = true
	}
	return len(data), nil
}
