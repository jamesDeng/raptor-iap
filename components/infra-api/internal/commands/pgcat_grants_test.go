package commands

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"raptor-iap/infra-api/internal/approval"
	"raptor-iap/infra-api/internal/domain"
	"raptor-iap/infra-api/internal/policy"
)

type grantedBackendFixture struct{ backendFixture }

func (b *grantedBackendFixture) ProtectWithReceipt(context.Context, domain.Environment, string, []string, bool) (string, error) {
	b.calls++
	return "protect-ack", nil
}

func (b *grantedBackendFixture) DrainWithReceipt(context.Context, domain.Environment, string, []string) (string, error) {
	b.calls++
	return "drain-ack", nil
}

func (b *grantedBackendFixture) RetirePgcat(context.Context, domain.Environment, domain.PgcatRetirement) (domain.PgcatRetirementReceipt, error) {
	b.calls++
	return domain.PgcatRetirementReceipt{ProviderRequestID: "retire-ack", ScalingActivityID: "retire-activity"}, nil
}

type pgcatMetricRows []policy.ClientObservation

func (rows pgcatMetricRows) ConnectedClients(context.Context, policy.ProxySnapshot) ([]policy.ClientObservation, error) {
	return rows, nil
}

type grantedRestarterFixture struct{ calls int }

func (r *grantedRestarterFixture) Restart(_ context.Context, _ domain.Environment, command domain.RestartCommand) (domain.RestartReceipt, error) {
	r.calls++
	return domain.RestartReceipt{Accepted: true, Target: command, Generation: 7}, nil
}

