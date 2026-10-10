package catalog

import (
	"context"
	"errors"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/adapters"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/db"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/domain"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/testutil"
	"testing"
)

type fixtureInfra struct {
	fail  bool
	calls int
}

func (f *fixtureInfra) ListDeployments(ctx context.Context, e domain.Environment, o domain.Object) ([]adapters.Deployment, error) {
	f.calls++
	if f.fail {
		return nil, errors.New("private upstream failure")
	}
	return []adapters.Deployment{}, nil
}
func setup(t *testing.T) (*Service, *fixtureInfra) {
	p := testutil.Database(t)
	if e := db.Migrate(context.Background(), p); e != nil {
		t.Fatal(e)
	}
	f := &fixtureInfra{}
	return NewService(p, f), f
}
func TestCatalogCreationDoesNotDeploy(t *testing.T) {
	s, f := setup(t)
	ctx := context.Background()
	a, e := s.CreateObject(ctx, CreateObjectInput{Kind: "database", Name: "Orders"})
	if e != nil {
		t.Fatal(e)
	}
	b, e := s.CreateObject(ctx, CreateObjectInput{Kind: "db-proxy", Name: "Pool"})
	if e != nil {
		t.Fatal(e)
	}
	if a.Code == "" || a.Code == b.Code || a.ID == b.ID {
		t.Fatal("codes not generated independently")
	}
	if f.calls != 0 {
		t.Fatal("catalog creation contacted infrastructure")
	}
	got, e := s.GetObject(ctx, a.ID)
	if e != nil || got.Name != "Orders" {
		t.Fatal("catalog persistence failed")
	}
	if _, e = s.UpdateObject(ctx, a.ID, UpdateObjectInput{Name: "New"}); e != nil {
		t.Fatal(e)
	}
	got, _ = s.GetObject(ctx, a.ID)
	if got.Code != a.Code {
		t.Fatal("editing changed code")
	}
}
func TestEnvironmentRelationships(t *testing.T) {
	s, _ := setup(t)
	ctx := context.Background()
	if e := s.CreateGroup(ctx, "biz-a", "Business A"); e != nil {
		t.Fatal(e)
	}
	if e := s.CreateGroup(ctx, "biz-b", "Business B"); e != nil {
		t.Fatal(e)
	}
	for _, v := range []domain.Environment{{Code: "adev", GroupCode: "biz-a", Stage: "dev", Config: map[string]any{"region": "ap-southeast-1", "ackClusterId": "cluster-a"}}, {Code: "bdev", GroupCode: "biz-b", Stage: "dev", Config: map[string]any{"region": "ap-southeast-1", "ackClusterId": "cluster-b"}}} {
		if e := s.SaveEnvironment(ctx, v, false); e != nil {
			t.Fatal(e)
		}
	}
	v, e := s.GetEnvironment(ctx, "adev")
	if e != nil {
		t.Fatal(e)
	}
	v.Config["ackClusterId"] = "cluster-edited"
	if e = s.SaveEnvironment(ctx, v, true); e != nil {
		t.Fatal(e)
	}
	fresh, _ := s.GetEnvironment(ctx, "adev")
	if fresh.Config["ackClusterId"] != "cluster-edited" {
		t.Fatal("environment context snapshotted")
	}
	if e = s.SaveEnvironment(ctx, domain.Environment{Code: "adev", GroupCode: "biz-b", Stage: "dev", Config: map[string]any{}}, false); e == nil {
		t.Fatal("duplicate global env code accepted")
	}
	if e = ValidateSameEnvironment("adev", "bdev"); e == nil {
		t.Fatal("cross-environment dependency accepted")
	}
}
func TestDiscoveryFailureIsNotEmpty(t *testing.T) {
	s, f := setup(t)
	ctx := context.Background()
	_ = s.CreateGroup(ctx, "biz", "Business")
	_ = s.SaveEnvironment(ctx, domain.Environment{Code: "adev", GroupCode: "biz", Stage: "dev", Config: map[string]any{}}, false)
	a, _ := s.CreateObject(ctx, CreateObjectInput{Kind: "application", Name: "Client"})
	items, e := s.Deployments(ctx, a.ID, "adev")
	if e != nil || items == nil || len(items) != 0 {
		t.Fatal("successful empty discovery wrong")
	}
	f.fail = true
	if _, e = s.Deployments(ctx, a.ID, "adev"); e == nil {
		t.Fatal("lookup failure converted to empty success")
	}
}

