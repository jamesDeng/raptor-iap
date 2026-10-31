package api

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"raptor-iap/infra-api/internal/resources"
	"strings"
	"testing"
	"time"
)

// A final response must not replace the prepared binding/identity with another
// individually valid one. Neither preparation nor proof alone is authority.
func TestResourceInspectorFinalCannotChangePreparedIdentity(t *testing.T) {
	for _, change := range []string{"none", "listener", "database", "uid", "attempt", "fingerprint", "expiry", "legacy", "denied", "unsupported", "ineligible"} {
		t.Run(change, func(t *testing.T) {
			prepared, proof := replacementResourceIdentity()
			data, _ := json.Marshal(prepared)
			var final AgentIdentity
			_ = json.Unmarshal(data, &final)
			switch change {
			case "listener":
				final.ReplacementScope.ListenerID = "changed"
			case "database":
				final.ReplacementScope.DatabaseResourceID = "changed"
			case "uid":
				final.ReplacementScope.Application.UID = "changed"
			case "attempt":
				final.Binding.AttemptID = "changed"
			case "fingerprint":
				final.Binding.DefinitionSHA256 = strings.Repeat("a", 64)
			case "expiry":
				expires := time.Now().Add(2 * time.Hour)
				final.ExpiresAt = &expires
			case "legacy":
				final.ReplacementScope.ResolutionDigest = ""
			case "denied":
				final.Active = false
			}
			calls, proofs := 0, 0
			client := &http.Client{Transport: resourceRoundTripper(func(r *http.Request) (*http.Response, error) {
				calls++
				status := 200
				var out any
				if r.URL.Path == "/v1/agent-access/prepare-infra" {
					out = map[string]any{"eligible": change != "ineligible", "prepared": strings.Repeat("a", 64), "identity": prepared}
					if change == "unsupported" {
						status = 404
					}
				} else if r.URL.Path == "/v1/agent-access/validate-infra" {
					out = final
				} else {
					t.Fatal("recursive fallback", r.URL.Path)
				}
				b, _ := json.Marshal(map[string]any{"data": out})
				return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(string(b))), Header: http.Header{}}, nil
			})}
			resolve := func(ctx context.Context, env, proxy string) (resources.Proof, error) {
				proofs++
				if calls != 1 || env != "dev" || proxy != "p" {
					t.Fatal("proof before eligibility or wrong selection")
				}
				if deadline, ok := ctx.Deadline(); !ok || time.Until(deadline) > 5*time.Second {
					t.Fatal("deadline relaxed")
				}
				return proof, nil
			}
			inspect, e := NewResourceAgentInspector("https://raptor.test", "svc", "private", client, resolve)
			if e != nil {
				t.Fatal(e)
			}
			out, e := inspect(context.Background(), "raptor_at_"+strings.Repeat("a", 43), "deployments_list", map[string]string{"envCode": "dev", "kind": "db-proxy", "code": "p"})
			allowed := e == nil && out.Active
			if allowed != (change == "none") {
				t.Fatalf("changed final accepted: %s active=%v err=%v", change, out.Active, e)
			}
			want := 1
			if change == "unsupported" || change == "ineligible" {
				want = 0
			}
			if proofs != want {
				t.Fatal("proof count", proofs, want)
			}
		})
	}
}
