package agentaccess

import (
	"context"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/adapters"
	"testing"
)

func TestReplacementResolverRechecksLiveCatalogIdentities(t *testing.T) {
	b := Binding{RequestID: "request", EnvCode: "rdev.ali", ObjectKind: "db-proxy", ObjectCode: "proxy", Operation: "db-proxy.replace-nodes", ClusterID: "cluster"}
	scope := ReplacementScope{EnvCode: b.EnvCode, ProxyCode: b.ObjectCode, GroupID: "asg", ServerGroupID: "backend", TargetDBCode: "db", Application: ApplicationScope{EnvCode: b.EnvCode, AppCode: "client", ClusterID: b.ClusterID, Namespace: "test", Name: "traffic", UID: "uid"}}
	rows := map[string][]adapters.Deployment{"db-proxy": {{Kind: "db-proxy", EnvCode: b.EnvCode, ObjectCode: "proxy", ResourceID: "asg", TargetDBCode: "db", EvidenceMode: "live"}}, "db": {{Kind: "db", EnvCode: b.EnvCode, ObjectCode: "db", ResourceID: "rds", EvidenceMode: "live"}}, "application": {{Kind: "application", EnvCode: b.EnvCode, ObjectCode: "client", ClusterID: "cluster", Namespace: "test", Name: "traffic", UID: "uid", EvidenceMode: "live"}}}
	read := func(_ context.Context, kind, code, env string) ([]adapters.Deployment, error) { return rows[kind], nil }
	resolver := ReplacementResolver{Scope: scope, Read: read}
	if got, err := resolver.Resolve(context.Background(), b); err != nil || got != scope {
		t.Fatalf("%+v %v", got, err)
	}
	for _, bad := range []string{"catalog", "uid", "db", "fleet", "duplicate"} {
		snapshot := rows["db-proxy"]
		app := rows["application"]
		switch bad {
		case "catalog":
			rows["db-proxy"] = []adapters.Deployment{{Kind: "db-proxy", EnvCode: b.EnvCode, ObjectCode: "proxy", ResourceID: "asg", TargetDBCode: "db", EvidenceMode: "catalog"}}
		case "uid":
			rows["application"] = []adapters.Deployment{{Kind: "application", EnvCode: b.EnvCode, ObjectCode: "client", ClusterID: "cluster", Namespace: "test", Name: "traffic", UID: "other", EvidenceMode: "live"}}
		case "db":
			rows["db-proxy"] = []adapters.Deployment{{Kind: "db-proxy", EnvCode: b.EnvCode, ObjectCode: "proxy", ResourceID: "asg", TargetDBCode: "other", EvidenceMode: "live"}}
		case "fleet":
			rows["db-proxy"] = []adapters.Deployment{}
		case "duplicate":
			rows["db-proxy"] = append(append([]adapters.Deployment{}, snapshot...), snapshot...)
		}
		if _, err := resolver.Resolve(context.Background(), b); err == nil {
			t.Error("accepted", bad)
		}
		rows["db-proxy"] = snapshot
		rows["application"] = app
	}
}
