package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"github.com/jackc/pgx/v5"
	"github.com/jamesDeng/raptor-iap/components/test-client/internal/control"
	"github.com/jamesDeng/raptor-iap/components/test-client/internal/observe"
	"github.com/jamesDeng/raptor-iap/components/test-client/internal/traffic"
	"io"
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
	if len(os.Args) > 1 {
		return finishCommand(context.Background(), os.Args[1:], os.Stdout)
	}
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
	controller, e := control.New(os.Getenv("TEST_POD_UID"), hex.EncodeToString(id))
	if e != nil {
		return e
	}
	controlCtx, stopControl := context.WithCancel(context.Background())
	defer stopControl()
	controlErr := make(chan error, 1)
	go func() {
		err := control.Serve(controlCtx, control.SocketPath, controller)
		controlErr <- err
		if err != nil {
			cancel()
		}
	}()
	mux := evidenceHandler(store)

	srv := http.Server{Addr: ":9090", Handler: mux, ReadHeaderTimeout: 5 * time.Second, WriteTimeout: 5 * time.Second}
	srvErr := make(chan error, 1)
	go func() { srvErr <- srv.ListenAndServe(); cancel() }()
	runner := traffic.Runner{Config: traffic.Config{Sessions: 4, Idle: 1, Timeout: 5 * time.Second, Interval: time.Second, ProcessID: hex.EncodeToString(id)}, Dial: func(c context.Context) (traffic.Session, error) { return traffic.ConnectPostgres(c, dsn) }, Emit: store.Record}
	runner.Ticks = controlledTicks(ctx, controller.Done(), time.Second)
	e = runner.Run(ctx)
	if final, err := store.Finalize(); err == nil {
		// Sanitized final counters survive Prometheus restart in external capture/logs.
		_ = json.NewEncoder(os.Stdout).Encode(struct {
			Kind     string           `json:"kind"`
			Snapshot observe.Snapshot `json:"snapshot"`
		}{"traffic_final", final})
		// Keep final evidence readable throughout several one-second collector polls.
		controller.MarkFinal()
		hold := 10 * time.Second
		select {
		case <-controller.Done():
			hold = 2 * time.Minute
		default:
		}
		if hold == 2*time.Minute {
			holdFinal(ctx, hold)
		} else {
			holdFinal(context.Background(), hold)
		}
	} else if e == nil {
		e = err
	}
	stop, done := context.WithTimeout(context.Background(), 2*time.Second)
	defer done()
	_ = srv.Shutdown(stop)
	if err := <-srvErr; err != http.ErrServerClosed {
		return fmt.Errorf("evidence server stopped")
	}
	stopControl()
	if err := <-controlErr; err != nil {
		return err
	}
	return e
}

func evidenceHandler(store *observe.Store) http.Handler {
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
	return mux
}

func finishCommand(ctx context.Context, args []string, out io.Writer) error {
	if len(args) == 0 || args[0] != "finish" {
		return fmt.Errorf("invalid command")
	}
	flags := flag.NewFlagSet("finish", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	uid := flags.String("expected-pod-uid", "", "")
	pid := flags.String("expected-process-id", "", "")
	if flags.Parse(args[1:]) != nil || flags.NArg() != 0 {
		return fmt.Errorf("invalid finish arguments")
	}
	status, err := control.Request(ctx, control.SocketPath, *uid, *pid)
	if err != nil {
		return err
	}
	return json.NewEncoder(out).Encode(status)
}
func controlledTicks(ctx context.Context, done <-chan struct{}, interval time.Duration) <-chan time.Time {
	ticks := make(chan time.Time)
	go func() {
		defer close(ticks)
		timer := time.NewTicker(interval)
		defer timer.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-done:
				return
			case tick := <-timer.C:
				select {
				case <-done:
					return
				default:
				}
				select {
				case <-ctx.Done():
					return
				case <-done:
					return
				case ticks <- tick:
				}
			}
		}
	}()
	return ticks
}
func holdFinal(ctx context.Context, d time.Duration) {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-timer.C:
	case <-ctx.Done():
	}
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
