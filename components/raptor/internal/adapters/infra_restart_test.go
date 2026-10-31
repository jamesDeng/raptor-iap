package adapters

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/domain"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHTTPRestartReceiptAndAmbiguousOutcomes(t *testing.T) {
	target := domain.RestartTarget{AppCode: "app", EnvCode: "dev", ClusterID: "cluster", Namespace: "ns", Name: "frontend", UID: "uid"}
	for _, tc := range []struct {
		name    string
		status  int
		changed bool
		want    error
	}{{"accepted", 200, false, nil}, {"refused", 403, false, ErrRejected}, {"lost acknowledgement", 503, false, ErrSubmissionUnknown}, {"foreign receipt", 200, true, ErrSubmissionUnknown}} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == "GET" {
					if r.URL.Path != "/v1/deployments" {
						t.Error("unexpected read")
					}
					w.Write([]byte(`{"data":{"resource":{"envCode":"dev","kind":"application","code":"app"},"applicationWorkloads":[{"type":"kubernetes","workloadKey":"key","clusterId":"cluster","namespace":"ns","name":"frontend","uid":"uid"}]}}`))
					return
				}
				calls++
				u, p, ok := r.BasicAuth()
				if !ok || u != "service" || p != "fixture" || r.Method != "POST" || r.URL.Path != "/v1/deployment-restart" {
					t.Error("wrong authenticated route")
				}
				var input map[string]any
				if json.NewDecoder(r.Body).Decode(&input) != nil || input["requestId"] != "request" || input["workloadKey"] != "key" || len(input) != 4 {
					t.Error("wrong selectors")
				}
				w.WriteHeader(tc.status)
				input = map[string]any{"requestId": "request", "envCode": "dev", "appCode": "app", "clusterId": "cluster", "namespace": "ns", "name": "frontend", "uid": "uid"}
				if tc.changed {
					input["uid"] = "another"
				}
				json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"accepted": true, "target": input, "generation": 2, "evidenceMode": "live"}})
			}))
			defer s.Close()
			p := HTTPInfra{Username: "service", Password: "fixture", ResolveEnvironment: func(context.Context, string) (domain.Environment, error) {
				return domain.Environment{Code: "dev", Config: map[string]any{"infraApiUrl": s.URL, "clusterId": "cluster"}}, nil
			}}
			err := p.RestartDeployment(context.Background(), "request", target)
			if !errors.Is(err, tc.want) || calls != 1 {
				t.Fatalf("error=%v calls=%d", err, calls)
			}
		})
	}
}
