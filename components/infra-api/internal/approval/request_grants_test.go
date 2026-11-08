package approval

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"raptor-iap/infra-api/internal/resources"
)

func TestRequestGrantReceiptCannotAuthorizeReplay(t *testing.T) {
	identity := resources.ResolutionIdentity{EnvCode: "dev", AccountID: "123", Region: "ap-southeast-1",
		ClusterID: "c", ProxyCode: "p", GroupID: "g", ServerGroupID: "sg", ListenerID: "l", Port: 5432,
		TargetDBCode: "db", DatabaseResourceID: "rds",
		Application: resources.ApplicationIdentity{EnvCode: "dev", AppCode: "app", ClusterID: "c", Namespace: "n", Name: "app", UID: "u"}}
	digest, err := resources.Digest(identity)
	if err != nil {
		t.Fatal(err)
	}
	proof := resources.Proof{Identity: identity, ResolutionDigest: digest}
	action := GrantAction{RequestID: "request", GrantID: "grant", ActionID: "operation", EnvCode: "dev", ScopeDigest: digest,
		Capability: GrantCapability{Capability: "db-proxy.scale-in", Target: GrantTarget{Kind: "db-proxy", Code: "p"},
			Constraints: json.RawMessage(`{"desiredCapacity":2}`)}, Arguments: json.RawMessage(`{"desiredCapacity":2,"remove":["old1","old2"]}`)}
	for _, test := range []struct {
		name, body string
		ok         bool
		admit      bool
	}{
		{"new", `{"data":{"claimed":true,"state":"claimed"}}`, true, true},
		{"replay during execution", `{"data":{"claimed":false,"state":"claimed"}}`, true, false},
		{"replay after failure", `{"data":{"claimed":false,"state":"unknown"}}`, true, false},
		{"replay after success", `{"data":{"claimed":false,"state":"submitted","providerRequestId":"ack"}}`, true, false},
		{"ambiguous", `{"data":{"state":"claimed"}}`, false, false},
		{"inconsistent", `{"data":{"claimed":true,"state":"unknown"}}`, false, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			client, err := New("https://raptor.test", "service", "private", &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
				user, password, ok := r.BasicAuth()
				if !ok || user != "service" || password != "private" || r.URL.Path != "/v1/infra/request-grant-claim" {
					t.Fatal("grant call lost its service identity or route")
				}
				var payload struct {
					Action GrantAction
					Proof  resources.Proof
				}
				if json.NewDecoder(r.Body).Decode(&payload) != nil || payload.Proof != proof ||
					payload.Action.ActionID != action.ActionID || string(payload.Action.Arguments) != string(action.Arguments) {
					t.Fatal("grant claim lost exact proof or arguments")
				}
				return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(test.body)), Header: http.Header{}}, nil
			})})
			if err != nil {
				t.Fatal(err)
			}
			receipt, err := client.ClaimGrant(context.Background(), action, proof)
			if (err == nil) != test.ok || receipt.Claimed != test.admit {
				t.Fatalf("claim receipt = %+v, %v", receipt, err)
			}
		})
	}
}
