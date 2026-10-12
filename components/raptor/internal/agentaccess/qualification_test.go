package agentaccess

import "testing"

func TestQualificationDeniesEveryInfrastructureMutation(t *testing.T) {
	b := Binding{RequestID: "request", Operation: "db-proxy.replace-nodes", ObjectKind: "db-proxy", ObjectCode: "proxy", EnvCode: "dev", ClusterID: "cluster", ExecutionMode: "qualification"}
	s := ReplacementScope{EnvCode: "dev", ProxyCode: "proxy", GroupID: "group", ServerGroupID: "backend", TargetDBCode: "db", Application: ApplicationScope{EnvCode: "dev", AppCode: "client", ClusterID: "cluster", Namespace: "ns", Name: "client", UID: "uid"}}
	for _, tool := range []string{"db_proxy_scale", "db_proxy_node_protection_set", "db_proxy_nodes_deregister", "deployment_restart"} {
		selectors := map[string]string{"requestId": "request", "envCode": "dev", "proxyCode": "proxy", "groupId": "group"}
		if tool == "db_proxy_nodes_deregister" {
			selectors["serverGroupId"] = "backend"
		}
		if tool == "deployment_restart" {
			selectors = map[string]string{"requestId": "request", "envCode": "dev", "appCode": "client", "clusterId": "cluster", "namespace": "ns", "name": "client", "uid": "uid"}
		}
		if ReplacementSelectorsAllowed(b, s, "infra", tool, selectors) {
			t.Fatal("qualification authorized mutation", tool)
		}
	}
	if !ReplacementSelectorsAllowed(b, s, "infra", "cloud_identity_get", map[string]string{"envCode": "dev"}) {
		t.Fatal("qualification read denied")
	}
	a := map[string]string{"requestId": "request", "envCode": "dev", "proxyCode": "proxy", "groupId": "group", "interface": "runtime.qualification-resume"}
	if !ReplacementSelectorsAllowed(b, s, "raptor", "approval_request", a) {
		t.Fatal("harmless continuation denied")
	}
	delete(a, "interface")
	if ReplacementSelectorsAllowed(b, s, "raptor", "approval_request", a) {
		t.Fatal("qualification accepted scale-in approval")
	}
	b.ExecutionMode = "replacement"
	a["interface"] = "runtime.qualification-resume"
	if ReplacementSelectorsAllowed(b, s, "raptor", "approval_request", a) {
		t.Fatal("normal request accepted qualification approval")
	}
}
