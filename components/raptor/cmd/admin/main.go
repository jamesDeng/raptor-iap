package main

import (
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/admin"
	"log"
	"net/http"
	"os"
	"time"
)

func main() {
	backendURL := os.Getenv("RAPTOR_BACKEND_URL")
	if backendURL == "" {
		backendURL = "http://127.0.0.1:8871"
	}
	h, e := admin.NewHandler(backendURL)
	if e != nil {
		log.Fatal("invalid backend configuration")
	}
	addr := os.Getenv("RAPTOR_ADMIN_ADDR")
	if addr == "" {
		addr = "127.0.0.1:8873"
	}
	s := &http.Server{Addr: addr, Handler: h, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 30 * time.Second}
	log.Printf("Raptor admin listening on %s", addr)
	log.Fatal(s.ListenAndServe())
}
