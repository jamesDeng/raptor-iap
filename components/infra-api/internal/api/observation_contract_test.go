package api

import (
	"context"
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"raptor-iap/infra-api/internal/domain"
	"testing"
	"time"
)

type contractReader struct{ observedReader }

func (f *contractReader) Deployments(context.Context, domain.Environment, string, string) ([]domain.Deployment, error) {
	return []domain.Deployment{{EnvCode: "dev", ObjectCode: "app", ClusterID: "cluster", Namespace: "ns", Name: "gateway", UID: "uid", EvidenceMode: "live", State: "running"}}, f.err
}
func TestReadObservationEnvelope(t *testing.T) {
	o := operations{reader: &contractReader{}, scopes: map[string]domain.Environment{"dev": {Code: "dev", AccountID: "123", ClusterID: "cluster"}}}
	for name, q := range map[string]string{"identity": "envCode=dev", "deployments": "envCode=dev&kind=application&code=app", "status": "envCode=dev&appCode=app&clusterId=cluster&namespace=ns&name=gateway&uid=uid"} {
		t.Run(name, func(t *testing.T) {
			routes := map[string]string{"identity": "/v1/cloud/identity", "deployments": "/v1/deployments", "status": "/v1/deployment-status"}
			args, _ := url.ParseQuery(q)
			v, e := o.execute(context.Background(), routes[name], args)
			if e != nil {
				t.Fatal(e)
			}
			raw, _ := json.Marshal(v)
			var row map[string]any
			if name == "deployments" {
				var rows []map[string]any
				json.Unmarshal(raw, &rows)
				row = rows[0]
			} else {
				json.Unmarshal(raw, &row)
			}
			stamp, _ := row["observedAt"].(string)
			when, e := time.Parse(time.RFC3339Nano, stamp)
			if e != nil || time.Since(when) > time.Second {
				t.Fatalf("missing collection time: %s", raw)
			}
			if name == "status" {
				for k, want := range map[string]string{"envCode": "dev", "objectCode": "app", "clusterId": "cluster", "namespace": "ns", "name": "gateway", "uid": "uid"} {
					if row[k] != want {
						t.Fatalf("missing checked %s: %s", k, raw)
					}
				}
			}
			if dir := os.Getenv("CONTRACT_FIXTURE_DIR"); dir != "" {
				if e = os.WriteFile(filepath.Join(dir, "infra-"+name+".json"), append(raw, '\n'), 0644); e != nil {
					t.Fatal(e)
				}
			}
		})
	}
}

// Current resource wire fixtures are generated separately; legacy fixtures above
// remain readable historical contracts and never authorize current execution.
func TestResourceReadObservationContract(t *testing.T) {
	f := &resourceFixture{rows: resourceRows()}
	f.rows.Environment.ClusterID = "cluster"
	w := &f.rows.ApplicationWorkloads[0]
	w.ClusterID = "cluster"
	w.Namespace = "ns"
	w.Name = "gateway"
	w.WorkloadKey = "opaque-key"
	w.Target = domain.Target{EnvCode: "dev", AppCode: "app", ClusterID: "cluster", Namespace: "ns", Name: "gateway", UID: "uid"}
	op := resourceOperations{reader: &resourceContractReader{}, resolver: f, principal: "user"}
	for name, query := range map[string]string{"deployments": "envCode=dev&kind=application&code=app", "status": "envCode=dev&appCode=app&workloadKey=opaque-key"} {
		args, _ := url.ParseQuery(query)
		route := "/v1/deployments"
		if name == "status" {
			route = "/v1/deployment-status"
		}
		data, e := op.execute(context.Background(), route, args)
		if e != nil {
			t.Fatal(e)
		}
		raw, _ := json.Marshal(data)
		var body map[string]any
		json.Unmarshal(raw, &body)
		if body["resource"].(map[string]any)["code"] != "app" {
			t.Fatal("wrong resource root")
		}
		if name == "deployments" && body["observedAt"] != nil {
			t.Fatal("discovery fabricated freshness")
		}
		if dir := os.Getenv("CONTRACT_FIXTURE_DIR"); dir != "" {
			if e = os.WriteFile(filepath.Join(dir, "infra-resource-"+name+".json"), append(raw, '\n'), 0644); e != nil {
				t.Fatal(e)
			}
		}
	}
}

type resourceContractReader struct{ observedReader }

func (r *resourceContractReader) Status(_ context.Context, _ domain.Environment, t domain.Target) (domain.Status, error) {
	return domain.Status{UID: t.UID, ObservedAt: time.Date(2026, 10, 31, 0, 0, 0, 0, time.UTC), EvidenceMode: "live", Generation: 1, ObservedGeneration: 1, Replicas: 1, UpdatedReplicas: 1, ReadyReplicas: 1}, nil
}
