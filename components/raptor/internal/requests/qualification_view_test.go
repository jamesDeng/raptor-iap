package requests

import (
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/agentaccess"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/domain"
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
