package main

import (
	"context"
	"flag"
	"github.com/jamesDeng/raptor-iap/components/agent-gateway/internal/db"
	"github.com/jamesDeng/raptor-iap/components/agent-gateway/internal/execution"
	"github.com/jamesDeng/raptor-iap/components/agent-gateway/internal/httpapi"
	runtimeadapter "github.com/jamesDeng/raptor-iap/components/agent-gateway/internal/runtime"
	"log"
	"net/http"
	"os"
	"time"
)

func main() {
	migrate := flag.Bool("migrate", false, "Run schema-owner migrations")
	flag.Parse()
	ctx := context.Background()
	databaseURL := os.Getenv("GATEWAY_DATABASE_URL")
	if *migrate {
		databaseURL = os.Getenv("MIGRATION_DATABASE_URL")
	}
	if databaseURL == "" {
		log.Fatal("database configuration required")
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
	user, password := os.Getenv("SERVICE_USERNAME"), os.Getenv("SERVICE_PASSWORD")
	if user == "" || password == "" {
		log.Fatal("service authentication required")
	}
	s := &execution.Store{Pool: p}
	if os.Getenv("GATEWAY_SIMULATION") == "true" {
		root := os.Getenv("GATEWAY_CHECKPOINT_DIR")
		if root == "" {
			log.Fatal("private checkpoint directory required")
		}
		worker := &execution.Worker{Store: s, Owner: execution.NewID(), Runtime: &runtimeadapter.Simulated{Root: root}, Raptor: execution.HTTPRaptor{BaseURL: os.Getenv("RAPTOR_OPEN_API_URL"), Username: user, Password: password}}
		go func() {
			ticker := time.NewTicker(time.Second)
			defer ticker.Stop()
			for range ticker.C {
				if worker.DrainSignals(ctx) != nil {
					log.Print("signal processing requires attention")
				}
				var held *string
				var owner *string
				if s.Pool.QueryRow(ctx, "SELECT request_id::text,owner FROM gateway.runtime_slot WHERE id=1").Scan(&held, &owner) == nil && held != nil && owner != nil && *owner == worker.Owner {
					if worker.ExpireApproval(ctx, *held) != nil {
						log.Print("approval wait requires attention")
					}
				}
				if worker.RunNext(ctx) != nil {
					log.Print("simulated worker encountered unavailable dependency")
				}
			}
		}()
	}
	addr := os.Getenv("GATEWAY_ADDR")
	if addr == "" {
		addr = "127.0.0.1:8874"
	}
	server := &http.Server{Addr: addr, Handler: httpapi.New(s, user, password), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second}
	log.Printf("Gateway listening on %s", addr)
	log.Fatal(server.ListenAndServe())
}
