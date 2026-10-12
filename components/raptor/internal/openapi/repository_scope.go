package openapi

import (
	"context"
	"errors"
	githubservice "github.com/jamesDeng/raptor-iap/components/raptor/internal/github"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type BoundPRArgs struct {
	RequestID string `json:"requestId"`
	githubservice.BoundPRInput
}

func addRepositoryAgentTools(s *mcp.Server, p agentPrincipal) {
	mcp.AddTool(s, &mcp.Tool{Name: "pull_request_attach", Description: "Attach the created PR to this exact repository change attempt"}, func(ctx context.Context, r *mcp.CallToolRequest, in BoundPRArgs) (*mcp.CallToolResult, any, error) {
		b, err := p.check(ctx, "pull_request_attach", map[string]string{"requestId": in.RequestID, "attemptId": in.AttemptID})
		if err != nil {
			return nil, nil, err
		}
		if b.RequestID != in.RequestID || b.AttemptID != in.AttemptID {
			return nil, nil, errors.New("Forbidden")
		}
		v, err := p.backend.Call(ctx, "POST", requestPath(b.RequestID)+"/bound-pull-requests", in.BoundPRInput)
		return nil, v, err
	})
}
