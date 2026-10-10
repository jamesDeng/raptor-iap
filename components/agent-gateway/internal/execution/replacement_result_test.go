package execution

import (
	"testing"
	"time"
)

func TestReplacementConvergenceResultIsPendingAcceptance(t *testing.T) {
	b := AttemptBinding{RequestID: "request", AttemptID: "attempt", DefinitionSHA256: stringRepeat("a", 64), SkillsCommit: stringRepeat("b", 40), ClusterID: "cluster", EnvCode: "rdev.ali", ObjectKind: "db-proxy", ObjectCode: "proxy", Operation: "db-proxy.replace-nodes", Model: "gpt-5.6-luna"}
	scope := ReplacementScope{EnvCode: b.EnvCode, ProxyCode: b.ObjectCode, GroupID: "group", ServerGroupID: "backend", TargetDBCode: "db", Application: ApplicationScope{EnvCode: b.EnvCode, AppCode: "client", ClusterID: b.ClusterID, Namespace: "test", Name: "client", UID: "uid"}}
	r := LiveResult{RequestID: b.RequestID, AttemptID: b.AttemptID, SelectedModel: b.Model, ActualModel: b.Model, Answer: ReplacementConvergenceAnswer, GeneratedAt: time.Now(), Replacement: &ReplacementConvergence{Version: 1, Converged: true, Acceptance: "pending", Scope: scope, OldInstanceIDs: []string{"old-a", "old-b"}, NewInstanceIDs: []string{"new-a", "new-b"}, DesiredCapacity: 2, ObservedAt: time.Now(), Actions: []ReplacementScaleAction{{ActionID: "expand", Previous: 2, Desired: 4, Outcome: "submitted"}, {ActionID: "shrink-a", Previous: 4, Desired: 3, Outcome: "submitted"}, {ActionID: "shrink-b", Previous: 3, Desired: 2, Outcome: "unknown"}}}}
	if e := ValidateLiveResult(r, b); e != nil {
		t.Fatal(e)
	}
	for _, change := range []string{"pass", "old", "sequence", "scope", "answer"} {
		copy := *r.Replacement
		copy.NewInstanceIDs = append([]string(nil), copy.NewInstanceIDs...)
		copy.Actions = append([]ReplacementScaleAction(nil), copy.Actions...)
		bad := r
		bad.Replacement = &copy
		switch change {
		case "pass":
			copy.Acceptance = "PASS"
		case "old":
			copy.NewInstanceIDs[0] = "old-a"
		case "sequence":
			copy.Actions[1].Desired = 2
		case "scope":
			copy.Scope.Application.ClusterID = "foreign"
		case "answer":
			bad.Answer = "Everything passed"
		}
		if ValidateLiveResult(bad, b) == nil {
			t.Fatal("accepted", change)
		}
	}
}
func stringRepeat(s string, n int) string {
	out := ""
	for i := 0; i < n; i++ {
		out += s
	}
	return out
}
