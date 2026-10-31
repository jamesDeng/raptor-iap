package backend

import (
	"context"
	"encoding/json"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/agentaccess"
	"net/http/httptest"
	"strings"
	"testing"
)

// Missing final lifecycle/catalog/pin checks would authorize changed state here.
func TestResourceInfraPrivateHandoff(t *testing.T) {
	for _, change := range []string{"none", "listener", "database", "uid", "revoked", "expired", "control", "fingerprint", "catalog", "operation", "missing", "legacy", "digest"} {
		t.Run(change, func(t *testing.T) {
			h, r, f, token := resourceRuntimeFixture(t)
			op := agentaccess.CheckInput{Credential: token, Audience: "infra", Tool: "deployments_list", Selectors: map[string]string{"envCode": "dev", "kind": "database", "code": f.proof.Identity.TargetDBCode}}
			f.proofs = 0
			prepared := accessCall(h, "POST", "/v1/agent-access/prepare-infra", "fixture-inspector", "fixture-inspector-password", op)
			var p struct {
				Data struct {
					Eligible bool
					Prepared string
				}
			}
			if json.Unmarshal(prepared.Body.Bytes(), &p) != nil || prepared.Code != 200 || !p.Data.Eligible || p.Data.Prepared == "" {
				t.Fatalf("prepare: %d %s", prepared.Code, prepared.Body.String())
			}
			if f.proofs != 0 {
				t.Fatal("prepare fetched nested provider proof")
			}
			proof := f.proof
			switch change {
			case "listener":
				proof.Identity.ListenerID = "changed"
			case "database":
				proof.Identity.DatabaseResourceID = "changed"
			case "uid":
				proof.Identity.Application.UID = "changed"
			case "revoked":
				_, _ = h.Requests.Pool.Exec(context.Background(), "UPDATE raptor.agent_tokens SET revoked=true WHERE request_id=$1", r.ID)
			case "expired":
				_, _ = h.Requests.Pool.Exec(context.Background(), "UPDATE raptor.agent_tokens SET expires_at=now()-interval '1 second' WHERE request_id=$1", r.ID)
			case "control":
				_, _ = h.Requests.Pool.Exec(context.Background(), "UPDATE raptor.requests SET control_state='paused' WHERE id=$1", r.ID)
			case "fingerprint":
				_, _ = h.Requests.Pool.Exec(context.Background(), `UPDATE raptor.requests SET definition=jsonb_set(definition,'{model}','"changed"') WHERE id=$1`, r.ID)
			case "catalog":
				_, _ = h.Requests.Pool.Exec(context.Background(), "UPDATE raptor.environments SET account_id='changed' WHERE code='dev'")
			case "operation":
				op.Tool = "deployments_list"
				op.Selectors["kind"] = "db-proxy"
				op.Selectors["code"] = r.Definition.Object.Code
			}
			proof.ResolutionDigest = proof.Identity.Digest()
			if change == "legacy" {
				proof.ResolutionDigest = ""
			}
			if change == "digest" {
				proof.ResolutionDigest = strings.Repeat("0", 64)
			}
			in := map[string]any{"operation": op, "prepared": p.Data.Prepared, "proof": proof}
			if change == "missing" {
				delete(in, "proof")
			}
			w := accessCall(h, "POST", "/v1/agent-access/validate-infra", "fixture-inspector", "fixture-inspector-password", in)
			var out struct{ Data agentaccess.CheckResult }
			_ = json.Unmarshal(w.Body.Bytes(), &out)
			if (change == "none") != (w.Code == 200 && out.Data.Active) {
				t.Fatalf("validation %s: %d %s", change, w.Code, w.Body.String())
			}
			if f.proofs != 0 {
				t.Fatal("handoff re-entered Infra", f.proofs)
			}
		})
	}
}
func TestResourceInfraPrivateHandoffTrustAndStrictJSON(t *testing.T) {
	h, _, f, token := resourceRuntimeFixture(t)
	op := agentaccess.CheckInput{Credential: token, Audience: "infra", Tool: "deployments_list", Selectors: map[string]string{"envCode": "dev", "kind": "db-proxy", "code": f.proof.Identity.ProxyCode}}
	for _, who := range []string{"agent", "fixture-controller"} {
		if w := accessCall(h, "POST", "/v1/agent-access/prepare-infra", who, token, op); w.Code != 401 {
			t.Fatal("untrusted prepare", w.Code)
		}
	}
	raw, _ := json.Marshal(op)
	for _, body := range []string{strings.Replace(string(raw), `"audience":"infra"`, `"Audience":"infra"`, 1), strings.Replace(string(raw), `"audience":"infra"`, `"audience":"infra","audience":"infra"`, 1), strings.Replace(string(raw), `"audience":"infra"`, `"audience":null`, 1)} {
		req := httptest.NewRequest("POST", "/v1/agent-access/prepare-infra", strings.NewReader(body))
		req.SetBasicAuth("fixture-inspector", "fixture-inspector-password")
		w := httptest.NewRecorder()
		h.Mux.ServeHTTP(w, req)
		if w.Code != 400 {
			t.Fatalf("malformed prepare accepted: %d %s", w.Code, body)
		}
	}
}
