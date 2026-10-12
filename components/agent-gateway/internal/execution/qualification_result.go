package execution

import (
	"reflect"
	"sort"
	"time"
)

const QualificationAnswer = "Native read-only qualification completed; replacement and traffic acceptance were not executed."

var qualificationChecks = []string{"mcp.raptor.request_get", "mcp.raptor.environment_get", "mcp.raptor.object_get", "mcp.infra.cloud_identity_get", "mcp.infra.deployments_list", "mcp.infra.deployment_status_get", "cloud.fleet", "cloud.backend", "cloud.capacity", "deployment.status", "metrics.trafficFailures", "metrics.connections", "metrics.pgcatClients"}

type QualificationResult struct {
	Version           int              `json:"version"`
	Passed            bool             `json:"passed"`
	ReadOnly          bool             `json:"readOnly"`
	TrafficAcceptance string           `json:"trafficAcceptance"`
	Scope             ReplacementScope `json:"scope"`
	Checks            []string         `json:"checks"`
	InstanceIDs       []string         `json:"instanceIds"`
	ConnectedNodeIDs  []string         `json:"connectedNodeIds"`
	ApprovalID        string           `json:"approvalId"`
	ActionID          string           `json:"actionId"`
	BindingDigest     string           `json:"bindingDigest"`
	ObservedAt        time.Time        `json:"observedAt"`
}

func validateQualificationResult(r LiveResult, b AttemptBinding) error {
	q := r.Qualification
	if q == nil || r.Replacement != nil || b.ExecutionMode != "qualification" || q.Version != 1 || !q.Passed || !q.ReadOnly || q.TrafficAcceptance != "not_assessed" || !q.Scope.ValidFor(b) || r.Answer != QualificationAnswer || !journalName.MatchString(q.ApprovalID) || !journalName.MatchString(q.ActionID) || !shaPattern.MatchString(q.BindingDigest) || q.ObservedAt.IsZero() || time.Since(q.ObservedAt) > 120*time.Second || q.ObservedAt.After(time.Now().Add(2*time.Second)) || len(q.InstanceIDs) != 2 || len(q.ConnectedNodeIDs) != 2 || len(r.Evidence) != 0 {
		return ErrInvalid
	}
	for _, ids := range [][]string{q.InstanceIDs, q.ConnectedNodeIDs} {
		if !journalName.MatchString(ids[0]) || !journalName.MatchString(ids[1]) || ids[0] == ids[1] {
			return ErrInvalid
		}
	}
	sorted := func(s []string) []string { v := append([]string{}, s...); sort.Strings(v); return v }
	if !reflect.DeepEqual(sorted(q.InstanceIDs), sorted(q.ConnectedNodeIDs)) || !reflect.DeepEqual(sorted(q.Checks), sorted(qualificationChecks)) {
		return ErrInvalid
	}
	return validateUsage(r.Usage)
}
func resultScope(r *LiveResult) *ReplacementScope {
	if r == nil {
		return nil
	}
	if r.Qualification != nil {
		return &r.Qualification.Scope
	}
	if r.Replacement != nil {
		return &r.Replacement.Scope
	}
	return nil
}
