package runtime

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"github.com/jamesDeng/raptor-iap/components/agent-gateway/internal/execution"
	"testing"
	"time"
)

func TestObserverMaterialMatchesAttemptAndKeepsSecretsPrivate(t *testing.T) {
	b := execution.AttemptBinding{RequestID: "request", DefinitionSHA256: "hash", EnvCode: "rdev.ali", ClusterID: "cluster"}
	scope := execution.ReplacementScope{EnvCode: "rdev.ali", GroupID: "group", ServerGroupID: "backend", Application: execution.ApplicationScope{ClusterID: "cluster", Namespace: "raptor-test", Name: "client", UID: "uid"}}
	cfg := map[string]any{"version": 1, "requestId": "request", "definitionSha256": "hash", "envCode": "rdev.ali", "accountId": "1360282071200743", "region": "ap-southeast-1", "groupId": "group", "serverGroupId": "backend", "expiresAt": time.Now().Add(900 * time.Second), "application": map[string]any{"clusterId": "cluster", "namespace": "raptor-test", "name": "client", "uid": "uid"}, "private": map[string]any{"kubeconfig": "/tmp/raptor-private/kubeconfig"}}
	raw, _ := json.Marshal(cfg)
	material := ObservationMaterial{Config: raw, Kubeconfig: observerKubeFixture("system:serviceaccount:raptor-test:client-observer")}
	if material.Validate(b, scope) != nil {
		t.Fatal("matching material refused")
	}
	cfg["requestId"] = "foreign"
	raw, _ = json.Marshal(cfg)
	material.Config = raw
	if material.Validate(b, scope) == nil {
		t.Fatal("foreign attempt accepted")
	}
}

func TestReplacementRequiresPrivateObservationPreparer(t *testing.T) {
	n := NativeLive{}
	in := execution.LiveStart{Binding: execution.AttemptBinding{Operation: "db-proxy.replace-nodes"}}
	if _, err := n.prepareObservation(context.Background(), in); err != ErrConfiguration {
		t.Fatal("replacement admitted without observer")
	}
	in.Binding.Operation = "application.question"
	if _, err := n.prepareObservation(context.Background(), in); err != nil {
		t.Fatal("question profile gained observer requirement")
	}
}

func observerKubeFixture(subject string) []byte {
	payload, _ := json.Marshal(map[string]any{"sub": subject, "exp": time.Now().Add(901 * time.Second).Unix()})
	token := "e30." + base64.RawURLEncoding.EncodeToString(payload) + ".fixture"
	raw, _ := json.Marshal(map[string]any{"apiVersion": "v1", "kind": "Config", "current-context": "observer", "clusters": []any{map[string]any{"name": "cluster", "cluster": map[string]any{"server": "https://10.70.0.1", "certificate-authority-data": base64.StdEncoding.EncodeToString([]byte("fixture-ca"))}}}, "contexts": []any{map[string]any{"name": "observer", "context": map[string]any{"cluster": "cluster", "user": "observer", "namespace": "raptor-test"}}}, "users": []any{map[string]any{"name": "observer", "user": map[string]any{"token": token}}}})
	return raw
}
func TestObserverKubeconfigRejectsOperatorCredentialsAndExecutables(t *testing.T) {
	b := execution.AttemptBinding{ClusterID: "cluster"}
	s := execution.ReplacementScope{Application: execution.ApplicationScope{Namespace: "raptor-test", Name: "client"}}
	if validateObserverKubeconfig(observerKubeFixture("system:serviceaccount:raptor-test:client-observer"), b, s, time.Now().Add(900*time.Second)) != nil {
		t.Fatal("observer refused")
	}
	if validateObserverKubeconfig(observerKubeFixture("system:serviceaccount:raptor-system:agent-gateway"), b, s, time.Now().Add(900*time.Second)) == nil {
		t.Fatal("controller token copied to sandbox")
	}
	var value map[string]any
	json.Unmarshal(observerKubeFixture("system:serviceaccount:raptor-test:client-observer"), &value)
	value["users"].([]any)[0].(map[string]any)["user"].(map[string]any)["exec"] = map[string]any{"command": "malicious"}
	raw, _ := json.Marshal(value)
	if validateObserverKubeconfig(raw, b, s, time.Now().Add(900*time.Second)) == nil {
		t.Fatal("exec credential accepted")
	}
}
