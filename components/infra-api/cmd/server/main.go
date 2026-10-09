package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"raptor-iap/infra-api/internal/api"
	"raptor-iap/infra-api/internal/domain"
	"raptor-iap/infra-api/internal/provider"
	"raptor-iap/infra-api/internal/scope"
	"time"
)

func main() {
	envs, e := scope.Load(os.Getenv("INFRA_SCOPE_FILE"))
	if e != nil {
		log.Fatal("scope configuration unavailable")
	}
	header := os.Getenv("INFRA_AUTH_HEADER")
	if header == "" {
		header = "Authorization"
	}
	reader, e := provider.New()
	if e != nil {
		log.Fatal("credential provider unavailable")
	}
	opts := runtimeOptions{Restart: os.Getenv("INFRA_ENABLE_RESTART") == "true", Proxy: os.Getenv("INFRA_ENABLE_PROXY_COMMANDS") == "true"}
	deps, e := liveDependencies(envs, opts, os.Getenv("RAPTOR_APPROVAL_ORIGIN"), os.Getenv("RAPTOR_SERVICE_USERNAME"), os.Getenv("RAPTOR_SERVICE_PASSWORD"))
	if e != nil {
		log.Fatal("proxy runtime configuration unavailable")
	}
	h, e := buildRuntimeHandler(reader, envs, os.Getenv("INFRA_USERNAME"), os.Getenv("INFRA_PASSWORD"), header, opts, deps)
	if e != nil {
		log.Fatal("authentication configuration unavailable")
	}
	addr := os.Getenv("INFRA_LISTEN")
	if addr == "" {
		addr = "127.0.0.1:8875"
	}
	s := http.Server{Addr: addr, Handler: h, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 8192}
	if s.ListenAndServe() != http.ErrServerClosed {
		log.Fatal("HTTP server stopped")
	}
}

// Restart is opt-in; proxy commands remain unavailable on this service.
type restarter interface {
	Restart(context.Context, domain.Environment, domain.RestartCommand) (domain.RestartReceipt, error)
}
type restartOnly struct{ restarter }

func (restartOnly) Scale(context.Context, domain.Environment, domain.ScaleCommand) (domain.ScaleReceipt, error) {
	return domain.ScaleReceipt{}, domain.ErrScope
}
func (restartOnly) SetProtection(context.Context, domain.Environment, domain.ProtectionCommand) (domain.NodeReceipt, error) {
	return domain.NodeReceipt{}, domain.ErrScope
}
func (restartOnly) Deregister(context.Context, domain.Environment, domain.DeregisterCommand) (domain.NodeReceipt, error) {
	return domain.NodeReceipt{}, domain.ErrScope
}
func buildHandler(reader api.Reader, envs map[string]domain.Environment, username, password, header string, enabled bool) (http.Handler, error) {
	if !enabled {
		return api.New(reader, envs, username, password, header)
	}
	r, ok := reader.(restarter)
	if !ok {
		return nil, domain.ErrNotConfigured
	}
	return api.NewWithCommands(reader, envs, username, password, header, restartOnly{r}, api.NewServiceAuthorizer(username, "/v1/deployment-restart"))
}
