package requests

import (
	"context"
	"errors"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/adapters"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/catalog"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/domain"
	"testing"
)

type aliasDeploymentFixture struct{}

func (aliasDeploymentFixture) ListDeployments(_ context.Context, env domain.Environment, obj domain.Object) ([]adapters.Deployment, error) {
	return []adapters.Deployment{{ResourceID: "uid", Kind: obj.Kind, EnvCode: env.Code, ObjectCode: obj.Code, ClusterID: "cluster", Namespace: "ns", Name: "app", UID: "uid", EvidenceMode: "simulated"}}, nil
}
func TestDirectTargetsRejectDuplicateAfterAliasResolution(t *testing.T) {
	s, _, u := setup(t)
	ctx := context.Background()
	s.Catalog.Infra = aliasDeploymentFixture{}
	obj, e := s.Catalog.CreateObject(ctx, catalog.CreateObjectInput{Kind: "application", Name: "Migrated"})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.Pool.Exec(ctx, "INSERT INTO raptor.object_code_aliases(code,object_id,kind,canonical_code) VALUES('old-app',$1,'application',$2)", obj.ID, obj.Code); e != nil {
		t.Fatal(e)
	}
	target := domain.RestartTarget{AppCode: "old-app", EnvCode: "adev", ClusterID: "cluster", Namespace: "ns", Name: "app", UID: "uid"}
	duplicate := target
	duplicate.AppCode = obj.Code
	in := domain.RequestInput{Type: "direct", Operation: "application.restart", Targets: []domain.RestartTarget{target, duplicate}}
	if _, e = s.Create(ctx, u, "mixed-alias", in); !errors.Is(e, domain.ErrInvalid) {
		t.Fatalf("duplicate normalized targets must be invalid, got %v", e)
	}
	var count int
	if e = s.Pool.QueryRow(ctx, "SELECT count(*) FROM raptor.requests").Scan(&count); e != nil || count != 0 {
		t.Fatalf("invalid request persisted: %d %v", count, e)
	}
	in.Targets = in.Targets[:1]
	r, e := s.Create(ctx, u, "single-alias", in)
	if e != nil || r.Definition.Targets[0].AppCode != obj.Code {
		t.Fatalf("canonical direct target missing: %+v %v", r, e)
	}
}

func TestHistoricalLegacyRequestCannotResume(t *testing.T) {
	s, in, u := setup(t)
	ctx := context.Background()
	r, e := s.Create(ctx, u, "historic", in)
	if e != nil {
		t.Fatal(e)
	}
	obj, e := s.Catalog.FindObject(ctx, "database", in.Object.Code)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.Pool.Exec(ctx, "INSERT INTO raptor.object_code_aliases(code,object_id,kind,canonical_code) VALUES('old-db',$1,'database',$2)", obj.ID, obj.Code); e != nil {
		t.Fatal(e)
	}
	if _, e = s.Pool.Exec(ctx, `UPDATE raptor.requests SET definition=jsonb_set(definition,'{object,code}','"old-db"'),status='failed' WHERE id=$1`, r.ID); e != nil {
		t.Fatal(e)
	}
	if e = s.Action(ctx, r.ID, "continue", "retry"); !errors.Is(e, domain.ErrConflict) {
		t.Fatalf("resumed immutable legacy binding: %v", e)
	}
	got, e := s.Get(ctx, r.ID)
	if e != nil || got.Status != "failed" || got.Definition.Object.Code != "old-db" {
		t.Fatalf("historical request modified: %+v %v", got, e)
	}
}
