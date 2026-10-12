package requests

import (
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/agentaccess"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/domain"
	"reflect"
	"regexp"
	"sort"
	"time"
)

type PublicQualification struct {
	Version           int                          `json:"version"`
	Passed            bool                         `json:"passed"`
	ReadOnly          bool                         `json:"readOnly"`
	TrafficAcceptance string                       `json:"trafficAcceptance"`
	Scope             agentaccess.ReplacementScope `json:"scope"`
	Checks            []string                     `json:"checks"`
	InstanceIDs       []string                     `json:"instanceIds"`
	ConnectedNodeIDs  []string                     `json:"connectedNodeIds"`
	ApprovalID        string                       `json:"approvalId"`
	ActionID          string                       `json:"actionId"`
	BindingDigest     string                       `json:"bindingDigest"`
	ObservedAt        time.Time                    `json:"observedAt"`
}

func isQualification(d domain.RequestInput) bool {
	return d.Type == "agent" && d.Object.Kind == "db-proxy" && len(d.Operations) == 1 && d.Operations[0].Name == "db-proxy.replace-nodes" && d.Operations[0].Parameters["mode"] == "qualification"
}
func validQualificationView(v PublicExecution, r domain.Request) bool {
	if !isQualification(r.Definition) || v.Result == nil {
		return false
	}
	q := v.Result.Qualification
	if q == nil || q.Version != 1 || !q.Passed || !q.ReadOnly || q.TrafficAcceptance != "not_assessed" || q.ObservedAt.IsZero() || v.Result.GeneratedAt == nil || v.Result.GeneratedAt.IsZero() || v.Result.SelectedModel != r.Definition.Model || v.Result.ActualModel != r.Definition.Model || v.Result.Answer != "Native read-only qualification completed; replacement and traffic acceptance were not executed." || len(q.InstanceIDs) != 2 || len(q.ConnectedNodeIDs) != 2 || q.InstanceIDs[0] == q.InstanceIDs[1] || q.ApprovalID == "" || q.ActionID == "" || !regexp.MustCompile(`^[a-f0-9]{64}$`).MatchString(q.BindingDigest) {
		return false
	}
	b := agentaccess.Binding{Operation: "db-proxy.replace-nodes", ObjectKind: r.Definition.Object.Kind, ObjectCode: r.Definition.Object.Code, EnvCode: r.Definition.EnvCode, ClusterID: q.Scope.Application.ClusterID, ExecutionMode: "qualification"}
	if !q.Scope.ValidFor(b) {
		return false
	}
	sorted := func(s []string) []string { c := append([]string{}, s...); sort.Strings(c); return c }
	checks := []string{"mcp.raptor.request_get", "mcp.raptor.environment_get", "mcp.raptor.object_get", "mcp.infra.cloud_identity_get", "mcp.infra.deployments_list", "mcp.infra.deployment_status_get", "cloud.fleet", "cloud.backend", "cloud.capacity", "deployment.status", "metrics.trafficFailures", "metrics.connections", "metrics.pgcatClients"}
	if !reflect.DeepEqual(sorted(checks), sorted(q.Checks)) || !reflect.DeepEqual(sorted(q.InstanceIDs), sorted(q.ConnectedNodeIDs)) {
		return false
	}
	return v.Status != "completed" || (v.CheckpointStatus == "verified" && v.CleanupStatus.confirmed() && !v.RecoveryNeeded)
}
