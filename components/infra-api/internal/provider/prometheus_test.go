package provider

import (
	"context"
	"encoding/pem"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"net/http"
	"net/http/httptest"
	"raptor-iap/infra-api/internal/domain"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestPrivatePrometheusTransportIsPinnedBoundedAndNoRedirect(t *testing.T) {
	for _, kind := range []string{"good", "auth failure", "redirect", "oversized", "cancelled", "foreign account", "foreign region"} {
		t.Run(kind, func(t *testing.T) {
			var destination atomic.Int32
			redirected := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				destination.Add(1)
				w.Write([]byte(`{"status":"success"}`))
			}))
			defer redirected.Close()
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "GET" || r.URL.Path != "/api/v1/namespaces/raptor-test/services/http:rdev-test-metrics-metrics:http/proxy/api/v1/query" || r.URL.Query().Get("query") != "up" || r.URL.Query().Get("time") != "2026-10-09T00:00:00Z" || r.Header.Get("Authorization") != "Bearer fixture" {
					t.Error("incorrect private service proxy request")
				}
				if kind == "auth failure" {
					w.WriteHeader(403)
					w.Write([]byte("private failure"))
					return
				}
				if kind == "redirect" {
					w.Header().Set("Location", redirected.URL)
					w.WriteHeader(302)
					return
				}
				if kind == "oversized" {
					w.Write([]byte(strings.Repeat("x", 1048577)))
					return
				}
				w.Write([]byte(`{"status":"success"}`))
			}))
			defer server.Close()
			ca := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw})
			client, e := kubernetesClient(&rest.Config{Host: server.URL, BearerToken: "fixture", TLSClientConfig: rest.TLSClientConfig{CAData: ca}, Timeout: time.Second})
			if e != nil {
				t.Fatal(e)
			}
			p := &reader{identity: func(context.Context, domain.Environment) (string, error) { return "123", nil }, kube: func(context.Context, domain.Environment) (kubernetes.Interface, error) { return client, nil }}
			env := domain.Environment{Code: "dev", AccountID: "123", Region: "ap-southeast-1", ClusterID: "cluster"}
			ctx := context.Background()
			if kind == "cancelled" {
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}
			if kind == "foreign account" {
				env.AccountID = "other"
			}
			if kind == "foreign region" {
				env.Region = "cn-hangzhou"
			}
			raw, e := p.PrometheusQuery(ctx, env, "up", "2026-10-09T00:00:00Z")
			if kind == "good" {
				if e != nil || string(raw) != `{"status":"success"}` {
					t.Fatalf("private query failed %v", e)
				}
			} else if e == nil {
				t.Fatal("accepted unsafe transport result")
			}
			if destination.Load() != 0 {
				t.Fatal("followed service proxy redirect")
			}
			if e != nil && strings.Contains(e.Error(), "private failure") {
				t.Fatal("private provider message leaked")
			}
		})
	}
}
