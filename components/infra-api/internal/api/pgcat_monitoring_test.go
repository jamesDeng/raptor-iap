package api

import (
	"context"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"raptor-iap/infra-api/internal/domain"
	"strings"
	"testing"
)

type monitoringReader struct {
	observedReader
	calls int
}

func (r *monitoringReader) ReconcilePgcatMonitoring(_ context.Context, env domain.Environment, mapping domain.ProxyMapping) (domain.PgcatInventoryReceipt, error) {
	r.calls++
	if env.Code != "rdev.ali" || mapping.Code != "DP00001" {
		return domain.PgcatInventoryReceipt{}, domain.ErrScope
	}
	return domain.PgcatInventoryReceipt{InstanceIDs: []string{"i-one"}}, nil
}

func TestPgcatMonitoringServiceOnlyFixedRoute(t *testing.T) {
	m := domain.ProxyMapping{Code: "DP00001", TargetDBCode: "D00002"}
	resolver := &resourceFixture{rows: domain.ResolvedResource{Environment: domain.Environment{Code: "rdev.ali"}, Proxy: &m}}
	reader := &monitoringReader{}
	h, err := NewResourceServer(reader, resolver, "service", "secret", "X-Infra-Authorization", nil,
		NewServiceAuthorizer("service", "/v1/pgcat-monitoring-reconcile"), nil)
	if err != nil {
		t.Fatal(err)
	}
	call := func(method, auth, query, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, "/v1/pgcat-monitoring-reconcile"+query, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		if auth != "" {
			r.Header.Set("X-Infra-Authorization", auth)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	service := "Basic " + base64.StdEncoding.EncodeToString([]byte("service:secret"))
	if w := call("POST", service, "", ""); w.Code != 200 || !strings.Contains(w.Body.String(), `"count":1`) {
		t.Fatalf("service reconcile: %d %s", w.Code, w.Body.String())
	}
	if reader.calls != 1 || resolver.calls != 1 {
		t.Fatalf("unexpected calls: %d %d", reader.calls, resolver.calls)
	}
	for _, tc := range []struct{ method, auth, query, body string }{
		{"GET", service, "", ""}, {"POST", "", "", ""}, {"POST", service, "?envCode=other", ""}, {"POST", service, "", `{}`},
	} {
		if w := call(tc.method, tc.auth, tc.query, tc.body); w.Code == http.StatusOK {
			t.Fatalf("accepted %+v", tc)
		}
	}
	if reader.calls != 1 {
		t.Fatalf("reconciled invalid request: %d", reader.calls)
	}
}