func TestGrantedPgcatPhasesRejectMissingOrMismatchedProofBeforeSubmitting(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	target := domain.ProxyTarget{RequestID: "req", EnvCode: "dev", ProxyCode: "p", GroupID: "g"}
	scope := "resolved"
	originals := []string{"old1", "old2"}
	added := []string{"new1", "new2"}
	hold := pgcatHoldWitness{RequestID: target.RequestID, ScopeDigest: scope, ObservationID: "hold-id", ObservedAt: now.Add(-time.Minute),
		Sockets: map[string]pgcatSocketWitness{
			"old1": {ID: "socket1", OpenedAt: now.Add(-time.Minute), LastSQLAt: now.Add(-time.Minute), Rounds: 1},
			"old2": {ID: "socket2", OpenedAt: now.Add(-time.Minute), LastSQLAt: now.Add(-time.Minute), Rounds: 1},
		}}
	drainAt := now.Add(-12 * time.Second)
	held := hold
	held.ObservedAt = now.Add(-10 * time.Second)
	held.Sockets = map[string]pgcatSocketWitness{}
	for id, socket := range hold.Sockets {
		socket.LastSQLAt = held.ObservedAt
		socket.Rounds++
		held.Sockets[id] = socket
	}
	drain := pgcatDrainWitness{pgcatHoldWitness: held, DrainActionID: "drain", DrainObservedAt: drainAt.Add(time.Second),
		FreshConnections: map[string]int{"new1": 12, "new2": 12}}
	app := domain.RestartCommand{RequestID: "req", EnvCode: "dev", AppCode: "app", UID: "uid"}
	expandAt := now.Add(-40 * time.Second)
	restartAt := now.Add(-8 * time.Second)
	traffic := map[string]any{"restartActionId": "restart", "applicationUid": "uid", "generation": 7, "noErrors": true,
		"nodes": map[string]any{"new1": map[string]int{"queries": 3, "transactions": 2}, "new2": map[string]int{"queries": 5, "transactions": 4}}}
	record := func(kind, subject string, at time.Time, value any) approval.PgcatObservation {
		raw, _ := json.Marshal(value)
		return approval.PgcatObservation{RequestID: target.RequestID, ScopeDigest: scope, Kind: kind, Subject: subject, ObservedAt: at, Data: raw}
	}
	action := func(capability, id, ack string, at time.Time) approval.GrantActionDetails {
		return approval.GrantActionDetails{RequestID: target.RequestID, EnvCode: target.EnvCode, ScopeDigest: scope,
			ActionID: id, State: "submitted", ProviderRequestID: ack, ClaimedAt: at.Add(-time.Second), RecordedAt: &at,
			Capability: approval.GrantCapability{Capability: capability}}
	}
	for _, test := range []struct {
		stage, fault string
		allow        bool
	}{
		{"drain", "", true}, {"drain", "missing new SQL", false}, {"drain", "wrong IDs", false}, {"drain", "metrics missing", false},
		{"restart", "", true}, {"restart", "different socket", false}, {"restart", "missing action", false}, {"restart", "fresh uses old", false}, {"restart", "probes still open", false},
		{"retire", "", true}, {"retire", "only probe traffic", false}, {"retire", "clients remain", false}, {"retire", "unrelated restart", false}, {"retire", "probes still open", false},
	} {
		t.Run(test.stage+"/"+test.fault, func(t *testing.T) {
			backend := &grantedBackendFixture{backendFixture{s: policy.ProxySnapshot{EnvCode: "dev", ProxyCode: "p", GroupID: "g", ServerGroupID: "sg", Desired: 4, Min: 2, Max: 4,
				Nodes: []policy.Node{{ID: "old1", Registered: true, Protected: true, Healthy: true}, {ID: "old2", Registered: true, Protected: true, Healthy: true},
					{ID: "new1", Registered: true, Protected: true, Healthy: true}, {ID: "new2", Registered: true, Protected: true, Healthy: true}}}}}
			metrics := pgcatMetricRows{}
			for _, id := range append(originals, added...) {
				metrics = append(metrics, policy.ClientObservation{InstanceID: id, ObservedAt: now})
			}
			observations := []approval.PgcatObservation{record("hold", "fleet", hold.ObservedAt, hold)}
			for _, id := range added {
				if test.fault == "missing new SQL" && id == "new2" {
					continue
				}
				observations = append(observations, record("sql", id, now, map[string]any{"instanceId": id, "connected": true}))
			}
			actions := []approval.GrantActionDetails{action("db-proxy.scale-out", "expand", "expand-ack", expandAt)}
			var operation any = domain.DeregisterCommand{ProxyTarget: target, ServerGroupID: "sg", InstanceIDs: originals}
			if test.fault == "wrong IDs" {
				operation = domain.DeregisterCommand{ProxyTarget: target, ServerGroupID: "sg", InstanceIDs: added}
			}
			if test.fault == "metrics missing" {
				metrics = metrics[:3]
			}
			if test.stage != "drain" {
				for i := 0; i < 2; i++ {
					backend.s.Nodes[i].Drained = true
					backend.s.Nodes[i].Healthy = false
				}
				operation = app
				if test.fault != "missing action" {
					actions = append(actions, action("db-proxy.drain", "drain", "drain-ack", drainAt))
				}
				evidence := drain
				if test.fault == "different socket" {
					evidence.Sockets = map[string]pgcatSocketWitness{"old1": held.Sockets["old1"], "old2": held.Sockets["old1"]}
				}
				if test.fault == "fresh uses old" {
					evidence.FreshConnections = map[string]int{"new1": 12, "old2": 12}
				}
				observations = append(observations, record("drain", "fleet", evidence.ObservedAt, evidence))
				if test.fault != "probes still open" {
					release := pgcatReleaseWitness{pgcatHoldWitness: evidence.pgcatHoldWitness, DrainActionID: "drain", ReleasedAt: evidence.ObservedAt}
					observations = append(observations, record("probe-release", "fleet", release.ReleasedAt, release))
				}
			}
			if test.stage == "retire" {
				operation = domain.ScaleCommand{ProxyTarget: target, ActionID: "retire", DesiredCapacity: 2}
				ack := "uid:7"
				if test.fault == "unrelated restart" {
					ack = "foreign:7"
				}
				actions = append(actions, action("application.restart", "restart", ack, restartAt))
				if test.fault == "clients remain" {
					metrics[0].Clients = 1
				}
				if test.fault != "only probe traffic" {
					observations = append(observations, record("traffic", "fleet", now, traffic))
				}
			}
			restarter := &grantedRestarterFixture{}
			runtime := Runtime{Backend: backend, Restarter: restarter, Metrics: metrics, Now: func() time.Time { return now }}
			prepared, err := runtime.PreparePgcatGrant(context.Background(), domain.Environment{Code: "dev"}, PgcatGrantInput{Target: target, ScopeDigest: scope, ActionID: "act", Operation: operation,
				State: approval.PgcatRequestState{Observations: observations, Actions: actions}})
			if (err == nil) != test.allow || backend.calls != 0 || restarter.calls != 0 {
				t.Fatalf("prepare gate: %+v %v, provider calls %d/%d", prepared.Capability, err, backend.calls, restarter.calls)
			}
			if test.allow {
				if _, ack, err := prepared.Submit(context.Background()); err != nil || ack == "" || backend.calls+restarter.calls != 1 {
					t.Fatalf("submit: %q %v, provider calls %d/%d", ack, err, backend.calls, restarter.calls)
				}
			}
		})
	}
}
