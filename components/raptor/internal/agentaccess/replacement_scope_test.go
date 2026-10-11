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
