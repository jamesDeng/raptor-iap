package requests

import (
	"context"
	"errors"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/adapters"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/catalog"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/db"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/domain"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/testutil"
	"strings"
	"testing"
)

func setup(t *testing.T) (*Service, domain.RequestInput, domain.User) {
	ctx := context.Background()
	p := testutil.Database(t)
	if e := db.Migrate(ctx, p); e != nil {
		t.Fatal(e)
	}
	c := catalog.NewService(p, adapters.UnavailableInfra{})
	if e := c.CreateGroup(ctx, "biz", "Business"); e != nil {
		t.Fatal(e)
	}
	if e := c.SaveEnvironment(ctx, domain.Environment{Code: "adev", GroupCode: "biz", Stage: "dev", Config: map[string]any{"ackClusterId": "cluster"}}, false); e != nil {
		t.Fatal(e)
	}
	o, e := c.CreateObject(ctx, catalog.CreateObjectInput{Kind: "database", Name: "DB"})
	if e != nil {
		t.Fatal(e)
	}
	s := NewService(p, c)
	s.ResolveSkills = func(ctx context.Context, v domain.SkillsVersion) (domain.SkillsVersion, error) { return v, nil }
	in := domain.RequestInput{Type: "agent", Object: domain.ObjectRef{Kind: o.Kind, Code: o.Code}, EnvCode: "adev", Skills: domain.SkillsVersion{Tag: "skills-v0.1.0", CommitSHA: strings.Repeat("a", 40)}, Operations: []domain.Operation{{Name: "database.deploy", Parameters: map[string]any{"engineVersion": "test-version", "instanceClass": "synthetic-small", "storageGiB": float64(20)}}}}
	return s, in, domain.User{ID: domain.NewID(), Role: "user"}
}
func TestOperationSchemaValidation(t *testing.T) {
	s, in, _ := setup(t)
	if e := s.Validate(in); e != nil {
		t.Fatal(e)
	}
	for _, tc := range []struct {
		key    string
		value  any
		remove bool
	}{{"engineVersion", nil, true}, {"storageGiB", "20", false}, {"storageGiB", 0, false}, {"storageGiB", 1.5, false}, {"unexpected", true, false}} {
		copy := in
		copy.Operations = []domain.Operation{{Name: in.Operations[0].Name, Parameters: map[string]any{}}}
		for k, v := range in.Operations[0].Parameters {
			copy.Operations[0].Parameters[k] = v
		}
		if tc.remove {
			delete(copy.Operations[0].Parameters, tc.key)
		} else {
			copy.Operations[0].Parameters[tc.key] = tc.value
		}
		if e := s.Validate(copy); e == nil {
			t.Fatalf("invalid input accepted: %s=%v", tc.key, tc.value)
		}
	}
	in.Object.Kind = "application"
	if e := s.Validate(in); e == nil {
		t.Fatal("operation allowed wrong primary object")
	}
}
func TestRequestIdempotencyAndTransactionalOutbox(t *testing.T) {
	s, in, u := setup(t)
	ctx := context.Background()
	a, e := s.Create(ctx, u, "stable-key", in)
	if e != nil {
		t.Fatal(e)
	}
	b, e := s.Create(ctx, u, "stable-key", in)
	if e != nil || a.ID != b.ID {
		t.Fatal("duplicate request on retry")
	}
	if a.SchemaHash == "" {
		t.Fatal("schema hash not saved")
	}
	in.Operations[0].Parameters["storageGiB"] = float64(30)
	if _, e = s.Create(ctx, u, "stable-key", in); !errors.Is(e, domain.ErrConflict) {
		t.Fatal("idempotency key accepted different body")
	}
	var count int
	_ = s.Pool.QueryRow(ctx, "SELECT count(*) FROM raptor.outbox WHERE entity_id=$1", a.ID).Scan(&count)
	if count != 1 {
		t.Fatal("request and outbox not atomic")
	}
	if _, e = s.Create(ctx, u, "invalid", domain.RequestInput{}); e == nil {
		t.Fatal("invalid request dispatched")
	}
}

type lostAck struct {
	failed bool
	ids    map[string]bool
}

func (f *lostAck) PutRequest(ctx context.Context, id string) error {
	f.ids[id] = true
	if !f.failed {
		f.failed = true
		return errors.New("accepted but acknowledgement lost")
	}
	return nil
}
func TestLostDispatchAcknowledgement(t *testing.T) {
	s, in, u := setup(t)
	ctx := context.Background()
	r, e := s.Create(ctx, u, "request", in)
	if e != nil {
		t.Fatal(e)
	}
	f := &lostAck{ids: map[string]bool{}}
	_ = s.DispatchPending(ctx, f)
	var delivered bool
	_ = s.Pool.QueryRow(ctx, "SELECT delivered FROM raptor.outbox WHERE entity_id=$1", r.ID).Scan(&delivered)
	if delivered {
		t.Fatal("lost acknowledgement marked delivered")
	}
	if e = s.DispatchPending(ctx, f); e != nil {
		t.Fatal(e)
	}
	_ = s.Pool.QueryRow(ctx, "SELECT delivered FROM raptor.outbox WHERE entity_id=$1", r.ID).Scan(&delivered)
	if !delivered || len(f.ids) != 1 || !f.ids[r.ID] {
		t.Fatal("retry handoff changed identity")
	}
}
func TestProxyRequiresDeployedDatabase(t *testing.T) {
	s, in, u := setup(t)
	ctx := context.Background()
	proxy, e := s.Catalog.CreateObject(ctx, catalog.CreateObjectInput{Kind: "db-proxy", Name: "Proxy"})
	if e != nil {
		t.Fatal(e)
	}
	databaseCode := in.Object.Code
	in.Object = domain.ObjectRef{Kind: "db-proxy", Code: proxy.Code}
	in.Operations = []domain.Operation{{Name: "db-proxy.deploy", Parameters: map[string]any{"targetDbCode": databaseCode, "desiredCapacity": float64(2), "instanceClass": "synthetic", "pgcatVersion": "test"}}}
	if _, e = s.Create(ctx, u, "proxy", in); e == nil {
		t.Fatal("proxy accepted undeployed database")
	}
}
