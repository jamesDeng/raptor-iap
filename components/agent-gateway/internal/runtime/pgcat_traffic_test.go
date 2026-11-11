package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/jamesDeng/raptor-iap/components/agent-gateway/internal/execution"
	"net/netip"
	"strings"
	"testing"
	"time"
)

func TestPgcatTrafficWaitsForOldPodFinalLogsAfterDeploymentReady(t *testing.T) {
	now := time.Now().UTC()
	routed := map[string]netip.Addr{
		"new-1": netip.MustParseAddr("10.70.1.250"),
		"new-2": netip.MustParseAddr("10.70.1.251"),
	}
	start := execution.LiveStart{
		Binding: execution.AttemptBinding{RequestID: "request-1"},
		Access: execution.AgentAccess{ReplacementScope: &execution.ReplacementScope{
			ResolutionDigest: "scope-1", Application: execution.ApplicationScope{UID: "deployment-1"},
		}},
	}
	for _, tc := range []struct {
		name     string
		final    bool
		bad      bool
		lastAt   time.Time
		wantWait bool
		wantPods bool
	}{
		{name: "healthy stream still following", lastAt: now, wantWait: true},
		{name: "stream closed before final event", lastAt: now, bad: true},
		{name: "final event predates restart", lastAt: now.Add(-2 * time.Minute), final: true},
		{name: "final event received", lastAt: now, final: true, wantPods: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			checkedPods := false
			proxy := &ObservationProxy{
				pgcat: newPgcatContinuity(),
				ReadPgcatRestartAction: func(context.Context, string, string, string, string, string, string) (execution.PgcatRestartAction, error) {
					return execution.PgcatRestartAction{Generation: 2, ClaimedAt: now.Add(-time.Minute), RecordedAt: now.Add(-time.Minute)}, nil
				},
				execute: func(_ context.Context, _ execution.LiveStart, _ ObservationMaterial, tool string, args json.RawMessage) (json.RawMessage, error) {
					if tool == "deployment_read" {
						return json.Marshal(map[string]any{"uid": "deployment-1", "observedAt": now,
							"generation": 2, "observedGeneration": 2, "replicas": 2, "readyReplicas": 2, "updatedReplicas": 2, "availableReplicas": 2})
					}
					if tool == "pgcat_client_pods" {
						checkedPods = true
						if string(args) != `{"retiringPodUids":["old-1","old-2"]}` {
							t.Fatalf("pod discovery was not bound to the old pods: %s", args)
						}
					}
					return nil, ErrRuntimeUnavailable
				},
			}
			proxy.pgcat.requests["request-1"] = &pgcatHeldPair{
				id: "observation-1", scope: "scope-1", releasedAt: now.Add(-time.Minute),
				lastDrain: &pgcatDrainEvidence{FreshConnections: map[string]int{"new-1": 12, "new-2": 12}},
				logs: &pgcatAppLogWatch{streams: map[string]*pgcatLogState{
					"old-1": {success: 1, final: tc.final, bad: tc.bad, lastAt: tc.lastAt},
					"old-2": {success: 1, final: true, lastAt: now},
				}},
			}
			result, err := proxy.pgcatTraffic(ctx, observationEntry{start: start},
				pgcatContinuityArgs{Phase: "verify-traffic", ObservationID: "observation-1", RestartActionID: "restart-1"}, routed)
			if tc.wantWait {
				if err != nil || string(result) != `{"readiness":"traffic-warming"}` {
					t.Fatalf("expected traffic warmup, got %s, %v", result, err)
				}
			} else if !errors.Is(err, ErrRuntimeUnavailable) {
				t.Fatalf("expected unavailable, got %s, %v", result, err)
			}
			if checkedPods != tc.wantPods {
				t.Fatalf("checked new pods = %v, want %v", checkedPods, tc.wantPods)
			}
		})
	}
}

