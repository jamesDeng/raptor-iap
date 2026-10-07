package runtime

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func fixtureTransport(t *testing.T, h http.HandlerFunc) (*SandboxTransport, func()) {
	t.Helper()
	s := httptest.NewTLSServer(h)
	tr, e := NewSandboxTransport(s.URL, "ap-southeast-1.e2b.fc.aliyuncs.com", "synthetic-control", s.Client().Transport)
	if e != nil {
		t.Fatal(e)
	}
	tr.envdFixtureURL = s.URL
	return tr, s.Close
}
func frame(raw string) []byte {
	b := make([]byte, 5+len(raw))
	binary.BigEndian.PutUint32(b[1:5], uint32(len(raw)))
	copy(b[5:], raw)
	return b
}
func TestNativeCreateConnectAndFiles(t *testing.T) {
	tr, close := fixtureTransport(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/sandboxes":
			if r.Method != "POST" || r.Header.Get("X-API-Key") != "synthetic-control" {
				t.Error("control auth")
			}
			var in map[string]any
			json.NewDecoder(r.Body).Decode(&in)
			if in["templateID"] != "template" || in["timeout"] != float64(900) {
				t.Error(in)
			}
			w.WriteHeader(201)
			w.Write([]byte(`{"sandboxID":"sandbox","envdVersion":"0.5.7","templateID":"template","envdAccessToken":"synthetic-envd"}`))
		case "/sandboxes/sandbox/connect":
			w.Write([]byte(`{"sandboxID":"sandbox","envdVersion":"0.5.7","templateID":"template","envdAccessToken":"synthetic-envd"}`))
		case "/files":
			if r.Header.Get("E2b-Sandbox-Id") != "sandbox" || r.Header.Get("X-Access-Token") != "synthetic-envd" || r.Header.Get("X-API-Key") != "" {
				t.Error("envd credential boundary")
			}
			if r.Method == "POST" {
				if e := r.ParseMultipartForm(1024); e != nil {
					t.Error(e)
				}
				w.Write([]byte(`[]`))
			} else {
				w.Write([]byte("hello"))
			}
		default:
			t.Error(r.URL.Path)
		}
	})
	defer close()
	ctx := context.Background()
	ref, e := tr.Create(ctx, CreateSpec{Template: "template", AttemptID: "attempt", VolumeName: "volume", ExecutionRole: "acs:ram::123:role/test", TTL: 900})
	if e != nil {
		t.Fatal(e)
	}
	ref, e = tr.Connect(ctx, ref)
	if e != nil {
		t.Fatal(e)
	}
	if e = tr.WriteFile(ctx, ref, "/tmp/fixture", []byte("hello")); e != nil {
		t.Fatal(e)
	}
	v, e := tr.ReadFile(ctx, ref, "/tmp/fixture", 10)
	if e != nil || string(v) != "hello" {
		t.Fatal(e, string(v))
	}
}
func TestNativeCreateIsNeverRetried(t *testing.T) {
	var count atomic.Int32
	tr, close := fixtureTransport(t, func(w http.ResponseWriter, r *http.Request) { count.Add(1); w.WriteHeader(503) })
	defer close()
	_, e := tr.Create(context.Background(), CreateSpec{Template: "template", AttemptID: "attempt", VolumeName: "volume", ExecutionRole: "role", TTL: 900})
	if e != ErrCreateUnconfirmed || count.Load() != 1 {
		t.Fatal(e, count.Load())
	}
	_, e = tr.Create(context.Background(), CreateSpec{TTL: 901})
	if e != ErrConfiguration || count.Load() != 1 {
		t.Fatal("TTL not bounded")
	}
}
func TestNativeRejectsCredentialRedirect(t *testing.T) {
	var destination atomic.Int32
	other := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { destination.Add(1) }))
	defer other.Close()
	tr, close := fixtureTransport(t, func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, other.URL, 307) })
	defer close()
	_, e := tr.Create(context.Background(), CreateSpec{Template: "template", AttemptID: "attempt", VolumeName: "volume", ExecutionRole: "role", TTL: 900})
	if e == nil || destination.Load() != 0 {
		t.Fatal("redirect followed")
	}
}
func TestNativeCommandStartFramesAndAmbiguity(t *testing.T) {
	for _, raw := range []string{`{"event":{"start":{"pid":12}}}`, `{"event":{"end":{"exitCode":0,"exited":true}}}`, `malformed`} {
		t.Run(raw, func(t *testing.T) {
			tr, close := fixtureTransport(t, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/process.Process/Start" || r.Header.Get("Content-Type") != "application/connect+json" {
					t.Error("RPC wire")
				}
				w.Header().Set("Content-Type", "application/connect+json")
				w.Write(frame(raw))
			})
			defer close()
			ref := SandboxRef{ID: "sandbox", EnvdVersion: "0.5.7", accessToken: "synthetic-envd"}
			command, e := tr.StartCommand(context.Background(), ref, CommandSpec{Executable: "/bin/bash", Args: []string{"-c", "true"}, Tag: "generation", Deadline: time.Second})
			if raw == `{"event":{"start":{"pid":12}}}` {
				if e != nil || command.PID != 12 {
					t.Fatal(e, command)
				}
			} else if e != ErrCommandUnconfirmed {
				t.Fatal("ambiguous command accepted", e)
			}
		})
	}
}
func TestNativeReadBoundAndTerminationConfirmation(t *testing.T) {
	var killed bool
	tr, close := fixtureTransport(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/files" {
			w.Write([]byte("oversized"))
			return
		}
		if r.Method == "DELETE" {
			killed = true
			w.WriteHeader(204)
			return
		}
		if killed {
			w.WriteHeader(404)
		} else {
			w.Write([]byte(`{"sandboxID":"sandbox"}`))
		}
	})
	defer close()
	ref := SandboxRef{ID: "sandbox", EnvdVersion: "0.5.7"}
	if _, e := tr.ReadFile(context.Background(), ref, "/tmp/file", 2); e != ErrInvalidRuntimeData {
		t.Fatal("unbounded read", e)
	}
	if e := tr.Terminate(context.Background(), ref); e != nil {
		t.Fatal(e)
	}
	state, e := tr.Inspect(context.Background(), ref)
	if e != nil || !state.Absent {
		t.Fatal("termination not confirmed", e)
	}
}
func TestSandboxInventoryRequiresCompleteAttemptPages(t *testing.T) {
	tr, close := fixtureTransport(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("metadata") != "raptor.attempt=attempt" {
			t.Error("inventory not bound")
		}
		if r.URL.Query().Get("nextToken") == "" {
			w.Header().Set("X-Next-Token", "next")
			w.Write([]byte(`[{"sandboxID":"first","metadata":{"raptor.attempt":"attempt"}}]`))
		} else {
			w.Write([]byte(`[{"sandboxID":"second","metadata":{"raptor.attempt":"attempt"}}]`))
		}
	})
	defer close()
	refs, e := tr.InventorySandboxes(context.Background(), "attempt")
	if e != nil || len(refs) != 2 {
		t.Fatal(refs, e)
	}
}
func TestNativeWaitRequiresExitEventAndCleanConnectEnd(t *testing.T) {
	for _, success := range []bool{true, false} {
		tr, close := fixtureTransport(t, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/connect+json")
			w.Write(frame(`{"event":{"start":{"pid":12}}}`))
			if success {
				w.Write(frame(`{"event":{"end":{"exitCode":0,"exited":true}}}`))
				end := frame(`{}`)
				end[0] = 2
				w.Write(end)
			}
		})
		ref := SandboxRef{ID: "sandbox", EnvdVersion: "0.5.7"}
		command, e := tr.StartCommand(context.Background(), ref, CommandSpec{Executable: "/bin/bash", Args: []string{"-c", "true"}, Tag: "generation", Deadline: time.Second})
		if e != nil {
			t.Fatal(e)
		}
		e = command.Wait()
		if success && e != nil || !success && e == nil {
			t.Fatal("EOF mistaken for completion", e)
		}
		close()
	}
}
