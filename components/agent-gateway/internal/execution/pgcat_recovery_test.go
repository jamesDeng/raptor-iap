package execution

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestReadPgcatPreDrainHold(t *testing.T) {
	binding := AttemptBinding{RequestID: "request"}
	scope := ReplacementScope{EnvCode: "rdev.ali", ProxyCode: "DP00001", ResolutionDigest: "scope"}
	now := time.Now()
	saved := json.RawMessage(`{"observationId":"persisted"}`)
	hold := PgcatObservationRecord{RequestID: binding.RequestID, ScopeDigest: scope.ResolutionDigest,
		Kind: "hold", Subject: "fleet", ObservedAt: now, Data: saved}
	expansion := map[string]any{"requestId": binding.RequestID, "scopeDigest": scope.ResolutionDigest, "envCode": scope.EnvCode,
		"state": "submitted", "providerRequestId": "provider:asa-receipt", "recordedAt": now,
		"capability": map[string]any{"capability": "db-proxy.scale-out", "target": map[string]string{"kind": "db-proxy", "code": scope.ProxyCode}}}
	claimedDrain := map[string]any{"requestId": binding.RequestID, "scopeDigest": scope.ResolutionDigest, "envCode": scope.EnvCode,
		"state": "claimed", "capability": map[string]string{"capability": "db-proxy.drain"}}
	for _, tc := range []struct {
		name         string
		actions      []map[string]any
		observations []PgcatObservationRecord
		want         string
		wantErr      bool
	}{
		{name: "exact pre-drain state", actions: []map[string]any{expansion}, observations: []PgcatObservationRecord{hold}, want: string(saved)},
		{name: "no expansion", observations: []PgcatObservationRecord{hold}},
		{name: "already claimed drain", actions: []map[string]any{expansion, claimedDrain}, observations: []PgcatObservationRecord{hold}, wantErr: true},
		{name: "missing original cohort", actions: []map[string]any{expansion}, wantErr: true},
		{name: "ambiguous expansion", actions: []map[string]any{expansion, expansion}, observations: []PgcatObservationRecord{hold}, wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet || r.URL.Path != "/v1/requests/request/pgcat-request-state" || r.URL.Query().Get("scopeDigest") != scope.ResolutionDigest {
					t.Error("unexpected request-scoped read")
					w.WriteHeader(http.StatusNotFound)
					return
				}
				json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"actions": tc.actions, "observations": tc.observations}})
			}))
			defer server.Close()
			client := HTTPRaptor{BaseURL: server.URL, Username: "service", Password: "test"}
			got, err := client.ReadPgcatPreDrainHold(t.Context(), binding, scope)
			if (err != nil) != tc.wantErr || string(got) != tc.want {
				t.Fatalf("hold = %s, error = %v", got, err)
			}
		})
	}
}
