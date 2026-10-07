package db

import (
	"context"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/testutil"
	"testing"
)

func TestCatalogCodesGloballyUnique(t *testing.T) {
	p := testutil.Database(t)
	ctx := context.Background()
	if e := Migrate(ctx, p); e != nil {
		t.Fatal(e)
	}
	if e := Migrate(ctx, p); e != nil {
		t.Fatal("migration replay", e)
	}
	_, e := p.Exec(ctx, `INSERT INTO raptor.objects(id,kind,code,name) VALUES('11111111-1111-4111-8111-111111111111','application','same','A')`)
	if e != nil {
		t.Fatal(e)
	}
	_, e = p.Exec(ctx, `INSERT INTO raptor.objects(id,kind,code,name) VALUES('22222222-2222-4222-8222-222222222222','database','same','B')`)
	if e == nil {
		t.Fatal("duplicate cross-kind code accepted")
	}
}
