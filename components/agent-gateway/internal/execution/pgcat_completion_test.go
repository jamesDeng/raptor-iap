package execution

import (
	"encoding/json"
	"testing"
	"time"
)

func TestPgcatCompletionRequiresTrustedTrafficAndProviderActivity(t *testing.T) {
	now := time.Now().UTC()
	binding := AttemptBinding{RequestID: "req", EnvCode: "dev", ObjectCode: "proxy"}
	result := &ReplacementConvergence{
		Scope:        ReplacementScope{ResolutionDigest: "digest"},
		Traffic:      &PgcatResultTraffic{RestartActionID: "restart", NoErrors: true, ObservedAt: now.Add(-time.Minute)},
		FinalTraffic: &PgcatResultTraffic{RestartActionID: "restart", NoErrors: true, ObservedAt: now},
		Retirement:   &PgcatResultRetirement{ActionID: "retire", ScalingActivityID: "asa-one", Status: "Successful", DestroyedIDs: []string{"old1", "old2"}, ObservedAt: now.Add(-time.Second)},
	}
	for _, fault := range []string{"", "missing final", "wrong activity", "foreign traffic", "future retirement"} {
		t.Run(fault, func(t *testing.T) {
			state := pgcatCompletionState{}
			if err := json.Unmarshal([]byte(`{"actions":[{"requestId":"req","envCode":"dev","scopeDigest":"digest","actionId":"retire","state":"submitted","providerRequestId":"provider:asa-one","capability":{"capability":"db-proxy.scale-in","target":{"kind":"db-proxy","code":"proxy"}}}]}`), &state); err != nil {
				t.Fatal(err)
			}
			add := func(kind string, proof any) {
				raw, err := json.Marshal(proof)
				if err != nil {
					t.Fatal(err)
				}
				state.Observations = append(state.Observations, PgcatObservationRecord{RequestID: "req", ScopeDigest: "digest", Kind: kind, Subject: "fleet", Data: raw})
			}
			add("traffic", result.Traffic)
			if fault != "missing final" {
				final := *result.FinalTraffic
				if fault == "foreign traffic" {
					final.RestartActionID = "other"
				}
				add("final-traffic", final)
			}
			retirement := *result.Retirement
			if fault == "future retirement" {
				retirement.ObservedAt = now.Add(time.Second)
			}
			add("retirement", retirement)
			if fault == "wrong activity" {
				state.Actions[0].ProviderRequestID = "provider:asa-two"
			}
			if err := verifyPgcatCompletionState(binding, result, state); (err == nil) != (fault == "") {
				t.Fatalf("trusted completion check for %q: %v", fault, err)
			}
		})
	}
}
