package openapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/url"

	"github.com/jamesDeng/raptor-iap/components/raptor/internal/approvals"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func strictAgentObject(raw json.RawMessage) (map[string]json.RawMessage, error) {
	if len(raw) > 8192 {
		return nil, errors.New("InvalidInput")
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	token, err := d.Token()
	if err != nil || token != json.Delim('{') {
		return nil, errors.New("InvalidInput")
	}
	out := map[string]json.RawMessage{}
	for d.More() {
		token, err = d.Token()
		key, ok := token.(string)
		if err != nil || !ok {
			return nil, errors.New("InvalidInput")
		}
		if _, exists := out[key]; exists {
			return nil, errors.New("InvalidInput")
		}
		var v json.RawMessage
		if d.Decode(&v) != nil {
			return nil, errors.New("InvalidInput")
		}
		out[key] = v
	}
	if token, err = d.Token(); err != nil || token != json.Delim('}') {
		return nil, errors.New("InvalidInput")
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		return nil, errors.New("InvalidInput")
	}
	return out, nil
}
func agentToolSelectors(tool string, raw json.RawMessage) (map[string]string, error) {
	fields, err := strictAgentObject(raw)
	if err != nil {
		return nil, err
	}
	bad := func() (map[string]string, error) { return nil, errors.New("InvalidInput") }
	if tool == "approval_request" {
		if len(fields) != 6 {
			return bad()
		}
		out := map[string]string{}
		for _, key := range []string{"requestId", "actionId", "interface", "envCode"} {
			var v string
			if json.Unmarshal(fields[key], &v) != nil || v == "" {
				return bad()
			}
			out[key] = v
		}
		if out["interface"] != "ess.scale-in" {
			return bad()
		}
		target, e := strictAgentObject(fields["target"])
		if e != nil || len(target) != 2 {
			return bad()
		}
		for _, key := range []string{"proxyCode", "groupId"} {
			var v string
			if json.Unmarshal(target[key], &v) != nil || v == "" {
				return bad()
			}
			out[key] = v
		}
		params, e := strictAgentObject(fields["parameters"])
		if e != nil || len(params) != 1 {
			return bad()
		}
		var capacity *int
		if json.Unmarshal(params["desiredCapacity"], &capacity) != nil || capacity == nil || *capacity < 0 {
			return bad()
		}
		delete(out, "actionId")
		delete(out, "interface")
		return out, nil
	}
	out := map[string]string{}
	for key, value := range fields {
		var v string
		if json.Unmarshal(value, &v) != nil {
			return bad()
		}
		out[key] = v
	}
	if tool == "request_pause" {
		if len(out) != 2 || (out["reason"] != "waiting_approval" && out["reason"] != "waiting_review") {
			return bad()
		}
		delete(out, "reason")
	}
	return out, nil
}

func addReplacementAgentTools(s *mcp.Server, p agentPrincipal) {
	mcp.AddTool(s, &mcp.Tool{Name: "approval_request", Description: "Request human approval for one exact scale-in target and desired capacity"}, func(ctx context.Context, r *mcp.CallToolRequest, in approvals.ApprovalInput) (*mcp.CallToolResult, any, error) {
		raw, err := json.Marshal(in)
		if err != nil {
			return nil, nil, err
		}
		selectors, err := agentToolSelectors("approval_request", raw)
		if err != nil {
			return nil, nil, err
		}
		if _, err = p.check(ctx, "approval_request", selectors); err != nil {
			return nil, nil, err
		}
		v, err := p.backend.Call(ctx, "POST", requestPath(in.RequestID)+"/approvals", in)
		return nil, v, err
	})
	mcp.AddTool(s, &mcp.Tool{Name: "approval_get", Description: "Read the decision for an approval belonging to this request"}, func(ctx context.Context, r *mcp.CallToolRequest, in ApprovalArgs) (*mcp.CallToolResult, any, error) {
		if _, err := p.check(ctx, "approval_get", map[string]string{"requestId": in.RequestID, "approvalId": in.ApprovalID}); err != nil {
			return nil, nil, err
		}
		v, err := p.backend.Call(ctx, "GET", requestPath(in.RequestID)+"/approvals/"+url.PathEscape(in.ApprovalID), nil)
		return nil, v, err
	})
	mcp.AddTool(s, &mcp.Tool{Name: "request_pause", Description: "Pause this request for human approval or review"}, func(ctx context.Context, r *mcp.CallToolRequest, in PauseArgs) (*mcp.CallToolResult, any, error) {
		if in.Reason != "waiting_approval" && in.Reason != "waiting_review" {
			return nil, nil, errors.New("InvalidInput")
		}
		if _, err := p.check(ctx, "request_pause", map[string]string{"requestId": in.RequestID}); err != nil {
			return nil, nil, err
		}
		v, err := p.backend.Call(ctx, "POST", requestPath(in.RequestID)+"/pause", map[string]string{"reason": in.Reason})
		return nil, v, err
	})
}
