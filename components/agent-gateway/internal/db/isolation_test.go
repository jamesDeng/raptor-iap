package db

import (
	"context"
	"github.com/jamesDeng/raptor-iap/components/agent-gateway/internal/testutil"
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
	if _, err = c.Exec(ctx, "SET ROLE gateway_app"); err != nil {
		t.Fatal(err)
	}
	if _, err = c.Exec(ctx, "SELECT request_id FROM gateway.executions LIMIT 0"); err != nil {
		t.Fatal("own schema:", err)
	}
	if _, err = c.Exec(ctx, "SELECT * FROM raptor.probe"); err == nil {
		t.Fatal("cross-schema access allowed")
	}
	_, _ = c.Exec(ctx, "RESET ROLE")
}
