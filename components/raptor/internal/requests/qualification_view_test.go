package requests

import (
	"encoding/json"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/agentaccess"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/domain"
	"strings"
	"testing"
	"time"
)

func TestQualificationViewCannotReportReplacementOrUnverifiedCleanup(t *testing.T) {
	r := domain.Request{ID: "r", Definition: domain.RequestInput{Type: "agent", Object: domain.ObjectRef{Kind: "db-proxy", Code: "p"}, EnvCode: "dev", Model: "gpt-5.6-luna", Operations: []domain.Operation{{Name: "db-proxy.replace-nodes", Parameters: map[string]any{"mode": "qualification"}}}}}
	now := time.Now()
	q := &PublicQualification{Version: 1, Passed: true, ReadOnly: true, TrafficAcceptance: "not_assessed", Scope: agentaccess.ReplacementScope{EnvCode: "dev", ProxyCode: "p", GroupID: "g", ServerGroupID: "s", TargetDBCode: "d", Application: agentaccess.ApplicationScope{EnvCode: "dev", AppCode: "a", ClusterID: "c", Namespace: "n", Name: "x", UID: "u"}}, Checks: []string{"mcp.raptor.request_get", "mcp.raptor.environment_get", "mcp.raptor.object_get", "mcp.infra.cloud_identity_get", "mcp.infra.deployments_list", "mcp.infra.deployment_status_get", "cloud.fleet", "cloud.backend", "cloud.capacity", "deployment.status", "metrics.trafficFailures", "metrics.connections", "metrics.pgcatClients"}, InstanceIDs: []string{"i1", "i2"}, ConnectedNodeIDs: []string{"i1", "i2"}, ApprovalID: "approval", ActionID: "action", BindingDigest: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", ObservedAt: now}
	v := PublicExecution{RuntimeMode: "live", Status: "completed", CheckpointStatus: "verified", CleanupStatus: &PublicCleanup{true, true, true}, Result: &PublicResult{RequestID: r.ID, ActualModel: r.Definition.Model, SelectedModel: r.Definition.Model, GeneratedAt: &now, Answer: "Native read-only qualification completed; replacement and traffic acceptance were not executed.", Qualification: q}}
	if !validQualificationView(v, r) {
		t.Fatal("valid qualification rejected")
	}
	v.CleanupStatus.AccessRevoked = false
	if validQualificationView(v, r) {
		t.Fatal("unfinished cleanup reported")
	}
	v.CleanupStatus.AccessRevoked = true
	q.TrafficAcceptance = "PASS"
	if validQualificationView(v, r) {
		t.Fatal("replacement acceptance claimed")
	}
	q.TrafficAcceptance = "not_assessed"
	q.Scope.ProxyCode = "other"
	if validQualificationView(v, r) {
		t.Fatal("foreign target reported")
	}
}

func TestResourceQuestionViewAcceptsActualDiscoveryWithoutCloudTool(t *testing.T) {
	now := time.Now()
	r := domain.Request{ID: "r", Definition: domain.RequestInput{Type: "agent", Object: domain.ObjectRef{Kind: "application", Code: "app"}, EnvCode: "dev", Model: "gpt-5.6-luna"}}
	id := EvidenceIdentity{EnvCode: "dev", ObjectCode: "app", ClusterID: "c", Namespace: "n", Name: "x", UID: "u"}
	result := &PublicResult{RequestID: "r", SelectedModel: r.Definition.Model, ActualModel: r.Definition.Model, GeneratedAt: &now, Evidence: []PublicEvidence{{Server: "raptor", Tool: "request_get", ObservedAt: now, EvidenceMode: "catalog", Identity: EvidenceIdentity{RequestID: "r", EnvCode: "dev", ObjectCode: "app"}}, {Server: "infra", Tool: "deployments_list", ObservedAt: now, EvidenceMode: "discovery", Identity: id}, {Server: "infra", Tool: "deployment_status_get", ObservedAt: now, EvidenceMode: "live", Identity: id}}}
	raw, _ := json.Marshal(result)
	var wire map[string]any
	json.Unmarshal(raw, &wire)
	wire["evidenceVersion"] = 2
	raw, _ = json.Marshal(wire)
	json.Unmarshal(raw, result)
	if !validQuestionResult(result, r) {
		t.Fatal("resource discovery incorrectly requires removed cloud tool")
	}
	result.Evidence[1].EvidenceMode = "live"
	if validQuestionResult(result, r) {
		t.Fatal("discovery time accepted as source live freshness")
	}
}

func TestResourceQualificationViewValidatesPrivateProofBeforeProjection(t *testing.T) {
	r := domain.Request{ID: "r", Definition: domain.RequestInput{Type: "agent", Object: domain.ObjectRef{Kind: "db-proxy", Code: "p"}, EnvCode: "dev", Model: "gpt-5.6-luna", Operations: []domain.Operation{{Name: "db-proxy.replace-nodes", Parameters: map[string]any{"mode": "qualification"}}}}}
	now := time.Now()
	scope := agentaccess.ReplacementScope{EnvCode: "dev", ProxyCode: "p", GroupID: "g", ServerGroupID: "s", TargetDBCode: "d", AccountID: "123", Region: "ap-southeast-1", ListenerID: "listener", Port: 6432, DatabaseResourceID: "rds", Application: agentaccess.ApplicationScope{EnvCode: "dev", AppCode: "a", ClusterID: "c", Namespace: "n", Name: "x", UID: "u"}}
	scope.ResolutionDigest = scope.Identity().Digest()
	q := &PublicQualification{Version: 2, TransportContract: "raptor-resource-v1", Passed: true, ReadOnly: true, TrafficAcceptance: "not_assessed", Scope: scope, Checks: []string{"mcp.raptor.request_get", "mcp.raptor.environment_get", "mcp.raptor.object_get", "mcp.raptor.deployments_list", "mcp.infra.deployments_list", "mcp.infra.deployment_status_get", "cloud.fleet", "cloud.backend", "cloud.capacity", "deployment.status", "metrics.trafficFailures", "metrics.connections", "metrics.pgcatClients"}, InstanceIDs: []string{"i1", "i2"}, ConnectedNodeIDs: []string{"i1", "i2"}, ApprovalID: "approval", ActionID: "action", BindingDigest: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", ObservedAt: now}
	v := PublicExecution{RequestID: "r", AttemptID: "a", RuntimeMode: "live", Status: "completed", CheckpointStatus: "verified", CleanupStatus: &PublicCleanup{true, true, true}, Result: &PublicResult{RequestID: "r", AttemptID: "a", ActualModel: r.Definition.Model, SelectedModel: r.Definition.Model, GeneratedAt: &now, Answer: "Native read-only qualification completed; replacement and traffic acceptance were not executed.", Qualification: q}}
	wire := func() map[string]any {
		raw, _ := json.Marshal(v)
		var m map[string]any
		json.Unmarshal(raw, &m)
		return m
	}
	out, err := publicExecution(wire(), r)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(out)
	for _, key := range []string{"accountId", "region", "listenerId", "port", "databaseResourceId", "resolutionDigest"} {
		if strings.Contains(string(raw), `"`+key+`"`) {
			t.Fatal("private proof spread into public qualification", key)
		}
	}
	if q.Scope.ResolutionDigest == "" {
		t.Fatal("projection mutated private result")
	}
	q.Scope.ListenerID = "retargeted"
	if _, err := publicExecution(wire(), r); err == nil {
		t.Fatal("bad private proof accepted before projection")
	}
}