func TestPgcatApplicationLogsRequireContinuousSuccessfulPodEvents(t *testing.T) {
	since := time.Now().UTC().Add(-time.Minute)
	line := func(kind, pod, detail string, sequence int) string {
		return fmt.Sprintf(`{"kind":%q,"podUid":%q,"processId":"proc-1","at":%q,"sequence":%d%s}`+"\n",
			kind, pod, since.Add(time.Duration(sequence)*time.Second).Format(time.RFC3339Nano), sequence, detail)
	}
	const pod = "pod-1"
	start := line("traffic_startup", pod, "", 0)
	connected := line("connected", pod, "", 1)
	success := line("operation", pod, `,"outcome":"success"`, 2)
	final := line("traffic_final", pod, `,"snapshot":{"final":true,"connected":0}`, 2)
	for _, test := range []struct {
		name   string
		log    string
		follow bool
		valid  bool
	}{
		{"rolling pod", start + connected + success, false, true},
		{"old pod shutdown", start + connected + success + final, true, true},
		{"missing final", start + connected + success, true, false},
		{"foreign pod", start + connected + line("operation", "foreign", `,"outcome":"success"`, 2), false, false},
		{"dropped event", start + line("operation", pod, `,"outcome":"success"`, 2), false, false},
		{"failed SQL", start + connected + line("operation", pod, `,"outcome":"timeout"`, 2), false, false},
		{"skipped SQL", start + connected + success + line("skipped", pod, "", 3), false, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			state := newPgcatLogState(pod)
			state.readLogs(strings.NewReader(test.log), since, test.follow)
			if got := state.check(test.follow, true, since.Add(time.Second)); got != test.valid {
				t.Fatalf("log valid = %v, want %v", got, test.valid)
			}
		})
	}
}

func TestPgcatAppRouteOnlyCountsSuccessfulTransactionsInObservationWindow(t *testing.T) {
	now := time.Now().UTC()
	state := newPgcatLogState("pod-1")
	state.routes = map[netip.Addr]int64{}
	state.routeFrom = now.Add(-2 * time.Second)
	route := netip.MustParseAddr("10.70.1.225")
	event := func(at time.Time, seq int, ip string) string {
		return fmt.Sprintf(`{"kind":"operation","outcome":"success","podUid":"pod-1","processId":"proc-1","at":%q,"sequence":%d,"proxyIp":%q}`+"\n", at.Format(time.RFC3339Nano), seq, ip)
	}
	state.readLogs(strings.NewReader(event(now.Add(-3*time.Second), 5, route.String())+event(now.Add(-time.Second), 6, route.String())), now.Add(-time.Minute), false)
	if !state.check(false, false, now.Add(-2*time.Second)) || state.routes[route] != 1 {
		t.Fatalf("counted an operation outside the window: %+v", state.routes)
	}
	state.readLogs(strings.NewReader(event(now, 7, "")), now.Add(-time.Minute), false)
	if state.check(false, false, now.Add(-2*time.Second)) {
		t.Fatal("an unattributed operation was accepted")
	}
}

func TestPgcatApplicationCounterUsesOnlyBoundPoolAndUser(t *testing.T) {
	const queries = "pgcat_stats_total_query_count"
	const transactions = "pgcat_stats_total_xact_count"
	const app = `{database="test",host="postgres.test",index="0",pool="test",role="primary",shard="0",username="poc_app"}`
	const probe = `{pool="test",username="probe"}`
	counts := queries + app + " 17\n" + transactions + app + " 9\n"
	for _, test := range []struct {
		name  string
		body  string
		valid bool
	}{
		{"counts with unrelated probes", counts + queries + probe + " 100\n", true},
		{"probe only", queries + probe + " 100\n" + transactions + probe + " 100\n", false},
		{"missing transactions", queries + app + " 17\n", false},
		{"duplicate series", counts + transactions + app + " 9\n", false},
		{"negative counter", queries + app + " -1\n" + transactions + app + " 9\n", false},
		{"oversized response", counts + strings.Repeat("#\n", 140000), false},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := parsePgcatApplicationCounter(strings.NewReader(test.body))
			if (err == nil) != test.valid || test.valid && got != (pgcatQueryCount{Queries: 17, Transactions: 9}) {
				t.Fatalf("counter = %+v, err = %v", got, err)
			}
		})
	}
}
