package adapters

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/domain"
)

type monitoringTransport func(*http.Request) (*http.Response, error)
func (f monitoringTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestPgcatMonitoringUsesFixedServiceRoute(t *testing.T) {
	calls := 0
	client := &http.Client{Transport: monitoringTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.Method != "POST" || r.URL.String() != "https://infra.example/v1/pgcat-monitoring-reconcile" ||
			r.Header.Get("X-Infra-Authorization") != "Basic c2VydmljZTpzZWNyZXQ=" || r.ContentLength != 0 {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL)
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"data":{"changed":true,"count":4}}`)), Header: http.Header{}}, nil
	})}
	infra := HTTPInfra{Username: "service", Password: "secret", AuthHeader: "X-Infra-Authorization", Client: client,
		ResolveEnvironment: func(_ context.Context, code string) (domain.Environment, error) {
			if code != "rdev.ali" { t.Fatal(code) }
			return domain.Environment{Code: code, Config: map[string]any{"infraApiUrl": "https://infra.example"}}, nil
		}}
	if err := infra.ReconcilePgcatMonitoring(context.Background()); err != nil || calls != 1 { t.Fatalf("reconcile: %v calls=%d", err, calls) }
	infra.ResolveEnvironment = func(context.Context, string) (domain.Environment, error) {
		return domain.Environment{Code: "other", Config: map[string]any{"infraApiUrl": "https://infra.example"}}, nil
	}
	if err := infra.ReconcilePgcatMonitoring(context.Background()); err == nil || calls != 1 { t.Fatalf("accepted foreign environment: %v calls=%d", err, calls) }
}
