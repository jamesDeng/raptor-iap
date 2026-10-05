package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"raptor-iap/infra-api/internal/api"
	"raptor-iap/infra-api/internal/domain"
	"raptor-iap/infra-api/internal/scope"
	"time"
)

// Reads remain unavailable until the provider adapter is installed.
type unavailable struct{}

func (unavailable) Identity(context.Context, domain.Environment) (string, error) {
	return "", domain.ErrNotConfigured
}
func (unavailable) Deployments(context.Context, domain.Environment, string, string) ([]domain.Deployment, error) {
	return nil, domain.ErrNotConfigured
}
func (unavailable) Status(context.Context, domain.Environment, domain.Target) (domain.Status, error) {
	return domain.Status{}, domain.ErrNotConfigured
}
func main() {
	envs, e := scope.Load(os.Getenv("INFRA_SCOPE_FILE"))
	if e != nil {
		log.Fatal("scope configuration unavailable")
	}
	header := os.Getenv("INFRA_AUTH_HEADER")
	if header == "" {
		header = "Authorization"
	}
	h, e := api.New(unavailable{}, envs, os.Getenv("INFRA_USERNAME"), os.Getenv("INFRA_PASSWORD"), header)
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
