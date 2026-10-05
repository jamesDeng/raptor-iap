package main

import (
	"log"
	"net/http"
	"os"
	"raptor-iap/infra-api/internal/api"
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
	h, e := api.New(reader, envs, os.Getenv("INFRA_USERNAME"), os.Getenv("INFRA_PASSWORD"), header)
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
