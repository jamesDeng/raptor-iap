package runtime

import (
	"testing"
	"time"
)

func TestSavedProbeReleaseMustMatchVerifiedDrain(t *testing.T) {
	drained := time.Now().UTC()
	evidence := pgcatHoldEvidence{
		RequestID: "request", ScopeDigest: "scope", ObservationID: "hold", ObservedAt: drained,
		Sockets: map[string]pgcatSocketEvidence{
			"old-a": {ID: "socket-a", Rounds: 12, OpenedAt: drained.Add(-time.Minute), LastSQLAt: drained},
			"old-b": {ID: "socket-b", Rounds: 15, OpenedAt: drained.Add(-time.Minute), LastSQLAt: drained},
		},
	}
	proof := pgcatDrainEvidence{pgcatHoldEvidence: evidence, DrainActionID: "drain"}
	released := drained.Add(time.Minute)
	receipt := pgcatProbeRelease{pgcatHoldEvidence: evidence, DrainActionID: "drain", ReleasedAt: released, Recovery: "previous-controller-cleanup"}
	receipt.ObservedAt = released
	if !matchesSavedProbeRelease(receipt, proof) {
		t.Fatal("same two verified sockets and action rejected")
	}
	for _, change := range []func(*pgcatProbeRelease){
		func(r *pgcatProbeRelease) { r.ObservationID = "different" },
		func(r *pgcatProbeRelease) { r.DrainActionID = "different" },
		func(r *pgcatProbeRelease) { r.Recovery = "" },
		func(r *pgcatProbeRelease) { r.ReleasedAt = drained.Add(-time.Second) },
		func(r *pgcatProbeRelease) { r.Sockets = map[string]pgcatSocketEvidence{"foreign": {ID: "socket-a"}} },
	} {
		changed := receipt
		change(&changed)
		if matchesSavedProbeRelease(changed, proof) {
			t.Fatal("foreign or inconsistent receipt accepted")
		}
	}
}
