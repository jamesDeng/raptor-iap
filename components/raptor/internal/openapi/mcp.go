package openapi

import (
	"context"
	"errors"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/approvals"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"net/url"
)

type RequestArgs struct {
	RequestID string `json:"requestId"`
}
type EnvironmentArgs struct {
	RequestID string `json:"requestId"`
	EnvCode   string `json:"envCode"`
}
type ObjectArgs struct {
	RequestID string `json:"requestId"`
	Kind      string `json:"kind"`
	Code      string `json:"code"`
}
type ApprovalArgs struct {
	RequestID  string `json:"requestId"`
	ApprovalID string `json:"approvalId"`
}
type PauseArgs struct {
	RequestID string `json:"requestId"`
	Reason    string `json:"reason"`
}

func requestPath(id string) string { return "/v1/requests/" + url.PathEscape(id) }
func definition(ctx context.Context, c Client, id string) (map[string]any, error) {
	v, e := c.Call(ctx, "GET", requestPath(id)+"/context", nil)
	if e != nil {
		return nil, e
	}
	d, ok := v["data"].(map[string]any)
	if !ok {
		return nil, errors.New("Unavailable")
	}
	def, ok := d["definition"].(map[string]any)
	if !ok {
		return nil, errors.New("Unavailable")
	}
	return def, nil
}
func NewMCP(c Client) *mcp.Server {
	s := mcp.NewServer(&mcp.Implementation{Name: "raptor-open-api", Version: "0.1.0"}, nil)
	mcp.AddTool(s, &mcp.Tool{Name: "request_pause", Description: "Pause a request while awaiting human approval or PR review"}, func(ctx context.Context, r *mcp.CallToolRequest, in PauseArgs) (*mcp.CallToolResult, any, error) {
		v, e := c.Call(ctx, "POST", requestPath(in.RequestID)+"/pause", map[string]string{"reason": in.Reason})
		return nil, v, e
	})
	mcp.AddTool(s, &mcp.Tool{Name: "request_get", Description: "Read request context and live environment configuration"}, func(ctx context.Context, r *mcp.CallToolRequest, in RequestArgs) (*mcp.CallToolResult, any, error) {
		v, e := c.Call(ctx, "GET", requestPath(in.RequestID)+"/context", nil)
		return nil, v, e
	})
	mcp.AddTool(s, &mcp.Tool{Name: "environment_get", Description: "Read the environment selected by this request"}, func(ctx context.Context, r *mcp.CallToolRequest, in EnvironmentArgs) (*mcp.CallToolResult, any, error) {
		d, e := definition(ctx, c, in.RequestID)
		if e != nil {
			return nil, nil, e
		}
		if in.EnvCode != d["envCode"] {
			return nil, nil, errors.New("environment does not match request")
		}
		v, e := c.Call(ctx, "GET", "/v1/environments/"+url.PathEscape(in.EnvCode), nil)
		return nil, v, e
	})
	mcp.AddTool(s, &mcp.Tool{Name: "object_get", Description: "Read the primary catalog object for a request"}, func(ctx context.Context, r *mcp.CallToolRequest, in ObjectArgs) (*mcp.CallToolResult, any, error) {
		d, e := definition(ctx, c, in.RequestID)
		if e != nil {
			return nil, nil, e
		}
		o, ok := d["object"].(map[string]any)
		if !ok || in.Kind != o["kind"] || in.Code != o["code"] {
			return nil, nil, errors.New("object does not match request")
		}
		v, e := c.Call(ctx, "GET", "/v1/objects/"+url.PathEscape(in.Kind)+"/"+url.PathEscape(in.Code), nil)
		return nil, v, e
	})
	mcp.AddTool(s, &mcp.Tool{Name: "deployments_list", Description: "Discover deployments for the request object and environment"}, func(ctx context.Context, r *mcp.CallToolRequest, in EnvironmentArgs) (*mcp.CallToolResult, any, error) {
		d, e := definition(ctx, c, in.RequestID)
		if e != nil {
			return nil, nil, e
		}
		if in.EnvCode != d["envCode"] {
			return nil, nil, errors.New("environment does not match request")
		}
		v, e := c.Call(ctx, "GET", requestPath(in.RequestID)+"/deployments?env="+url.QueryEscape(in.EnvCode), nil)
		return nil, v, e
	})
	mcp.AddTool(s, &mcp.Tool{Name: "approval_request", Description: "Request human approval bound to one action, target and parameters"}, func(ctx context.Context, r *mcp.CallToolRequest, in approvals.ApprovalInput) (*mcp.CallToolResult, any, error) {
		d, e := definition(ctx, c, in.RequestID)
		if e != nil {
			return nil, nil, e
		}
		if in.EnvCode != d["envCode"] {
			return nil, nil, errors.New("environment does not match request")
		}
		v, e := c.Call(ctx, "POST", requestPath(in.RequestID)+"/approvals", in)
		return nil, v, e
	})
	mcp.AddTool(s, &mcp.Tool{Name: "approval_get", Description: "Read a human approval decision"}, func(ctx context.Context, r *mcp.CallToolRequest, in ApprovalArgs) (*mcp.CallToolResult, any, error) {
		v, e := c.Call(ctx, "GET", requestPath(in.RequestID)+"/approvals/"+url.PathEscape(in.ApprovalID), nil)
		return nil, v, e
	})
	return s
}
