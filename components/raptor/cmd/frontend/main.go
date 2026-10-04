package main

import (
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/frontend"
	"log"
	"net/http"
	"os"
	"time"
)

func main() {
	base := os.Getenv("RAPTOR_BACKEND_URL")
	if base == "" {
		base = "http://127.0.0.1:8871"
	}
	h, e := frontend.NewHandler(base)
	if e != nil {
		log.Fatal("backend configuration invalid")
	}
	addr := os.Getenv("RAPTOR_FRONTEND_ADDR")
	if addr == "" {
		addr = "127.0.0.1:8870"
	}
	server := &http.Server{Addr: addr, Handler: h, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second}
	log.Printf("Raptor frontend listening on %s", addr)
	log.Fatal(server.ListenAndServe())
}
