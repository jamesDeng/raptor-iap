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
