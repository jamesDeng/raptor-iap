package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/jackc/pgx/v5"
	"github.com/jamesDeng/raptor-iap/components/test-client/internal/observe"
	"github.com/jamesDeng/raptor-iap/components/test-client/internal/traffic"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"
)

func main() {
	if run() != nil {
		fmt.Fprintln(os.Stderr, "test client stopped with configuration/runtime error")
		os.Exit(1)
	}
}
func run() error {
	path := os.Getenv("TEST_DB_DSN_FILE")
	if path == "" {
		return fmt.Errorf("secret file required")
	}
	raw, e := os.ReadFile(path)
	if e != nil {
		return fmt.Errorf("secret unreadable")
	}
	dsn := strings.TrimSpace(string(raw))
	cfg, e := pgx.ParseConfig(dsn)
	if e != nil {
		return fmt.Errorf("invalid secret configuration")
	}
	if os.Getenv("TEST_EVIDENCE_MODE") != "local" {
		if e := validateTransport(cfg, os.Getenv("TEST_ALLOW_PLAINTEXT_POC") == "true"); e != nil {
			return e
		}
	}
	id := make([]byte, 16)
	if _, e = rand.Read(id); e != nil {
		return e
	}
	store := observe.New(hex.EncodeToString(id), 4, 8192)
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer cancel()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /metrics", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4")
		fmt.Fprint(w, store.Metrics())
	})
	mux.HandleFunc("GET /evidence", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(store.Snapshot())
	})
	mux.HandleFunc("GET /ready", func(w http.ResponseWriter, r *http.Request) {
		if !store.Ready() {
			w.WriteHeader(503)
		}
		fmt.Fprint(w, "readiness")
	})
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, "ok") })
	srv := http.Server{Addr: ":9090", Handler: mux, ReadHeaderTimeout: 5 * time.Second, WriteTimeout: 5 * time.Second}
	srvErr := make(chan error, 1)
	go func() { srvErr <- srv.ListenAndServe(); cancel() }()
	runner := traffic.Runner{Config: traffic.Config{Sessions: 4, Idle: 1, Timeout: 5 * time.Second, Interval: time.Second, ProcessID: hex.EncodeToString(id)}, Dial: func(c context.Context) (traffic.Session, error) { return traffic.ConnectPostgres(c, dsn) }, Emit: store.Record}
	e = runner.Run(ctx)
	stop, done := context.WithTimeout(context.Background(), 2*time.Second)
	defer done()
	_ = srv.Shutdown(stop)
	if err := <-srvErr; err != http.ErrServerClosed {
		return fmt.Errorf("evidence server stopped")
	}
	return e
}

func validateTransport(cfg *pgx.ConnConfig, allowPlaintext bool) error {
	if cfg.TLSConfig == nil && allowPlaintext && len(cfg.Fallbacks) == 0 {
		return nil
	}
	if cfg.TLSConfig == nil || cfg.TLSConfig.InsecureSkipVerify || len(cfg.Fallbacks) > 0 {
		return fmt.Errorf("verified TLS or explicit POC plaintext required")
	}
	return nil
}
