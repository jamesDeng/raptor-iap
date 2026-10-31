package catalog

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"github.com/jamesDeng/raptor-iap/components/raptor/internal/domain"
)

func resourceContextFixture(t *testing.T) (*Service, *fixtureInfra) {
	t.Helper()
	s, infra := setup(t)
	ctx := context.Background()
	if err := s.CreateGroup(ctx, "fixture", "Fixture"); err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"application", "database", "db-proxy"} {
		if _, err := s.CreateObject(ctx, CreateObjectInput{Kind: kind, Name: kind}); err != nil {
			t.Fatal(err)
		}
	}
	env := domain.Environment{Code: "rdev.ali", GroupCode: "fixture", Stage: "dev", Cloud: "aliyun", Region: "ap-southeast-1", AccountID: "fixture-account", ACKClusterID: "fixture-cluster", Config: map[string]any{
		"clusterId": "fixture-cluster", "infraApiUrl": "https://private.example", "privateSetting": "do-not-expose", "proxyApplications": map[string]any{"DP00001": []any{"A00001"}},
	}}
	if err := s.SaveEnvironment(ctx, env, false); err != nil {
		t.Fatal(err)
	}
	return s, infra
}

// Catches accidental deployment discovery and leaking arbitrary environment fields.
func TestInfraResourceContextCatalogOnly(t *testing.T) {
	s, infra := resourceContextFixture(t)
	got, err := s.InfraResourceContext(context.Background(), "rdev.ali", "db-proxy", "DP00001")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]any
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatal(err)
	}
	want := map[string]any{"envCode": "rdev.ali", "kind": "db-proxy", "code": "DP00001", "cloud": "aliyun", "region": "ap-southeast-1", "accountId": "fixture-account", "clusterId": "fixture-cluster", "relatedResources": []any{map[string]any{"kind": "application", "code": "A00001"}}}
	if !reflect.DeepEqual(fields, want) {
		t.Fatalf("unsafe or incorrect context: %s", raw)
	}
	if infra.calls != 0 {
		t.Fatalf("context contacted Infra %d times", infra.calls)
	}
	for _, tc := range []struct{ kind, code string }{{"application", "A00001"}, {"database", "D00001"}} {
		got, err := s.InfraResourceContext(context.Background(), "rdev.ali", tc.kind, tc.code)
		if err != nil || got.RelatedResources == nil || len(got.RelatedResources) != 0 {
			t.Fatalf("ordinary resource dependencies: %+v %v", got, err)
		}
	}
}

