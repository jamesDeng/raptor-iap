package runtime

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/jamesDeng/raptor-iap/components/agent-gateway/internal/execution"
)

func TestPreExpansionHoldRequiresReadOnlyRequestJournal(t *testing.T) {
	b := execution.AttemptBinding{RequestID: "request"}
	scope := execution.ReplacementScope{ResolutionDigest: "scope"}
	hold := execution.PgcatObservationRecord{RequestID: b.RequestID, ScopeDigest: scope.ResolutionDigest, Kind: "hold", Subject: "fleet", ObservedAt: time.Now(), Data: json.RawMessage(`{"observationId":"original"}`)}
	state := execution.PgcatRequestJournal{Observations: []execution.PgcatObservationRecord{hold}}
	if data, err := preExpansionHold(state, b, scope); err != nil || string(data) != string(hold.Data) {
		t.Fatalf("did not preserve the saved hold: %v", err)
	}
	state.Actions = []execution.PgcatJournalAction{{State: "unknown"}}
	if _, err := preExpansionHold(state, b, scope); err == nil {
		t.Fatal("a potentially submitted write was accepted")
	}
	state.Actions = nil
	state.Observations[0].Kind = "drain"
	if _, err := preExpansionHold(state, b, scope); err == nil {
		t.Fatal("post-drain journal was accepted")
	}
}
