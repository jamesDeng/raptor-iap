package main

import (
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/openapi"
	"log"
	"net/http"
	"os"
	"time"
)

func main() {
	user, password := os.Getenv("SERVICE_USERNAME"), os.Getenv("SERVICE_PASSWORD")
	if user == "" || password == "" {
		log.Fatal("service authentication required")
	}
	base := os.Getenv("RAPTOR_BACKEND_URL")
	if base == "" {
		base = "http://127.0.0.1:8871"
	}
	h, e := openapi.NewHandler(openapi.Client{BaseURL: base, Username: user, Password: password})
	if e != nil {
		log.Fatal("backend configuration invalid")
	}
	addr := os.Getenv("RAPTOR_OPEN_API_ADDR")
	if addr == "" {
		addr = "127.0.0.1:8872"
	}
	server := &http.Server{Addr: addr, Handler: h, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second}
	log.Printf("Raptor Open API listening on %s", addr)
	log.Fatal(server.ListenAndServe())
}
