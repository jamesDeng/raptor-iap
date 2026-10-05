package db

import (
	"context"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/testutil"
	"testing"
)

func TestSchemaIsolation(t *testing.T) {
	p := testutil.Database(t)
	ctx := context.Background()
	if err := Migrate(ctx, p); err != nil {
		t.Fatal(err)
	}
	if err := Migrate(ctx, p); err != nil {
		t.Fatal("repeat migration:", err)
	}
	c, err := p.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Release()
	if _, err = c.Exec(ctx, "SET ROLE raptor_app"); err != nil {
		t.Fatal(err)
	}
	if _, err = c.Exec(ctx, "SELECT id FROM raptor.objects LIMIT 0"); err != nil {
		t.Fatal("own schema:", err)
	}
	if _, err = c.Exec(ctx, "SELECT * FROM gateway.probe"); err == nil {
		t.Fatal("cross-schema access allowed")
	}
	if _, err = c.Exec(ctx, "RESET ROLE"); err != nil {
		t.Fatal(err)
	}
	_, err = p.Exec(ctx, "INSERT INTO raptor.objects(id,kind,code,name) VALUES ('00000000-0000-0000-0000-000000000001','database','unique','a'),('00000000-0000-0000-0000-000000000002','database','unique','b')")
	if err == nil {
		t.Fatal("duplicate object code accepted")
	}
	_, err = p.Exec(ctx, "INSERT INTO raptor.environment_groups(code,name) VALUES ('g','g'); INSERT INTO raptor.environments(code,group_code,stage,config) VALUES ('adev','g','dev','{}'),('adev','g','dev','{}')")
	if err == nil {
		t.Fatal("duplicate environment code accepted")
	}
}
