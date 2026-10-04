package main

import (
	"context"
	"flag"
	"fmt"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/adapters"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/auth"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/backend"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/db"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/domain"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/requests"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/skills"
	"golang.org/x/term"
	"log"
	"net/http"
	"os"
	"time"
)

func main() {
	bootstrap := flag.String("bootstrap-admin", "", "Create an administrator; password entered privately")
	migrate := flag.Bool("migrate", false, "Run owner migrations")
	flag.Parse()
	ctx := context.Background()
	databaseURL := os.Getenv("RAPTOR_DATABASE_URL")
	if *migrate {
		databaseURL = os.Getenv("MIGRATION_DATABASE_URL")
	}
	if databaseURL == "" {
		log.Fatal("database configuration is required")
	}
	p, e := db.Open(ctx, databaseURL)
	if e != nil {
		log.Fatal("database unavailable")
	}
	defer p.Close()
	if *migrate {
		if db.Migrate(ctx, p) != nil {
			log.Fatal("migration failed")
		}
		return
	}
	if *bootstrap != "" {
		fmt.Print("Administrator password (hidden): ")
		b, e := term.ReadPassword(int(os.Stdin.Fd()))
		fmt.Println()
		if e != nil {
			log.Fatal("private password input unavailable")
		}
		_, e = auth.NewService(p).CreateUser(ctx, domain.User{Role: "admin"}, *bootstrap, string(b), "admin")
		if e != nil {
			log.Fatal("administrator creation failed")
		}
		return
	}
	addr := os.Getenv("RAPTOR_BACKEND_ADDR")
	if addr == "" {
		addr = "127.0.0.1:8871"
	}
	h := backend.New(p)
	h.Skills.Source = skills.GitSource{Repository: os.Getenv("RAPTOR_REPOSITORY")}
	if gatewayURL := os.Getenv("GATEWAY_URL"); gatewayURL != "" {
		client := requests.HTTPGateway{BaseURL: gatewayURL, Username: os.Getenv("SERVICE_USERNAME"), Password: os.Getenv("SERVICE_PASSWORD")}
		h.Requests.Gateway = client
		go func() {
			ticker := time.NewTicker(time.Second)
			defer ticker.Stop()
			for range ticker.C {
				if h.Requests.DispatchPending(ctx, client) != nil {
					log.Print("request dispatch will retry")
				}
			}
		}()
	}
	h.RegisterService(os.Getenv("SERVICE_USERNAME"), os.Getenv("SERVICE_PASSWORD"))
	h.Mux.Handle("POST /webhooks/github", h.GitHub.Handler(os.Getenv("GITHUB_WEBHOOK_SECRET")))
	if os.Getenv("RAPTOR_SIMULATION") == "true" && os.Getenv("RAPTOR_FIXTURE_FILE") != "" {
		h.Catalog.Infra = adapters.FixtureInfra{Path: os.Getenv("RAPTOR_FIXTURE_FILE")}
		commands := &adapters.SimulatedCommands{}
		go func() {
			ticker := time.NewTicker(time.Second)
			defer ticker.Stop()
			for range ticker.C {
				if h.Requests.RunDirectPending(ctx, commands) != nil {
					log.Print("simulated direct execution requires attention")
				}
			}
		}()
	}
	server := &http.Server{Addr: addr, Handler: h, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second}
	log.Printf("Raptor backend listening on %s", addr)
	log.Fatal(server.ListenAndServe())
}
