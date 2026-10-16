package main

import (
	"context"
	"encoding/json"
	"github.com/jamesDeng/raptor-iap/components/test-client/internal/observe"
	"io"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

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
