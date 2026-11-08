package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"raptor-iap/infra-api/internal/domain"
)

type timerResolver struct{ calls int }

func (r *timerResolver) Resolve(_ context.Context, env, kind, code string) (domain.ResolvedResource, error) {
	r.calls++
	if env != "rdev.ali" || kind != "db-proxy" || code != "DP00001" {
		return domain.ResolvedResource{}, fmt.Errorf("wrong selector")
	}
	return domain.ResolvedResource{Environment: domain.Environment{Code: env}, Proxy: &domain.ProxyMapping{Code: code, TargetDBCode: "D00002"}}, nil
}

type timerReconciler struct{ calls int }

func (r *timerReconciler) ReconcilePgcatMonitoring(_ context.Context, _ domain.Environment, _ domain.ProxyMapping) (domain.PgcatInventoryReceipt, error) {
	r.calls++
	return domain.PgcatInventoryReceipt{InstanceIDs: []string{"one", "two"}}, nil
}

func TestMonitoringTimerOnlyAcceptsFixedFreshEvent(t *testing.T) {
	now := time.Date(2026, 11, 8, 8, 0, 0, 0, time.UTC)
	resolver, reconciler := &timerResolver{}, &timerReconciler{}
	base := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusTeapot) })
	h := monitoringTimerHandler(base, resolver, reconciler, func() time.Time { return now })
	call := func(body, path, contentType string) int {
		t.Helper()
		r := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
		r.Header.Set("Content-Type", contentType)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w.Code
	}
	valid := `{"triggerTime":"2026-11-08T08:00:00Z","triggerName":"raptor-rdev-pgcat-monitoring","payload":"pgcat-monitoring-v1"}`
	for _, body := range []string{
		strings.Replace(valid, "pgcat-monitoring-v1", "foreign", 1),
		strings.Replace(valid, "08:00:00Z", "07:50:00Z", 1),
		strings.Replace(valid, `"payload":`, `"payload":"duplicate","payload":`, 1),
		strings.Replace(valid, `"triggerName":`, `"unknown":1,"triggerName":`, 1),
	} {
		if got := call(body, "/invoke", "application/octet-stream"); got != http.StatusNotFound {
			t.Fatalf("invalid event accepted: %d", got)
		}
	}
	if got := call(valid, "/invoke?x=1", "application/octet-stream"); got != http.StatusNotFound {
		t.Fatalf("query accepted: %d", got)
	}
	if got := call(valid, "/invoke", "application/json"); got != http.StatusNotFound {
		t.Fatalf("http-like event accepted: %d", got)
	}
	if resolver.calls != 0 || reconciler.calls != 0 {
		t.Fatal("invalid timer reached provider")
	}
	if got := call(valid, "/invoke", "application/octet-stream"); got != http.StatusOK || resolver.calls != 1 || reconciler.calls != 1 {
		t.Fatalf("valid event: %d, calls %d/%d", got, resolver.calls, reconciler.calls)
	}
	if got := call(valid, "/healthz", "application/octet-stream"); got != http.StatusTeapot {
		t.Fatalf("existing HTTP route changed: %d", got)
	}
}
