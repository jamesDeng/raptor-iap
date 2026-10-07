package main

import (
	"context"
	"encoding/json"
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
	mode, e := runtimeadapter.RuntimeMode(os.Getenv("GATEWAY_RUNTIME_MODE"), os.Getenv("GATEWAY_SIMULATION"))
	if e != nil {
		log.Fatal("runtime mode conflict or invalid configuration")
	}
	s := &execution.Store{Pool: p, KnownSecrets: []string{password}, RuntimeMode: mode}
	if path := os.Getenv("GATEWAY_REDACTION_VALUES_FILE"); path != "" {
		info, e := os.Stat(path)
		if e != nil || info.Mode().Perm()&0077 != 0 {
			log.Fatal("private redaction values file required")
		}
		b, e := os.ReadFile(path)
		var values []string
		if e != nil || json.Unmarshal(b, &values) != nil {
			log.Fatal("redaction configuration unavailable")
		}
		s.KnownSecrets = append(s.KnownSecrets, values...)
	}
	if mode == "simulated" {
		release, e := s.AcquireWorker(ctx)
		if e != nil {
			log.Fatal("another worker is active or worker lock unavailable")
		}
		defer release()
		root := os.Getenv("GATEWAY_CHECKPOINT_DIR")
		if root == "" {
			log.Fatal("private checkpoint directory required")
		}
		worker := &execution.Worker{Store: s, Owner: execution.NewID(), Runtime: &runtimeadapter.Simulated{Root: root}, Raptor: execution.HTTPRaptor{BaseURL: os.Getenv("RAPTOR_OPEN_API_URL"), Username: user, Password: password}}
		worker.SimulatedFlow = os.Getenv("GATEWAY_SIMULATED_SCENARIO") == "approval-review"
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
				if worker.RunActive(ctx) != nil {
					log.Print("simulated active runtime requires attention")
				}
				if worker.RunNext(ctx) != nil {
					log.Print("simulated worker encountered unavailable dependency")
				}
			}
		}()
	}

	if mode == "live" {
		var config runtimeadapter.LiveConfig
		var credential runtimeadapter.ControllerCredential
		if runtimeadapter.ReadPrivateJSON(os.Getenv("GATEWAY_LIVE_CONFIG_FILE"), &config) != nil || runtimeadapter.ReadPrivateJSON(os.Getenv("GATEWAY_CONTROLLER_CREDENTIAL_FILE"), &credential) != nil {
			log.Fatal("private live configuration required")
		}
		management, verifier, e := runtimeadapter.NewCloudClients(config, credential)
		if e != nil {
			log.Fatal("live configuration invalid")
		}
		preflight, cancel := context.WithTimeout(ctx, 30*time.Second)
		if management.Preflight(preflight, config) != nil {
			cancel()
			log.Fatal("existing volume preflight failed")
		}
		bootstrap, e := verifier.VerifyCheckpoint(preflight, config.BootstrapCheckpoint)
		cancel()
		if e != nil {
			log.Fatal("bootstrap checkpoint verification failed")
		}
		lease, e := s.AcquireLiveWorker(ctx)
		if e != nil {
			log.Fatal("exclusive live worker unavailable")
		}
		defer lease.Close()
		owner := execution.NewID()
		raptor := execution.HTTPRaptor{BaseURL: os.Getenv("RAPTOR_OPEN_API_URL"), Username: user, Password: password}
		adapter := &runtimeadapter.NativeLive{Config: config, Management: management, Verifier: verifier, Store: s, Owner: owner, Lease: lease}
		worker := &execution.LiveWorker{Store: s, Owner: owner, Lease: lease, Runtime: adapter, Access: raptor, Raptor: raptor, Bootstrap: bootstrap}
		if worker.Reconcile(ctx) != nil {
			log.Fatal("live reconciliation requires attention")
		}
		go func() {
			ticker := time.NewTicker(2 * time.Second)
			defer ticker.Stop()
			for range ticker.C {
				if !lease.Valid(ctx) {
					log.Print("live worker ownership lost; dispatch stopped")
					return
				}
				if worker.RunActive(ctx) != nil {
					log.Print("live active attempt requires attention")
				}
				if worker.RunNext(ctx) != nil {
					log.Print("live worker dependency unavailable")
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
