package api

import (
	"context"
	"net/url"
	"raptor-iap/infra-api/internal/domain"
)

// QuestionScope is a server-verified binding, never accepted from tool inputs.
// Verify must check current request/attempt state and authenticated context on
// every call. Deployment identity is additionally enforced by the provider.
type QuestionScope struct {
	RequestID, AttemptID, EnvCode, AppCode string
	Active                                 bool
}
type QuestionAuthorizer struct {
	RequestID, AttemptID, EnvCode, AppCode string
	Verify                                 func(context.Context, string) (QuestionScope, error)
}

func (a QuestionAuthorizer) Authorize(ctx context.Context, principal, path string, args any) error {
	if a.Verify == nil || a.RequestID == "" || a.AttemptID == "" || a.EnvCode == "" || a.AppCode == "" {
		return domain.ErrScope
	}
	s, e := a.Verify(ctx, principal)
	if e != nil || !s.Active || s.RequestID != a.RequestID || s.AttemptID != a.AttemptID || s.EnvCode != a.EnvCode || s.AppCode != a.AppCode {
		return domain.ErrScope
	}
	q, ok := args.(url.Values)
	if !ok || q.Get("envCode") != s.EnvCode {
		return domain.ErrScope
	}
	switch path {
	case "/v1/cloud/identity":
		return nil
	case "/v1/deployments":
		if q.Get("kind") == "application" && q.Get("code") == s.AppCode {
			return nil
		}
	case "/v1/deployment-status":
		if q.Get("appCode") == s.AppCode {
			return nil
		}
	}
	return domain.ErrScope
}
