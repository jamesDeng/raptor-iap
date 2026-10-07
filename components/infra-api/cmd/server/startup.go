package main

import (
	"errors"
	"net/http"
	"raptor-iap/infra-api/internal/api"
	"raptor-iap/infra-api/internal/commands"
	"raptor-iap/infra-api/internal/domain"
)

// buildHandler leaves the established read-only startup unchanged by default.
// Restart mode uses service Basic Auth and provider resource identity checks;
// it intentionally does not introduce request-signed context for this POC.
func buildHandler(reader api.Reader, envs map[string]domain.Environment, username, password, header string, enableRestart bool) (http.Handler, error) {
	if !enableRestart {
		return api.New(reader, envs, username, password, header)
	}
	restarter, ok := reader.(commands.Restarter)
	if !ok {
		return nil, errors.New("restart provider unavailable")
	}
	runtime := commands.Runtime{Restarter: restarter}
	auth := api.NewServiceAuthorizer(username, "/v1/deployment-restart")
	return api.NewWithCommands(reader, envs, username, password, header, runtime, auth)
}
