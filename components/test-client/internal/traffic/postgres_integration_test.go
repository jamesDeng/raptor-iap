package traffic

import (
	"context"
	"github.com/jackc/pgx/v5"
	"os"
	"testing"
	"time"
)

func TestPostgresInsertReadbackCommitsExactlyOnce(t *testing.T) {
	dsn := os.Getenv("TEST_LOCAL_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("dedicated local PostgreSQL DSN required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, e := pgx.Connect(ctx, dsn)
	if e != nil {
		t.Fatal("local database connection failed")
	}
	defer conn.Close(ctx)
	_, e = conn.Exec(ctx, "CREATE TABLE IF NOT EXISTS infra_test_operations(id text PRIMARY KEY,created_at timestamptz NOT NULL DEFAULT now())")
	if e != nil {
		t.Fatal(e)
	}
	s, e := ConnectPostgres(ctx, dsn)
	if e != nil {
		t.Fatal("local session failed")
	}
	defer s.Close()
	id := "integration-" + time.Now().Format("150405.000000000")
	if s.Operate(ctx, id) != Success {
		t.Fatal("insert/readback failed")
	}
	if s.Operate(ctx, id) != Failure {
		t.Fatal("duplicate silently retried")
	}
	var n int
	if e = conn.QueryRow(ctx, "SELECT count(*) FROM infra_test_operations WHERE id=$1", id).Scan(&n); e != nil || n != 1 {
		t.Fatal(n)
	}
}
