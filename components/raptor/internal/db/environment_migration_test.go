package db

import (
	"context"
	"testing"

	"github.com/jamesDeng/raptor-iap/components/raptor/internal/testutil"
	"github.com/jamesDeng/raptor-iap/components/raptor/migrations"
)

func TestStructuredEnvironmentMigrationPreservesEveryKey(t *testing.T) {
	p := testutil.Database(t)
	ctx := context.Background()
	tx, e := p.Begin(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer tx.Rollback(ctx)
	if _, e = tx.Exec(ctx, "SET LOCAL ROLE raptor_owner"); e != nil {
		t.Fatal(e)
	}
	initial, _ := migrations.Files.ReadFile("001_initial.sql")
	if _, e = tx.Exec(ctx, string(initial)); e != nil {
		t.Fatal(e)
	}
	_, e = tx.Exec(ctx, `INSERT INTO raptor.environment_groups(code,name) VALUES('g','Group'); INSERT INTO raptor.environments(code,group_code,stage,config) VALUES('dev','g','dev','{"cloud":"aliyun","region":"ap-southeast-1","accountId":"123","ackClusterId":"ack","terraformRepo":"https://github.com/jamesDeng/raptor-iap","terraformBaseBranch":7,"terraformPath":"infra-terraform","kubernetesRepo":"https://github.com/jamesDeng/raptor-iap","kubernetesPath":"infra-kubernetes","namespace":"apps","infraApiUrl":"https://infra.example","clusterId":"legacy","unknown":{"nested":[1,true,null]},"oddKnown":42,"empty":""}'::jsonb)`)
	if e != nil {
		t.Fatal(e)
	}
	migration, _ := migrations.Files.ReadFile("008_structured_environments.sql")
	if _, e = tx.Exec(ctx, string(migration)); e != nil {
		t.Fatal(e)
	}
	var cloud, region, account, ack, cluster, namespace, infra string
	if e = tx.QueryRow(ctx, "SELECT cloud,region,account_id,ack_cluster_id,cluster_id,namespace,infra_api_url FROM raptor.environments WHERE code='dev'").Scan(&cloud, &region, &account, &ack, &cluster, &namespace, &infra); e != nil {
		t.Fatal(e)
	}
	if cloud != "aliyun" || region != "ap-southeast-1" || account != "123" || ack != "ack" || cluster != "legacy" || namespace != "apps" || infra != "https://infra.example" {
		t.Fatal("named values lost")
	}
	var count int
	if e = tx.QueryRow(ctx, "SELECT count(*) FROM raptor.environment_repositories WHERE environment_code='dev' AND repository_url='https://github.com/jamesDeng/raptor-iap'").Scan(&count); e != nil || count != 2 {
		t.Fatal("shared repository lost", e, count)
	}
	var preserved bool
	if e = tx.QueryRow(ctx, "SELECT value_json::jsonb = '{\"nested\":[1,true,null]}'::jsonb FROM raptor.environment_settings WHERE environment_code='dev' AND key='unknown'").Scan(&preserved); e != nil || !preserved {
		t.Fatal("unknown value lost", e)
	}
	if e = tx.QueryRow(ctx, "SELECT value_json='\"\"' FROM raptor.environment_settings WHERE environment_code='dev' AND key='empty'").Scan(&preserved); e != nil || !preserved {
		t.Fatal("empty value lost", e)
	}
	if e = tx.QueryRow(ctx, "SELECT value_json='7' FROM raptor.environment_settings WHERE environment_code='dev' AND key='terraformBaseBranch'").Scan(&preserved); e != nil || !preserved {
		t.Fatal("nonstring known value lost", e)
	}
	if _, e = tx.Exec(ctx, string(migration)); e != nil {
		t.Fatal("migration replay", e)
	}
	if e = tx.Commit(ctx); e != nil {
		t.Fatal(e)
	}
}
