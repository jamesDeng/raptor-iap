package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"raptor-iap/infra-api/internal/domain"
	"raptor-iap/infra-api/internal/jsoninput"
	"time"
)

const monitoringTriggerName = "raptor-rdev-pgcat-monitoring"
const monitoringTriggerPayload = "pgcat-monitoring-v1"

type monitoringResolver interface {
	Resolve(context.Context, string, string, string) (domain.ResolvedResource, error)
}

type monitoringReconciler interface {
	ReconcilePgcatMonitoring(context.Context, domain.Environment, domain.ProxyMapping) (domain.PgcatInventoryReceipt, error)
}

// FC sends timer events to POST /invoke for a custom runtime. API Gateway has
// no route to /invoke; every invocation still resolves the fixed catalog object
// and the provider verifies its account, cluster, ESS and NLB binding.
func monitoringTimerHandler(base http.Handler, resolver monitoringResolver, reconciler monitoringReconciler, now func() time.Time) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/invoke" {
			base.ServeHTTP(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		fail := func() {
			w.Header().Set("x-fc-status", "404")
			http.Error(w, `{"error":"monitoring reconciliation refused"}`, http.StatusNotFound)
		}
		if r.Method != http.MethodPost || r.URL.RawQuery != "" || r.Header.Get("Content-Type") != "application/octet-stream" ||
			r.ContentLength > 512 || resolver == nil || reconciler == nil {
			fail()
			return
		}
		body, err := io.ReadAll(io.LimitReader(r.Body, 513))
		if err != nil || len(body) > 512 {
			fail()
			return
		}
		var event struct {
			TriggerTime time.Time `json:"triggerTime"`
			TriggerName string    `json:"triggerName"`
			Payload     string    `json:"payload"`
		}
		if jsoninput.Decode(body, &event) != nil ||
			event.TriggerName != monitoringTriggerName || event.Payload != monitoringTriggerPayload ||
			event.TriggerTime.IsZero() || now().Sub(event.TriggerTime) > 2*time.Minute || event.TriggerTime.Sub(now()) > 5*time.Second {
			fail()
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 25*time.Second)
		defer cancel()
		resolved, err := resolver.Resolve(ctx, "rdev.ali", "db-proxy", "DP00001")
		if err != nil || resolved.Proxy == nil || resolved.Proxy.TargetDBCode != "D00002" {
			fail()
			return
		}
		receipt, err := reconciler.ReconcilePgcatMonitoring(ctx, resolved.Environment, *resolved.Proxy)
		if err != nil {
			fail()
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"changed": receipt.Changed, "count": len(receipt.InstanceIDs)})
	})
}
