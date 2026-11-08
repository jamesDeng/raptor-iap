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
	if tool == "pull_request_attach" {
		if len(fields) != 9 {
			return bad()
		}
		for _, key := range []string{"requestId", "attemptId", "repository", "url", "headSha", "baseCommit", "baseBranch", "workingBranch"} {
			var value string
			if json.Unmarshal(fields[key], &value) != nil || value == "" {
				return bad()
			}
		}
		var number int
		if json.Unmarshal(fields["number"], &number) != nil || number < 1 {
			return bad()
		}
		var requestID, attemptID string
		json.Unmarshal(fields["requestId"], &requestID)
		json.Unmarshal(fields["attemptId"], &attemptID)
		return map[string]string{"requestId": requestID, "attemptId": attemptID}, nil
	}
	if tool == "request_grant_request" {
		if len(fields) != 4 {
			return bad()
		}
		var capabilities []json.RawMessage
		if json.Unmarshal(fields["capabilities"], &capabilities) != nil || len(capabilities) < 1 || len(capabilities) > 16 {
			return bad()
		}
		selectors := map[string]string{}
		for _, key := range []string{"requestId", "envCode", "scopeDigest"} {
			var value string
			if json.Unmarshal(fields[key], &value) != nil || value == "" {
				return bad()
			}
			selectors[key] = value
		}
		return selectors, nil
	}
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
		if out["interface"] != "db-proxy.scale-in" && out["interface"] != "runtime.qualification-resume" {
			return bad()
		}
		target, e := strictAgentObject(fields["target"])
		if e != nil || len(target) != 1 {
			return bad()
		}
		for _, key := range []string{"proxyCode"} {
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
		if out["interface"] == "runtime.qualification-resume" {
			var phase string
			if json.Unmarshal(params["phase"], &phase) != nil || phase != "resume" {
				return bad()
			}
		} else {
			var capacity *int
			if json.Unmarshal(params["desiredCapacity"], &capacity) != nil || capacity == nil || *capacity < 0 {
				return bad()
			}
			delete(out, "interface")
		}
		delete(out, "actionId")
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
	mcp.AddTool(s, &mcp.Tool{Name: "approval_request", Description: "Request one exact scale-in approval, or a qualification-only continuation approval"}, func(ctx context.Context, r *mcp.CallToolRequest, in approvals.ApprovalInput) (*mcp.CallToolResult, any, error) {
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

type grantGetArgs struct {
	RequestID string `json:"requestId"`
	GrantID   string `json:"grantId"`
}

type grantActionGetArgs struct {
	RequestID string `json:"requestId"`
	ActionID  string `json:"actionId"`
}

func addRequestGrantAgentTools(s *mcp.Server, p agentPrincipal) {
	const schema = `{"type":"object","required":["requestId","envCode","scopeDigest","capabilities"],"additionalProperties":false,
	"properties":{"requestId":{"type":"string"},"envCode":{"type":"string"},"scopeDigest":{"type":"string"},
	"capabilities":{"type":"array","minItems":1,"maxItems":16,"items":{"type":"object","additionalProperties":false,
	"required":["capability","target","constraints"],"properties":{"capability":{"type":"string"},
	"target":{"type":"object","additionalProperties":false,"required":["kind","code"],"properties":{"kind":{"type":"string"},"code":{"type":"string"}}},
	"constraints":{"type":"object"}}}}}}`
	mcp.AddTool(s, &mcp.Tool{Name: "request_grant_request", Description: "Submit one bundle of human approvals bound to this request and its resolved objects. The runtime checkpoints and waits for the decision.", InputSchema: json.RawMessage(schema)},
		func(ctx context.Context, _ *mcp.CallToolRequest, in approvals.RequestGrantInput) (*mcp.CallToolResult, any, error) {
			selectors := map[string]string{"requestId": in.RequestID, "envCode": in.EnvCode, "scopeDigest": in.ScopeDigest}
			if _, err := p.check(ctx, "request_grant_request", selectors); err != nil {
				return nil, nil, err
			}
			v, err := p.backend.Call(ctx, "POST", requestPath(in.RequestID)+"/grants", in)
			return nil, v, err
		})
	mcp.AddTool(s, &mcp.Tool{Name: "request_grant_get", Description: "Read the authoritative current decision and exact capabilities of a bundle belonging to this request."},
		func(ctx context.Context, _ *mcp.CallToolRequest, in grantGetArgs) (*mcp.CallToolResult, any, error) {
			if _, err := p.check(ctx, "request_grant_get", map[string]string{"requestId": in.RequestID, "grantId": in.GrantID}); err != nil {
				return nil, nil, err
			}
			v, err := p.backend.Call(ctx, "GET", requestPath(in.RequestID)+"/grants/"+url.PathEscape(in.GrantID), nil)
			return nil, v, err
		})
	mcp.AddTool(s, &mcp.Tool{Name: "request_grant_actions_list", Description: "Read saved action receipts for the resolved Request. Match exact command arguments to reconcile a lost response."},
		func(ctx context.Context, _ *mcp.CallToolRequest, in struct {
			RequestID string `json:"requestId"`
		}) (*mcp.CallToolResult, any, error) {
			checked, err := p.checkResult(ctx, "request_grant_actions_list", map[string]string{"requestId": in.RequestID})
			if err != nil {
				return nil, nil, err
			}
			query := url.Values{"scopeDigest": {checked.ReplacementScope.ResolutionDigest}}
			v, err := p.backend.Call(ctx, "GET", requestPath(in.RequestID)+"/grant-actions?"+query.Encode(), nil)
			return nil, v, err
		})
	mcp.AddTool(s, &mcp.Tool{Name: "request_grant_action_get", Description: "Read the saved exact action receipt for this request. Use this read to reconcile a lost response, never to replay a write."},
		func(ctx context.Context, _ *mcp.CallToolRequest, in grantActionGetArgs) (*mcp.CallToolResult, any, error) {
			if _, err := p.check(ctx, "request_grant_action_get", map[string]string{"requestId": in.RequestID, "actionId": in.ActionID}); err != nil {
				return nil, nil, err
			}
			v, err := p.backend.Call(ctx, "GET", requestPath(in.RequestID)+"/grant-actions/"+url.PathEscape(in.ActionID), nil)
			return nil, v, err
		})
}
