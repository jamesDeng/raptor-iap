package agentaccess

import "testing"

func TestResourceSelectorsUseLogicalIdentity(t *testing.T) {
	b := Binding{RequestID: "r", EnvCode: "dev", ObjectKind: "db-proxy", ObjectCode: "p", ExecutionMode: "replacement", Operation: "db-proxy.replace-nodes", ClusterID: "c"}
	s := ReplacementScope{EnvCode: "dev", ProxyCode: "p", GroupID: "g", ServerGroupID: "sg", TargetDBCode: "db", Application: ApplicationScope{EnvCode: "dev", AppCode: "app", ClusterID: "c", Namespace: "n", Name: "app", UID: "u"}}
	s.AccountID = "123"
	s.Region = "ap-southeast-1"
	s.ListenerID = "listener"
	s.Port = 5432
	s.DatabaseResourceID = "rds"
	s.ResolutionDigest = s.Identity().Digest()
	for _, tc := range []struct {
		audience, tool string
		selectors      map[string]string
	}{
		{"infra", "db_proxy_scale", map[string]string{"requestId": "r", "envCode": "dev", "proxyCode": "p"}},
		{"infra", "deployment_status_get", map[string]string{"envCode": "dev", "appCode": "app"}},
		{"infra", "deployment_status_get", map[string]string{"envCode": "dev", "appCode": "app", "workloadKey": "opaque"}},
		{"infra", "deployments_list", map[string]string{"envCode": "dev", "kind": "database", "code": "db"}},
		{"raptor", "object_get", map[string]string{"requestId": "r", "kind": "application", "code": "app"}},
		{"raptor", "deployments_list", map[string]string{"requestId": "r", "envCode": "dev", "kind": "database", "code": "db"}},
	} {
		if !ReplacementSelectorsAllowed(b, s, tc.audience, tc.tool, tc.selectors) {
			t.Errorf("logical %s %s denied", tc.audience, tc.tool)
		}
	}
	if ReplacementSelectorsAllowed(b, s, "infra", "cloud_identity_get", map[string]string{"envCode": "dev"}) {
		t.Error("cloud selector retained")
	}
}

func TestResourceSelectorsRejectHistoricalModeAndMissingProof(t *testing.T) {
	b := Binding{RequestID: "r", EnvCode: "dev", ObjectKind: "db-proxy", ObjectCode: "p", ExecutionMode: "replacement", Operation: "db-proxy.replace-nodes", ClusterID: "c"}
	s := ReplacementScope{EnvCode: "dev", ProxyCode: "p", GroupID: "g", ServerGroupID: "sg", TargetDBCode: "db", Application: ApplicationScope{EnvCode: "dev", AppCode: "app", ClusterID: "c", Namespace: "n", Name: "app", UID: "u"}, AccountID: "123", Region: "ap-southeast-1", ListenerID: "l", Port: 5432, DatabaseResourceID: "rds"}
	s.ResolutionDigest = s.Identity().Digest()
	selectors := map[string]string{"requestId": "r", "envCode": "dev", "proxyCode": "p"}
	if !ReplacementSelectorsAllowed(b, s, "infra", "db_proxy_scale", selectors) {
		t.Fatal("new proof rejected")
	}
	b.ExecutionMode = ""
	if ReplacementSelectorsAllowed(b, s, "infra", "db_proxy_scale", selectors) {
		t.Error("historical mode authorized")
	}
	b.ExecutionMode = "replacement"
	s.ResolutionDigest = ""
	if ReplacementSelectorsAllowed(b, s, "infra", "db_proxy_scale", selectors) {
		t.Error("missing proof authorized")
	}
}
