package approval

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

func TestResourceApprovalRequiresEligibleFreshSameOperationExchange(t *testing.T) {
	proof := resources.Proof{Identity: resources.ResolutionIdentity{EnvCode: "dev", AccountID: "123", Region: "ap-southeast-1", ClusterID: "c", ProxyCode: "p", GroupID: "g", ServerGroupID: "sg", ListenerID: "l", Port: 5432, TargetDBCode: "db", DatabaseResourceID: "rds", Application: resources.ApplicationIdentity{EnvCode: "dev", AppCode: "app", ClusterID: "c", Namespace: "n", Name: "app", UID: "u"}}}
	proof.ResolutionDigest, _ = resources.Digest(proof.Identity)
	for _, phase := range []string{"check", "claim"} {
		for _, mode := range []string{"ok", "ineligible", "unsupported", "proof-changed", "final-denied", "cancelled"} {
			t.Run(phase+"/"+mode, func(t *testing.T) {
				steps := []string{}
				proofs := 0
				client := &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
					user, password, ok := r.BasicAuth()
					if !ok || user != "service" || password != "private" {
						t.Error("caller trust changed")
					}
					var input map[string]json.RawMessage
					if json.NewDecoder(r.Body).Decode(&input) != nil {
						t.Fatal("invalid private request")
					}
					var operation string
					_ = json.Unmarshal(input["operation"], &operation)
					if operation != phase {
						t.Fatal("operation changed", operation)
					}
					var binding map[string]any
					_ = json.Unmarshal(input["binding"], &binding)
					if binding["actionId"] != "action" || binding["interface"] != "db-proxy.scale-in" {
						t.Fatal("approval binding changed", binding)
					}
					status := 200
					body := `{"data":{"eligible":true,"prepared":"` + strings.Repeat("a", 64) + `"}}`
					switch r.URL.Path {
					case "/v1/infra/approval-prepare":
						steps = append(steps, "prepare")
						if mode == "ineligible" {
							body = `{"data":{"eligible":false,"prepared":""}}`
						}
						if mode == "unsupported" {
							status = 404
						}
					case "/v1/infra/approval-validate":
						steps = append(steps, "validate")
						if proofs != 1 {
							t.Fatal("final without fresh local proof")
						}
						var supplied resources.Proof
						if json.Unmarshal(input["proof"], &supplied) != nil || supplied != proof {
							t.Fatal("final missing complete proof")
						}
						field := "allowed"
						if phase == "claim" {
							field = "claimed"
						}
						value := "true"
						if mode == "final-denied" {
							value = "false"
						}
						body = `{"data":{"` + field + `":` + value + `}}`
					default:
						t.Fatal("fell back to recursive approval route", r.URL.Path)
					}
					return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body)), Header: http.Header{}}, nil
				})}
				resolve := func(ctx context.Context, env, proxy string) (resources.Proof, error) {
					steps = append(steps, "proof")
					proofs++
					if env != "dev" || proxy != "p" {
						t.Fatal("foreign proof selection")
					}
					if deadline, ok := ctx.Deadline(); !ok || time.Until(deadline) > 5*time.Second {
						t.Fatal("deadline relaxed")
					}
					if mode == "cancelled" {
						<-ctx.Done()
						return resources.Proof{}, ctx.Err()
					}
					p := proof
					if mode == "proof-changed" {
						p.Identity.GroupID = "foreign"
						p.ResolutionDigest, _ = resources.Digest(p.Identity)
					}
					return p, nil
				}
				c, e := NewResource("https://raptor.test", "service", "private", client, resolve)
				if e != nil {
					t.Fatal(e)
				}
				ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
				defer cancel()
				binding := Binding{RequestID: "r", ActionID: "action", EnvCode: "dev", ProxyCode: "p", GroupID: "g", DesiredCapacity: 2}
				var allowed bool
				if phase == "check" {
					allowed, e = c.Check(ctx, binding)
				} else {
					allowed, e = c.Claim(ctx, binding)
				}
				if allowed != (mode == "ok") {
					t.Fatal(mode, allowed, e)
				}
				want := "prepare,proof,validate"
				if mode == "ineligible" || mode == "unsupported" {
					want = "prepare"
				}
				if mode == "proof-changed" || mode == "cancelled" {
					want = "prepare,proof"
				}
				if strings.Join(steps, ",") != want {
					t.Fatal(steps, want)
				}
			})
		}
	}
}
