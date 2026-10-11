package runtime

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"github.com/jamesDeng/raptor-iap/components/agent-gateway/internal/execution"
	"testing"
	"time"
)

func TestObserverPreparerRefreshesCredentialsAndBindsMaterial(t *testing.T) {
	b := execution.AttemptBinding{RequestID: "request", AttemptID: "attempt", DefinitionSHA256: "hash", EnvCode: "rdev.ali", ClusterID: "cluster", Operation: "db-proxy.replace-nodes", ObjectKind: "db-proxy", ObjectCode: "proxy"}
	scope := execution.ReplacementScope{EnvCode: b.EnvCode, ProxyCode: b.ObjectCode, GroupID: "group", ServerGroupID: "backend", TargetDBCode: "db-code", Application: execution.ApplicationScope{EnvCode: b.EnvCode, AppCode: "app", ClusterID: b.ClusterID, Namespace: "raptor-test", Name: "client", UID: "uid"}}
	cloudCalls, kubeCalls := 0, 0
	prepare := ObserverPreparation{Server: "https://10.70.0.1", CA: []byte("fixture-ca"), DBInstanceID: "db-instance", ListenerID: "listener", PrometheusURL: "http://10.70.1.10:9090", ClientTargets: []string{"10.70.1.20:9090"}, Cloud: func(context.Context, execution.LiveStart) (ObserverCredential, error) {
		cloudCalls++
		return ObserverCredential{AccessKeyID: "key", AccessKeySecret: "secret", SecurityToken: "sts", ExpiresAt: time.Now().Add(800 * time.Second)}, nil
	}, Kube: func(context.Context, execution.LiveStart) (ObserverKubeCredential, error) {
		kubeCalls++
		payload, _ := json.Marshal(map[string]any{"sub": "system:serviceaccount:raptor-test:client-observer", "exp": time.Now().Add(900 * time.Second).Unix()})
		return ObserverKubeCredential{Token: "e30." + base64.RawURLEncoding.EncodeToString(payload) + ".fixture", ExpiresAt: time.Now().Add(900 * time.Second)}, nil
	}, SQL: func(context.Context, execution.LiveStart) (ObserverSQLCredential, error) {
		return ObserverSQLCredential{User: "probe", Password: "private-password", Database: "test", SSLMode: "disable"}, nil
	}}
	in := execution.LiveStart{Binding: b, Access: execution.AgentAccess{ReplacementScope: &scope}}
	for j := 0; j < 2; j++ {
		material, err := prepare.Prepare(context.Background(), in)
		if err != nil || material.Validate(b, scope) != nil {
			t.Fatal("bound material failed", err)
		}
		if len(material.Secrets) != 5 {
			t.Fatal("private values omitted from redaction")
		}
	}
	if cloudCalls != 2 || kubeCalls != 2 {
		t.Fatal("resumed attempt reused expired credentials")
	}
	scope.EnvCode = "foreign"
	if _, err := prepare.Prepare(context.Background(), in); err != ErrConfiguration {
		t.Fatal("foreign scope accepted")
	}
	if cloudCalls != 2 {
		t.Fatal("issued credential before scope validation")
	}
}
