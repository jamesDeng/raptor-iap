package execution

import (
	"testing"
	"time"
)

func qualificationResultFixture() (AttemptBinding, LiveResult) {
	b := AttemptBinding{RequestID: "r", AttemptID: "a", Operation: "db-proxy.replace-nodes", ObjectKind: "db-proxy", ObjectCode: "p", EnvCode: "dev", ClusterID: "c", Model: "gpt-5.6-luna", SkillsCommit: stringRepeat("b", 40), DefinitionSHA256: stringRepeat("a", 64), ExecutionMode: "qualification"}
	s := ReplacementScope{EnvCode: "dev", ProxyCode: "p", GroupID: "g", ServerGroupID: "s", TargetDBCode: "d", Application: ApplicationScope{EnvCode: "dev", AppCode: "a", ClusterID: "c", Namespace: "n", Name: "x", UID: "u"}}
	q := &QualificationResult{Version: 1, Passed: true, ReadOnly: true, TrafficAcceptance: "not_assessed", Scope: s, Checks: append([]string{}, qualificationChecks...), InstanceIDs: []string{"i1", "i2"}, ConnectedNodeIDs: []string{"i1", "i2"}, ApprovalID: "approval", ActionID: "action", BindingDigest: stringRepeat("c", 64), ObservedAt: time.Now()}
	return b, LiveResult{RequestID: b.RequestID, AttemptID: b.AttemptID, SelectedModel: b.Model, ActualModel: b.Model, Answer: QualificationAnswer, GeneratedAt: time.Now(), Qualification: q}
}
func TestQualificationResultCannotMasqueradeAsReplacement(t *testing.T) {
	b, r := qualificationResultFixture()
	if e := ValidateLiveResult(r, b); e != nil {
		t.Fatal(e)
	}
	b.ExecutionMode = "replacement"
	if ValidateLiveResult(r, b) == nil {
		t.Fatal("qualification accepted as replacement")
	}
	b.ExecutionMode = "qualification"
	for _, what := range []string{"missing", "traffic-pass", "foreign", "stale", "scope", "answer"} {
		bad := r
		q := *r.Qualification
		q.Checks = append([]string{}, q.Checks...)
		bad.Qualification = &q
		switch what {
		case "missing":
			q.Checks = q.Checks[:1]
		case "traffic-pass":
			q.TrafficAcceptance = "PASS"
		case "foreign":
			q.ConnectedNodeIDs = []string{"i1", "other"}
		case "stale":
			q.ObservedAt = time.Now().Add(-time.Hour)
		case "scope":
			q.Scope.ProxyCode = "foreign"
		case "answer":
			bad.Answer = ReplacementConvergenceAnswer
		}
		if ValidateLiveResult(bad, b) == nil {
			t.Fatal("invalid report", what)
		}
	}
}
