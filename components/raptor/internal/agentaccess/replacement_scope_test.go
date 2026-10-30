package agentaccess

import "testing"

func TestReplacementScopeRefusesForeignDependency(t *testing.T) {
	b := Binding{RequestID: "request", EnvCode: "rdev.ali", ObjectKind: "db-proxy", ObjectCode: "proxy", Operation: "db-proxy.replace-nodes", ClusterID: "cluster"}
	s := ReplacementScope{EnvCode: "rdev.ali", ProxyCode: "proxy", GroupID: "group", ServerGroupID: "backend", TargetDBCode: "db", Application: ApplicationScope{EnvCode: "rdev.ali", AppCode: "client", ClusterID: "cluster", Namespace: "test", Name: "traffic", UID: "uid"}}
	if !s.ValidFor(b) {
		t.Fatal("matching scope rejected")
	}
	for _, which := range []string{"env", "proxy", "group", "app-env", "cluster", "uid"} {
		wrong := s
		switch which {
		case "env":
			wrong.EnvCode = "prod"
		case "proxy":
			wrong.ProxyCode = "other"
		case "group":
			wrong.GroupID = ""
		case "app-env":
			wrong.Application.EnvCode = "prod"
		case "cluster":
			wrong.Application.ClusterID = "other"
		case "uid":
			wrong.Application.UID = ""
		}
		if wrong.ValidFor(b) {
			t.Fatal("foreign/incomplete scope accepted", which)
		}
	}
}

func TestReplacementCapabilitySelectors(t *testing.T) {
	b := Binding{RequestID: "request", EnvCode: "rdev.ali", ObjectKind: "db-proxy", ObjectCode: "proxy", Operation: "db-proxy.replace-nodes", ClusterID: "cluster"}
	s := ReplacementScope{EnvCode: "rdev.ali", ProxyCode: "proxy", GroupID: "group", ServerGroupID: "backend", TargetDBCode: "db", Application: ApplicationScope{EnvCode: "rdev.ali", AppCode: "client", ClusterID: "cluster", Namespace: "test", Name: "traffic", UID: "uid"}}
	in := map[string]string{"requestId": "request", "envCode": "rdev.ali", "proxyCode": "proxy", "groupId": "group"}
	if !ReplacementSelectorsAllowed(b, s, "infra", "db_proxy_scale", in) {
		t.Fatal("bound scale denied")
	}
	in["groupId"] = "foreign"
	if ReplacementSelectorsAllowed(b, s, "infra", "db_proxy_scale", in) {
		t.Fatal("foreign group allowed")
	}
	restart := map[string]string{"requestId": "request", "envCode": "rdev.ali", "appCode": "client", "clusterId": "cluster", "namespace": "test", "name": "traffic", "uid": "uid"}
	if !ReplacementSelectorsAllowed(b, s, "infra", "deployment_restart", restart) {
		t.Fatal("dependent restart denied")
	}
	restart["appCode"] = "foreign"
	if ReplacementSelectorsAllowed(b, s, "infra", "deployment_restart", restart) {
		t.Fatal("foreign app restart allowed")
	}
	if ReplacementSelectorsAllowed(b, s, "raptor", "approval_claim", map[string]string{"requestId": "request"}) {
		t.Fatal("server-only claim exposed")
	}
}

func TestProxyReadReplacementSelectors(t *testing.T) {
	b := Binding{RequestID: "request", EnvCode: "dev", ObjectKind: "db-proxy", ObjectCode: "proxy", Operation: "db-proxy.replace-nodes", ClusterID: "cluster"}
	s := ReplacementScope{EnvCode: "dev", ProxyCode: "proxy", GroupID: "group", ServerGroupID: "backend", TargetDBCode: "db", Application: ApplicationScope{EnvCode: "dev", AppCode: "client", ClusterID: "cluster", Namespace: "ns", Name: "client", UID: "uid"}}
	for _, mode := range []string{"", "replacement", "qualification"} {
		b.ExecutionMode = mode
		for _, tool := range []string{"proxy_observation_get", "proxy_pgcat_metrics_get"} {
			selectors := map[string]string{"requestId": "request", "envCode": "dev", "proxyCode": "proxy", "groupId": "group", "serverGroupId": "backend"}
			if !ReplacementSelectorsAllowed(b, s, "infra", tool, selectors) {
				t.Errorf("bound %s read denied in %s", tool, mode)
			}
			if ReplacementSelectorsAllowed(b, s, "raptor", tool, selectors) {
				t.Fatal("wrong audience accepted")
			}
			if SelectorsAllowed(b, "infra", tool, selectors) {
				t.Fatal("application-question allowlist gained proxy read")
			}
			for key, value := range selectors {
				selectors[key] = "foreign"
				if ReplacementSelectorsAllowed(b, s, "infra", tool, selectors) {
					t.Fatal("foreign selector", key)
				}
				selectors[key] = value
			}
			for _, key := range []string{"kind", "series", "extra"} {
				selectors[key] = "up"
				if ReplacementSelectorsAllowed(b, s, "infra", tool, selectors) {
					t.Fatal("raw selector accepted", key)
				}
				delete(selectors, key)
			}
			delete(selectors, "serverGroupId")
			if ReplacementSelectorsAllowed(b, s, "infra", tool, selectors) {
				t.Fatal("missing backend allowed")
			}
		}
	}
}
