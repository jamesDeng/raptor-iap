package adapters

import (
	"context"
	"encoding/json"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/domain"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestResourceHTTPDeploymentEnvelope(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"resource": map[string]string{"envCode": "dev", "kind": "application", "code": "app"}, "applicationWorkloads": []any{map[string]string{"type": "kubernetes", "workloadKey": "key", "clusterId": "c", "namespace": "n", "name": "app", "uid": "u", "state": "Running"}}}})
	}))
	defer s.Close()
	p := HTTPInfra{Username: "u", Password: "p"}
	rows, err := p.ListDeployments(context.Background(), domain.Environment{Code: "dev", ACKClusterID: "c", Config: map[string]any{"infraApiUrl": s.URL}}, domain.Object{Kind: "application", Code: "app"})
	if err != nil || len(rows) != 1 || rows[0].UID != "u" || rows[0].WorkloadKey != "key" {
		t.Fatalf("rows=%+v err=%v", rows, err)
	}
}

func TestResourceProofUsesRoutedServiceRepresentation(t *testing.T) {
	i := ResolutionIdentity{EnvCode: "dev", AccountID: "123", Region: "ap-southeast-1", ClusterID: "c", ProxyCode: "p", GroupID: "g", ServerGroupID: "sg", ListenerID: "l", Port: 5432, TargetDBCode: "db", DatabaseResourceID: "rds", Application: ApplicationIdentity{EnvCode: "dev", AppCode: "app", ClusterID: "c", Namespace: "n", Name: "app", UID: "u"}}
	proof := ResolutionProof{Identity: i, ResolutionDigest: i.Digest()}
	status := 200
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		q := r.URL.Query()
		user, password, ok := r.BasicAuth()
		if !ok || user != "u" || password != "p" || r.URL.Path != "/v1/deployments" || r.Header.Get("Accept") != "application/vnd.raptor.resolution-proof+json" || len(q) != 3 || q.Get("envCode") != "dev" || q.Get("kind") != "db-proxy" || q.Get("code") != "p" {
			t.Error("proof not sent through exact existing route", r.URL, r.Header.Get("Accept"))
		}
		w.WriteHeader(status)
		json.NewEncoder(w).Encode(map[string]any{"data": proof})
	}))
	defer server.Close()
	client := HTTPInfra{Username: "u", Password: "p", ResolveEnvironment: func(context.Context, string) (domain.Environment, error) {
		return domain.Environment{Code: "dev", AccountID: "123", Region: "ap-southeast-1", ACKClusterID: "c", Config: map[string]any{"infraApiUrl": server.URL}}, nil
	}}
	if got, e := client.ResolutionProof(context.Background(), "dev", "p"); e != nil || got != proof {
		t.Fatal(got, e)
	}
	proof.Identity.Application.UID = "changed"
	if _, e := client.ResolutionProof(context.Background(), "dev", "p"); e == nil {
		t.Fatal("changed tuple with old digest accepted")
	}
	proof.Identity = i
	proof.ResolutionDigest = ""
	if _, e := client.ResolutionProof(context.Background(), "dev", "p"); e == nil {
		t.Fatal("missing proof accepted")
	}
	proof.ResolutionDigest = i.Digest()
	status = 409
	if _, e := client.ResolutionProof(context.Background(), "dev", "p"); e == nil {
		t.Fatal("ambiguous workload refusal became proof")
	}
	if calls != 4 {
		t.Fatal("proof retry", calls)
	}
}

func TestResourceProxyWorkloadKeyIsNeverProviderIdentity(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"data":{"resource":{"envCode":"dev","kind":"db-proxy","code":"p"},"pgproxyWorkload":{"type":"ess","code":"p","workloadKey":"opaque","targetDbCode":"db"}}}`))
	}))
	defer s.Close()
	p := HTTPInfra{Username: "u", Password: "p"}
	rows, e := p.ListDeployments(context.Background(), domain.Environment{Code: "dev", Config: map[string]any{"infraApiUrl": s.URL}}, domain.Object{Kind: "db-proxy", Code: "p"})
	if e != nil || len(rows) != 1 || rows[0].WorkloadKey != "opaque" || rows[0].ResourceID != "" {
		t.Fatal(rows, e)
	}
}
