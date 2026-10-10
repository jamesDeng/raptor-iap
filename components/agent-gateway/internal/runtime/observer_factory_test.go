package runtime

import (
	"context"
	"encoding/pem"
	"github.com/jamesDeng/raptor-iap/components/agent-gateway/internal/execution"
	"net/http/httptest"
	"testing"
)

func TestObserverFactoryIsOptInAndRejectsUnboundConfiguration(t *testing.T) {
	c := testCloudConfig()
	m, _, err := NewCloudClients(c, ControllerCredential{AccessKeyID: "fixture", AccessKeySecret: "fixture-secret", SecurityToken: "fixture-token"})
	if err != nil {
		t.Fatal(err)
	}
	prepare, err := BuildObservationPreparer(c, m)
	if err != nil || prepare != nil {
		t.Fatal("question default changed", err)
	}
	c.ReplacementObserver = &ReplacementObserverConfig{}
	if _, err := BuildObservationPreparer(c, m); err != ErrConfiguration {
		t.Fatal("unbound replacement observer accepted", err)
	}
}

func TestObserverFactoryBuildsWithoutAmbientIdentityAndRejectsForeignScope(t *testing.T) {
	c := testCloudConfig()
	m, _, err := NewCloudClients(c, ControllerCredential{AccessKeyID: "fixture", AccessKeySecret: "fixture-secret", SecurityToken: "fixture-token"})
	if err != nil {
		t.Fatal(err)
	}
	scope := execution.ReplacementScope{EnvCode: "rdev.ali", ProxyCode: "proxy", GroupID: "asg-test", ServerGroupID: "sgp-test", TargetDBCode: "db", Application: execution.ApplicationScope{EnvCode: "rdev.ali", AppCode: "app", ClusterID: "cluster", Namespace: "raptor-test", Name: "client", UID: "uid"}}
	c.ReplacementObserver = &ReplacementObserverConfig{Scope: scope, ClusterServer: "https://10.70.0.1", DBInstanceID: "db-instance", LoadBalancerID: "nlb-test", ListenerID: "lsn-test", PrometheusURL: "http://10.70.1.10:9090", ClientTargets: []string{"10.70.1.20:9090"}, SQLBucket: "raptor-observer-private", SQLKey: "observer/sql.json"}
	server := httptest.NewTLSServer(nil)
	defer server.Close()
	ca := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw})
	prepare, err := buildObservationPreparer(c, m, func(name string, _ int64) ([]byte, error) {
		if name != "ca.crt" {
			t.Fatal("credential read during constructor")
		}
		return ca, nil
	})
	if err != nil || prepare == nil {
		t.Fatal("valid preparation refused", err)
	}
	scope.GroupID = "asg-foreign"
	if _, err := prepare(context.Background(), execution.LiveStart{Access: execution.AgentAccess{ReplacementScope: &scope}}); err != ErrConfiguration {
		t.Fatal("foreign target acquired credential", err)
	}
}
