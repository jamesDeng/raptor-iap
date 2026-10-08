package evidence

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestCaptureRecordsSamplesAndMarksUnavailableReplica(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "missing") {
			w.WriteHeader(404)
			return
		}
		_ = json.NewEncoder(w).Encode(RemoteSnapshot{Sample: Sample{ProcessID: "p", At: time.Now()}})
	}))
	defer server.Close()
	b, err := Capture(context.Background(), []Target{{PodUID: "pod-a", URL: server.URL}, {PodUID: "pod-b", URL: server.URL + "/missing"}}, 30*time.Millisecond, 10*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	if len(b.Samples) < 2 || len(b.Issues) == 0 {
		t.Fatalf("%+v", b)
	}
}
func TestCaptureRefusesRedirectAndRestartedIdentity(t *testing.T) {
	var n atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := "before"
		if n.Add(1) > 1 {
			p = "after"
		}
		_ = json.NewEncoder(w).Encode(RemoteSnapshot{Sample: Sample{ProcessID: p, At: time.Now()}})
	}))
	defer server.Close()
	b, err := Capture(context.Background(), []Target{{PodUID: "pod", URL: server.URL}}, 30*time.Millisecond, 10*time.Millisecond)
	if err != nil || len(b.Issues) == 0 {
		t.Fatal("restart lost", err, b)
	}
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, server.URL, 302) }))
	defer redirect.Close()
	b, err = Capture(context.Background(), []Target{{PodUID: "pod", URL: redirect.URL}}, 10*time.Millisecond, 10*time.Millisecond)
	if err != nil || len(b.Samples) != 0 || len(b.Issues) == 0 {
		t.Fatal("redirect followed", err, b)
	}
}
