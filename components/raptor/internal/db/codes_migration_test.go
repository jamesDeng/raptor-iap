package db

import (
	"context"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/testutil"
	"github.com/jamesDeng/raptor-iap/components/raptor/migrations"
	"testing"
)

func TestExistingCodesMigratedOnceWithAliases(t *testing.T) {
	p := testutil.Database(t)
	ctx := context.Background()
	b, e := migrations.Files.ReadFile("001_initial.sql")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = p.Exec(ctx, "SET ROLE raptor_owner;"+string(b)+";RESET ROLE;"); e != nil {
		t.Fatal(e)
	}
	_, e = p.Exec(ctx, `INSERT INTO raptor.objects(id,kind,code,name) VALUES
 ('00000000-0000-0000-0000-000000000001','application','old-app','App'),
 ('00000000-0000-0000-0000-000000000002','database','old-db','DB'),
 ('00000000-0000-0000-0000-000000000003','db-proxy','old-proxy','Proxy')`)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = p.Exec(ctx, `INSERT INTO raptor.requests(id,creator_id,idempotency_key,input_hash,definition,schema_hash,status) VALUES('00000000-0000-0000-0000-000000000004','00000000-0000-0000-0000-000000000005','key','original-hash','{"type":"agent","object":{"kind":"application","code":"old-app"}}','schema','completed')`); e != nil {
		t.Fatal(e)
	}
	if e = Migrate(ctx, p); e != nil {
		t.Fatal(e)
	}
	for _, v := range []struct{ old, want string }{{"old-app", "A00001"}, {"old-db", "D00001"}, {"old-proxy", "DP00001"}} {
		var got string
		e = p.QueryRow(ctx, `SELECT o.code FROM raptor.object_code_aliases a JOIN raptor.objects o ON o.id=a.object_id WHERE a.code=$1`, v.old).Scan(&got)
		if e != nil || got != v.want {
			t.Fatalf("mapping %s: %s %v", v.old, got, e)
		}
	}
	if _, e = p.Exec(ctx, "SELECT nextval('raptor.application_code_seq')"); e != nil {
		t.Fatal(e)
	}
	if e = Migrate(ctx, p); e != nil {
		t.Fatal(e)
	}
	var next int
	if e = p.QueryRow(ctx, "SELECT nextval('raptor.application_code_seq')").Scan(&next); e != nil || next != 3 {
		t.Fatalf("migration reset sequence: %d %v", next, e)
	}
	var historicalCode, hash string
	if e = p.QueryRow(ctx, "SELECT definition->'object'->>'code',input_hash FROM raptor.requests").Scan(&historicalCode, &hash); e != nil || historicalCode != "old-app" || hash != "original-hash" {
		t.Fatalf("historical evidence changed: %q %q %v", historicalCode, hash, e)
	}
	var count int
	if e = p.QueryRow(ctx, "SELECT count(*) FROM raptor.object_code_aliases").Scan(&count); e != nil || count != 3 {
		t.Fatalf("aliases changed: %d %v", count, e)
	}
}

func TestCodeMigrationRefusesUnfinishedRequests(t *testing.T) {
	p := testutil.Database(t)
	ctx := context.Background()
	b, e := migrations.Files.ReadFile("001_initial.sql")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = p.Exec(ctx, "SET ROLE raptor_owner;"+string(b)+";RESET ROLE;"); e != nil {
		t.Fatal(e)
	}
	_, e = p.Exec(ctx, `INSERT INTO raptor.objects(id,kind,code,name) VALUES('00000000-0000-0000-0000-000000000001','application','old-app','App');
 INSERT INTO raptor.requests(id,creator_id,idempotency_key,input_hash,definition,schema_hash,status) VALUES('00000000-0000-0000-0000-000000000002','00000000-0000-0000-0000-000000000003','key','hash','{"type":"agent","object":{"kind":"application","code":"old-app"}}','schema','running')`)
	if e != nil {
		t.Fatal(e)
	}
	if e = Migrate(ctx, p); e == nil {
		t.Fatal("renamed catalog while a request was unfinished")
	}
	var code string
	if e = p.QueryRow(ctx, "SELECT code FROM raptor.objects").Scan(&code); e != nil || code != "old-app" {
		t.Fatalf("failed migration changed identity: %q %v", code, e)
	}
}
