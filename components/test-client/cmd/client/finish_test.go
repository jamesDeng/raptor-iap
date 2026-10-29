package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"github.com/jamesDeng/raptor-iap/components/test-client/internal/control"
	"github.com/jamesDeng/raptor-iap/components/test-client/internal/observe"
	"github.com/jamesDeng/raptor-iap/components/test-client/internal/traffic"
	"io"
	"net/http/httptest"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

type signalSession struct{ gate <-chan struct{} }

func (s *signalSession) Operate(ctx context.Context, _ string) traffic.Outcome {
	select {
	case <-s.gate:
		return traffic.Success
	case <-ctx.Done():
		return traffic.Timeout
	}
}
func (*signalSession) Close() {}

// A subprocess keeps the real OS signal isolated from the test runner. The
// external SQL boundary is gated; Runner and the service coordination are real.
func TestSIGTERMDrainsAcceptedQueueAndRetainsFinalEvidence(t *testing.T) {
	cmd := exec.Command(os.Args[0], "-test.run=^TestSIGTERMClientProcess$")
	cmd.Env = append(os.Environ(), "TEST_SIGNAL_CLIENT_HELPER=1")
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	cmd.Stderr = os.Stderr
	if err = cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill() })
	lines := make(chan string, 20)
	go func() {
		defer close(lines)
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			lines <- scanner.Text()
		}
	}()
	wait := make(chan error, 1)
	go func() { wait <- cmd.Wait() }()
	select {
	case line := <-lines:
		if line != "ADMITTED" {
			t.Fatal(line)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("client did not admit active and queued operations")
	}
	start := time.Now()
	if err = cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	select {
	case line := <-lines:
		if !strings.HasPrefix(line, "FINAL ") {
			t.Fatal(line)
		}
		var s observe.Snapshot
		if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "FINAL ")), &s); err != nil {
			t.Fatal(err)
		}
		if !s.Final || s.Connected != 0 || s.Sample.Scheduled != 6 || s.Sample.Attempts != 6 || s.Sample.Success != 6 || s.Sample.Skipped != 0 || s.Sample.Failure != 0 || s.Sample.Timeout != 0 {
			t.Fatalf("unsettled final: %+v", s)
		}
	case <-time.After(8 * time.Second):
		t.Fatal("final evidence unavailable")
	}
	select {
	case err = <-wait:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(30*time.Second - time.Since(start)):
		t.Fatal("SIGTERM exceeded chart grace")
	}
	if time.Since(start) < 10*time.Second {
		t.Fatal("final evidence hold shortened")
	}
}

func TestSIGTERMClientProcess(t *testing.T) {
	if os.Getenv("TEST_SIGNAL_CLIENT_HELPER") != "1" {
		t.Skip("subprocess helper")
	}
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer cancel()
	c, err := control.New("pod", "process")
	if err != nil {
		t.Fatal(err)
	}
	store := observe.New("process", 4, 8192)
	gate := make(chan struct{})
	// Forward two actual service-clock ticks, then wait for the OS signal.
	clock := controlledTicks(ctx, c.Done(), 50*time.Millisecond)
	ticks := make(chan time.Time)
	go func() {
		defer close(ticks)
		for i := 0; i < 2; i++ {
			ticks <- <-clock
		}
		<-ctx.Done()
		for range clock {
		}
		close(gate)
	}()
	scheduled := 0
	r := traffic.Runner{Config: traffic.Config{Sessions: 4, Idle: 1, Timeout: 5 * time.Second, ProcessID: "process"}, Ticks: ticks, Admit: c.Admit,
		Dial: func(context.Context) (traffic.Session, error) { return &signalSession{gate: gate}, nil },
		Emit: func(e traffic.Event) {
			store.Record(e)
			if e.Kind == "scheduled" {
				scheduled++
				if scheduled == 6 {
					fmt.Println("ADMITTED")
				}
			}
		}}
	if err := r.Run(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Finalize(); err != nil {
		t.Fatal(err)
	}
	c.MarkFinal()
	handler := evidenceHandler(store)
	for _, path := range []string{"/evidence", "/ready", "/health"} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest("GET", path, nil))
		if path == "/evidence" {
			fmt.Println("FINAL " + strings.TrimSpace(response.Body.String()))
		} else if path == "/ready" && response.Code != 503 || path == "/health" && response.Code != 200 {
			t.Fatal("incorrect final endpoint status", path, response.Code)
		}
	}
	holdFinal(context.Background(), 10*time.Second)
}

func TestFinishArgumentsBeforeSecret(t *testing.T) {
	for _, args := range [][]string{{"finish"}, {"finish", "--expected-pod-uid", "u"}, {"finish", "--bad", "secret"}, {"other"}} {
		if err := finishCommand(context.Background(), args, io.Discard); err == nil {
			t.Fatal("invalid command accepted")
		}
	}
}
func TestFinalEvidenceHealthAndReadiness(t *testing.T) {
	store := observe.New("p", 1, 10)
	if _, err := store.Finalize(); err != nil {
		t.Fatal(err)
	}
	handler := evidenceHandler(store)
	for _, tc := range []struct {
		path string
		code int
	}{{"/ready", 503}, {"/health", 200}, {"/evidence", 200}} {
		r := httptest.NewRecorder()
		handler.ServeHTTP(r, httptest.NewRequest("GET", tc.path, nil))
		if r.Code != tc.code {
			t.Fatal(tc.path, r.Code)
		}
		if tc.path == "/evidence" {
			var s observe.Snapshot
			if json.Unmarshal(r.Body.Bytes(), &s) != nil || !s.Final || s.Connected != 0 {
				t.Fatal(r.Body.String())
			}
		}
	}
}
func TestControlledTicksCloseWithoutCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	ticks := controlledTicks(ctx, done, time.Hour)
	close(done)
	select {
	case _, ok := <-ticks:
		if ok {
			t.Fatal("tick after finish")
		}
	case <-time.After(time.Second):
		t.Fatal("ticks did not close")
	}
	if ctx.Err() != nil {
		t.Fatal("runner context cancelled")
	}
}
func TestHoldBoundedAndSignalInterruptible(t *testing.T) {
	start := time.Now()
	holdFinal(context.Background(), time.Millisecond)
	if time.Since(start) > time.Second {
		t.Fatal("hold unbounded")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	start = time.Now()
	holdFinal(ctx, 2*time.Minute)
	if time.Since(start) > time.Second {
		t.Fatal("signal ignored")
	}
}

func TestServiceRequiresPodUID(t *testing.T) {
	path := filepath.Join(t.TempDir(), "dsn")
	if err := os.WriteFile(path, []byte("postgres://localhost/test?sslmode=disable"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TEST_DB_DSN_FILE", path)
	t.Setenv("TEST_EVIDENCE_MODE", "local")
	t.Setenv("TEST_POD_UID", "")
	saved := os.Args
	os.Args = []string{"client"}
	defer func() { os.Args = saved }()
	if err := run(); err == nil || err.Error() != "control identity required" {
		t.Fatal("empty pod UID did not fail before service startup", err)
	}
}
