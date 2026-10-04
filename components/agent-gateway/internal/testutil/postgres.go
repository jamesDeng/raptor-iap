package testutil

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"github.com/jackc/pgx/v5/pgxpool"
	"os"
	"testing"
)

func Database(t *testing.T) *pgxpool.Pool {
	t.Helper()
	ctx := context.Background()
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Fatal("TEST_DATABASE_URL is required")
	}
	master, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal("test database config invalid")
	}
	t.Cleanup(master.Close)
	var b [8]byte
	_, _ = rand.Read(b[:])
	name := "raptor_test_" + hex.EncodeToString(b[:])
	c, err := master.Acquire(ctx)
	if err != nil {
		t.Fatal("test cluster unavailable:", err)
	}
	_, err = c.Exec(ctx, "SELECT pg_advisory_lock(55881)")
	if err != nil {
		t.Fatal(err)
	}
	_, err = c.Exec(ctx, "DO $$ BEGIN IF NOT EXISTS (SELECT FROM pg_roles WHERE rolname='raptor_owner') THEN CREATE ROLE raptor_owner NOLOGIN; END IF; IF NOT EXISTS (SELECT FROM pg_roles WHERE rolname='gateway_owner') THEN CREATE ROLE gateway_owner NOLOGIN; END IF; IF NOT EXISTS (SELECT FROM pg_roles WHERE rolname='raptor_app') THEN CREATE ROLE raptor_app NOLOGIN; END IF; IF NOT EXISTS (SELECT FROM pg_roles WHERE rolname='gateway_app') THEN CREATE ROLE gateway_app NOLOGIN; END IF; END $$")
	_, _ = c.Exec(ctx, "SELECT pg_advisory_unlock(55881)")
	c.Release()
	if err != nil {
		t.Fatal(err)
	}
	if _, err = master.Exec(ctx, "CREATE DATABASE "+name); err != nil {
		t.Fatal(err)
	}
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ConnConfig.Database = name
	p, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { p.Close(); _, _ = master.Exec(ctx, "DROP DATABASE "+name+" WITH (FORCE)") })
	if _, err = p.Exec(ctx, "GRANT CREATE ON DATABASE "+name+" TO raptor_owner,gateway_owner"); err != nil {
		t.Fatal(err)
	}
	_, err = p.Exec(ctx, "REVOKE CREATE ON SCHEMA public FROM PUBLIC; CREATE SCHEMA raptor AUTHORIZATION raptor_owner; CREATE SCHEMA gateway AUTHORIZATION gateway_owner; CREATE TABLE raptor.probe(id integer); CREATE TABLE gateway.probe(id integer); REVOKE ALL ON SCHEMA raptor,gateway FROM PUBLIC; GRANT USAGE ON SCHEMA raptor TO raptor_app; GRANT USAGE ON SCHEMA gateway TO gateway_app;")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = p.Exec(ctx, "ALTER TABLE raptor.probe OWNER TO raptor_owner; ALTER TABLE gateway.probe OWNER TO gateway_owner"); err != nil {
		t.Fatal(err)
	}
	return p
}
