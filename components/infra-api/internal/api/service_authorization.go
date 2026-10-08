package api

import (
	"context"
	"raptor-iap/infra-api/internal/domain"
)

// ServiceAuthorizer implements the owner's Basic Auth-only POC policy.
// It permits configured operations for the authenticated service principal.
// It does not enforce request/attempt/object-specific access.
type ServiceAuthorizer struct {
	principal string
	commands  map[string]bool
}

func NewServiceAuthorizer(principal string, paths ...string) ServiceAuthorizer {
	commands := map[string]bool{}
	for _, path := range paths {
		commands[path] = true
	}
	return ServiceAuthorizer{principal: principal, commands: commands}
}
func (a ServiceAuthorizer) Authorize(_ context.Context, principal, path string, _ any) error {
	if a.principal == "" || principal != a.principal {
		return domain.ErrScope
	}
	switch path {
	case "/v1/cloud/identity", "/v1/deployments", "/v1/deployment-status":
		return nil
	}
	if a.commands[path] {
		return nil
	}
	return domain.ErrScope
}
func (a ServiceAuthorizer) ExposeCommands() bool            { return len(a.commands) > 0 }
func (a ServiceAuthorizer) VisibleCommand(path string) bool { return a.commands[path] }