func TestEnvironmentRejectsCredentialsPreservesReferences(t *testing.T) {
	for _, v := range []any{map[string]any{"client_secret": "synthetic"}, map[string]any{"access_token": "synthetic"}, map[string]any{"api_key": "synthetic"}, map[string]any{"endpoint": "https://synthetic:synthetic@host"}, map[string]any{"endpoint": "https://host/?api_key=synthetic"}} {
		if validConfig(v) {
			t.Fatal("credential-bearing config accepted", v)
		}
	}
	if !validConfig(map[string]any{"secretRef": "kms://reference-name", "endpoint": "https://host", "client_secret_ref": "reference-only"}) {
		t.Fatal("secret reference rejected")
	}
}

func TestStructuredEnvironmentAndLegacyConfigRoundTrip(t *testing.T) {
	s, _ := setup(t)
	ctx := context.Background()
	if e := s.CreateGroup(ctx, "g", "Group"); e != nil {
		t.Fatal(e)
	}
	original := domain.Environment{Code: "dev", GroupCode: "g", Stage: "dev", Config: map[string]any{"cloud": "aliyun", "region": "ap-southeast-1", "accountId": "123", "ackClusterId": "ack", "clusterId": "legacy", "namespace": "apps", "infraApiUrl": "https://infra.example", "unknown": map[string]any{"nested": []any{float64(1), true}}, "terraformRepo": "https://github.com/jamesDeng/raptor-iap", "terraformPath": "infra-terraform"}}
	if e := s.SaveEnvironment(ctx, original, false); e != nil {
		t.Fatal(e)
	}
	got, e := s.GetEnvironment(ctx, "dev")
	if e != nil {
		t.Fatal(e)
	}
	if got.Cloud != "aliyun" || got.ACKClusterID != "ack" || got.Config["clusterId"] != "legacy" || len(got.Repositories) != 1 {
		t.Fatal("legacy adapter lost values", got)
	}
	got.Config = map[string]any{"region": "ap-southeast-2"}
	if e = s.SaveEnvironment(ctx, got, true); e != nil {
		t.Fatal(e)
	}
	got, e = s.GetEnvironment(ctx, "dev")
	if e != nil {
		t.Fatal(e)
	}
	if got.Config["region"] != "ap-southeast-2" || got.Config["unknown"] == nil || got.Config["infraApiUrl"] != "https://infra.example" {
		t.Fatal("partial legacy update lost values", got.Config)
	}
	got.Config = nil
	got.Repositories = []domain.EnvironmentRepository{{Purpose: "terraform", URL: "https://github.com/jamesDeng/raptor-iap", BaseBranch: "main", Directory: "infra-terraform"}, {Purpose: "terraform", URL: "https://github.com/jamesDeng/other", BaseBranch: "main", Directory: "terraform"}, {Purpose: "kubernetes", URL: "https://github.com/jamesDeng/raptor-iap", BaseBranch: "main", Directory: "infra-kubernetes"}}
	if e = s.SaveEnvironment(ctx, got, true); e != nil {
		t.Fatal(e)
	}
	got, e = s.GetEnvironment(ctx, "dev")
	if e != nil {
		t.Fatal(e)
	}
	if len(got.Repositories) != 3 || got.Config["unknown"] == nil || got.Config["namespace"] != "apps" {
		t.Fatal("structured update lost values", got)
	}
}

func TestRepositoryMetadataRejectsCredentialsAndTraversal(t *testing.T) {
	base := domain.EnvironmentRepository{Purpose: "terraform", URL: "https://github.com/jamesDeng/raptor-iap", BaseBranch: "main", Directory: "infra-terraform"}
	if !validRepository(base) {
		t.Fatal("valid repository rejected")
	}
	for _, change := range []func(*domain.EnvironmentRepository){func(r *domain.EnvironmentRepository) { r.URL = "https://token@github.com/jamesDeng/raptor-iap" }, func(r *domain.EnvironmentRepository) { r.URL = "https://github.com/jamesDeng/raptor-iap?token=secret" }, func(r *domain.EnvironmentRepository) { r.Directory = "../infra" }, func(r *domain.EnvironmentRepository) { r.BaseBranch = "" }} {
		r := base
		change(&r)
		if validRepository(r) {
			t.Fatal("unsafe repository accepted", r)
		}
	}
}