func TestInfraResourceContextCanonicalResource(t *testing.T) {
	s, _ := resourceContextFixture(t)
	ctx := context.Background()
	if _, err := s.Pool.Exec(ctx, "INSERT INTO raptor.object_code_aliases(code,object_id,kind,canonical_code) SELECT 'old-proxy',id,kind,code FROM raptor.objects WHERE code='DP00001'"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Pool.Exec(ctx, "INSERT INTO raptor.object_code_aliases(code,object_id,kind,canonical_code) SELECT 'old-app',id,kind,code FROM raptor.objects WHERE code='A00001'"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Pool.Exec(ctx, `UPDATE raptor.environment_settings SET value_json='{"DP00001":["old-app"],"DP00002":null}' WHERE key='proxyApplications'`); err != nil {
		t.Fatal(err)
	}
	got, err := s.InfraResourceContext(ctx, "rdev.ali", "db-proxy", "old-proxy")
	if err != nil || got.Code != "DP00001" || !reflect.DeepEqual(got.RelatedResources, []domain.ObjectRef{{Kind: "application", Code: "A00001"}}) {
		t.Fatalf("canonical proxy and dependency: %+v %v", got, err)
	}
}

func TestInfraResourceContextForeignOrMissingResource(t *testing.T) {
	s, infra := resourceContextFixture(t)
	for _, tc := range []struct {
		env, kind, code string
		want            error
	}{
		{"foreign", "application", "A00001", domain.ErrNotFound},
		{"rdev.ali", "application", "D00001", domain.ErrNotFound},
		{"rdev.ali", "database", "missing", domain.ErrNotFound},
		{"rdev.ali", "ess", "DP00001", domain.ErrInvalid},
		{"", "application", "A00001", domain.ErrInvalid},
		{"rdev.ali", "application", "https://foreign.example", domain.ErrInvalid},
	} {
		_, err := s.InfraResourceContext(context.Background(), tc.env, tc.kind, tc.code)
		if !errors.Is(err, tc.want) {
			t.Errorf("%+v: got %v", tc, err)
		}
	}
	if infra.calls != 0 {
		t.Fatal("failed lookup contacted Infra")
	}
}

func TestInfraResourceContextInvalidProxyApplications(t *testing.T) {
	s, _ := resourceContextFixture(t)
	ctx := context.Background()
	for _, raw := range []string{
		`null`, `[]`, `"DP00001"`, `{"DP00001":"A00001"}`, `{"DP00001":null}`, `{"DP00001":[]}`, `{"DP00001":["A00001","A00001"]}`, `{"DP00001":[1]}`, `{"DP00001":[""]}`, `{"DP00001":["D00001"]}`, `{"DP00001":["A99999"]}`, `{"DP00001":["asg-cloud-id"]}`, `{"DP00001":[{"code":"A00001"}]}`,
	} {
		t.Run(raw, func(t *testing.T) {
			if _, err := s.Pool.Exec(ctx, "UPDATE raptor.environment_settings SET value_json=$1 WHERE environment_code='rdev.ali' AND key='proxyApplications'", raw); err != nil {
				t.Fatal(err)
			}
			if _, err := s.InfraResourceContext(ctx, "rdev.ali", "db-proxy", "DP00001"); err == nil {
				t.Fatalf("invalid mapping accepted: %s", raw)
			}
		})
	}
	for _, raw := range []string{`{}`, `{"DP00002":["A00001"]}`} {
		if _, err := s.Pool.Exec(ctx, "UPDATE raptor.environment_settings SET value_json=$1 WHERE environment_code='rdev.ali' AND key='proxyApplications'", raw); err != nil {
			t.Fatal(err)
		}
		got, err := s.InfraResourceContext(ctx, "rdev.ali", "db-proxy", "DP00001")
		if err != nil || got.RelatedResources == nil || len(got.RelatedResources) != 0 {
			t.Fatalf("unconfigured dependency read: %+v %v", got, err)
		}
	}
	if _, err := s.Pool.Exec(ctx, "DELETE FROM raptor.environment_settings WHERE key='proxyApplications'"); err != nil {
		t.Fatal(err)
	}
	got, err := s.InfraResourceContext(ctx, "rdev.ali", "db-proxy", "DP00001")
	if err != nil || got.RelatedResources == nil || len(got.RelatedResources) != 0 {
		t.Fatalf("missing dependency read: %+v %v", got, err)
	}
}

func TestInfraResourceContextClusterConflict(t *testing.T) {
	s, _ := resourceContextFixture(t)
	ctx := context.Background()
	for _, tc := range []struct {
		ack, legacy, want string
		conflict          bool
	}{
		{"cluster-a", "cluster-b", "", true}, {"cluster-a", "cluster-a", "cluster-a", false}, {"cluster-a", "", "cluster-a", false}, {"", "cluster-b", "cluster-b", false}, {"", "", "", false},
	} {
		if _, err := s.Pool.Exec(ctx, "UPDATE raptor.environments SET ack_cluster_id=$1,cluster_id=$2 WHERE code='rdev.ali'", tc.ack, tc.legacy); err != nil {
			t.Fatal(err)
		}
		got, err := s.InfraResourceContext(ctx, "rdev.ali", "application", "A00001")
		if tc.conflict {
			if !errors.Is(err, domain.ErrConflict) {
				t.Fatalf("conflicting cluster accepted: %+v %v", got, err)
			}
			continue
		}
		if err != nil || got.ClusterID != tc.want {
			t.Fatalf("cluster normalization: %+v %v", got, err)
		}
	}
}

// Existing catalog validation rejects stored credential keys before any projection.
func TestInfraResourceContextRejectsCredentialBearingSettings(t *testing.T) {
	s, infra := resourceContextFixture(t)
	if _, err := s.Pool.Exec(context.Background(), `INSERT INTO raptor.environment_settings(environment_code,key,value_json) VALUES('rdev.ali','credentials','{"token":"fixture-secret"}')`); err != nil {
		t.Fatal(err)
	}
	got, err := s.InfraResourceContext(context.Background(), "rdev.ali", "application", "A00001")
	if !errors.Is(err, domain.ErrInvalid) || got.Code != "" {
		t.Fatalf("credential-bearing environment accepted: %+v %v", got, err)
	}
	if infra.calls != 0 {
		t.Fatal("invalid environment contacted Infra")
	}
}
