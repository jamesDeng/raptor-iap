package agentaccess

import (
	"context"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/adapters"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/domain"
	"testing"
)

func TestReplacementResolverRechecksLiveCatalogIdentities(t *testing.T) {
	b := Binding{RequestID: "r", EnvCode: "dev", ObjectKind: "db-proxy", ObjectCode: "proxy", ExecutionMode: "replacement", Operation: "db-proxy.replace-nodes", ClusterID: "c"}
	c := domain.InfraResourceContext{EnvCode: "dev", Kind: "db-proxy", Code: "proxy", Cloud: "aliyun", Region: "ap-southeast-1", AccountID: "123", ClusterID: "c", RelatedResources: []domain.ObjectRef{{Kind: "application", Code: "app"}}}
	i := adapters.ResolutionIdentity{EnvCode: "dev", AccountID: "123", Region: "ap-southeast-1", ClusterID: "c", ProxyCode: "proxy", GroupID: "g", ServerGroupID: "sg", ListenerID: "l", Port: 5432, TargetDBCode: "db", DatabaseResourceID: "rds", Application: adapters.ApplicationIdentity{EnvCode: "dev", AppCode: "app", ClusterID: "c", Namespace: "n", Name: "app", UID: "u"}}
	calls := 0
	proof := adapters.ResolutionProof{Identity: i, ResolutionDigest: i.Digest()}
	resolver := ReplacementResolver{Context: func(context.Context, string, string, string) (domain.InfraResourceContext, error) { return c, nil }, Proof: func(context.Context, string, string) (adapters.ResolutionProof, error) { calls++; return proof, nil }}
	scope, e := resolver.Resolve(context.Background(), b)
	if e != nil || !scope.ResourceValidFor(b) {
		t.Fatal(scope, e)
	}
	for _, which := range []string{"missing-app", "multiple-apps", "foreign-env", "foreign-cluster"} {
		original := c
		switch which {
		case "missing-app":
			c.RelatedResources = nil
		case "multiple-apps":
			c.RelatedResources = append(append([]domain.ObjectRef{}, c.RelatedResources...), domain.ObjectRef{Kind: "application", Code: "other"})
		case "foreign-env":
			c.EnvCode = "foreign"
		case "foreign-cluster":
			c.ClusterID = "foreign"
		}
		calls = 0
		if _, e := resolver.Resolve(context.Background(), b); e == nil || calls != 0 {
			t.Fatal("unbound context reached proof", which, calls, e)
		}
		c = original
	}
	for _, which := range []string{"digest", "app", "account", "region", "cluster", "database", "listener"} {
		proof = adapters.ResolutionProof{Identity: i, ResolutionDigest: i.Digest()}
		switch which {
		case "digest":
			proof.ResolutionDigest = "wrong"
		case "app":
			proof.Identity.Application.AppCode = "foreign"
		case "account":
			proof.Identity.AccountID = "other"
		case "region":
			proof.Identity.Region = "other"
		case "cluster":
			proof.Identity.ClusterID = "other"
		case "database":
			proof.Identity.DatabaseResourceID = ""
		case "listener":
			proof.Identity.ListenerID = ""
		}
		if _, e := resolver.Resolve(context.Background(), b); e == nil {
			t.Fatal("invalid proof accepted", which)
		}
	}
}
