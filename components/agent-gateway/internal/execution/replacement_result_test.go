package execution

import (
	"bytes"
	"encoding/json"
	"os"
	"slices"
	"strings"
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

func TestDirectReplacementResultVersionDispatch(t *testing.T) {
	b := AttemptBinding{RequestID: "request", AttemptID: "attempt", DefinitionSHA256: stringRepeat("a", 64), SkillsCommit: stringRepeat("b", 40), ClusterID: "cluster", EnvCode: "rdev.ali", ObjectKind: "db-proxy", ObjectCode: "proxy", Operation: "db-proxy.replace-nodes", Model: "gpt-5.6-luna"}
	raw := `{"requestId":"request","attemptId":"attempt","selectedModel":"gpt-5.6-luna","actualModel":"gpt-5.6-luna","answer":"Replacement converged; retained traffic and approval assessment is pending.","generatedAt":"2026-10-29T00:00:00Z","replacement":{"version":2,"converged":true,"acceptance":"pending","scope":{"envCode":"rdev.ali","proxyCode":"proxy","groupId":"group","serverGroupId":"backend","targetDbCode":"db","application":{"envCode":"rdev.ali","appCode":"client","clusterId":"cluster","namespace":"test","name":"client","uid":"uid"}},"oldInstanceIds":["old-a","old-b"],"newInstanceIds":["new-a","new-b"],"desiredCapacity":2,"restartGeneration":6,"actions":[{"actionId":"expand","previousDesiredCapacity":2,"desiredCapacity":4,"outcome":"submitted"},{"actionId":"shrink","previousDesiredCapacity":4,"desiredCapacity":2,"outcome":"unknown"}],"observedAt":"2026-10-29T00:00:00Z"},"evidence":[],"usage":[]}`
	for _, change := range []string{"valid", "pass", "duplicate-id", "duplicate-action", "legacy-version", "mixed-stages", "unsupported-version", "missing-generation", "legacy-stages", "negative-generation", "unsafe-generation"} {
		var r LiveResult
		if err := json.Unmarshal([]byte(raw), &r); err != nil {
			t.Fatal(err)
		}
		switch change {
		case "pass":
			r.Replacement.Acceptance = "PASS"
		case "duplicate-id":
			r.Replacement.NewInstanceIDs[1] = "new-a"
		case "duplicate-action":
			r.Replacement.Actions[1].ActionID = "expand"
		case "legacy-version":
			r.Replacement.Version = 1
		case "mixed-stages":
			r.Replacement.Actions[1].Desired = 3
		case "legacy-stages":
			r.Replacement.Actions = []ReplacementScaleAction{{ActionID: "expand", Previous: 2, Desired: 4, Outcome: "submitted"}, {ActionID: "shrink-a", Previous: 4, Desired: 3, Outcome: "submitted"}, {ActionID: "shrink-b", Previous: 3, Desired: 2, Outcome: "unknown"}}
		case "negative-generation":
			r.Replacement.RestartGeneration = -1
		case "unsafe-generation":
			r.Replacement.RestartGeneration = 9007199254740992
		case "unsupported-version":
			r.Replacement.Version = 3
		case "missing-generation":
			r = LiveResult{}
			if err := json.Unmarshal([]byte(strings.Replace(raw, `"restartGeneration":6,`, "", 1)), &r); err != nil {
				t.Fatal(err)
			}
		}
		err := ValidateLiveResult(r, b)
		if change == "valid" && err != nil {
			t.Fatal("direct v2 rejected", err)
		}
		if change != "valid" && err == nil {
			t.Fatal("accepted", change)
		}
	}
}

func directHarnessFixture(t *testing.T) (AttemptBinding, LiveResult) {
	t.Helper()
	raw, err := os.ReadFile("testdata/replacement-direct-v2.json")
	if err != nil {
		t.Fatal(err)
	}
	var r LiveResult
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(&r); err != nil {
		t.Fatal(err)
	}
	b := AttemptBinding{RequestID: r.RequestID, AttemptID: r.AttemptID, DefinitionSHA256: stringRepeat("a", 64), SkillsCommit: stringRepeat("b", 40), ClusterID: "cluster", EnvCode: "rdev.ali", ObjectKind: "db-proxy", ObjectCode: "proxy", Operation: "db-proxy.replace-nodes", Model: r.SelectedModel}
	return b, r
}
func TestActualHarnessDirectFixtureSurvivesGatewayRoundTrip(t *testing.T) {
	b, r := directHarnessFixture(t)
	if err := ValidateLiveResult(r, b); err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	var stored LiveResult
	if err = json.Unmarshal(raw, &stored); err != nil {
		t.Fatal(err)
	}
	if err = ValidateLiveResult(stored, b); err != nil {
		t.Fatal(err)
	}
	if stored.Replacement.RestartGeneration != 6 || stored.Replacement.Version != 2 || len(stored.Replacement.Actions) != 2 {
		t.Fatal("direct contract lost", string(raw))
	}
}

func TestGrantedReplacementRequiresVerifiedTrafficAndExactRetirement(t *testing.T) {
	b, original := directHarnessFixture(t)
	b.ApprovalProtocol, b.ExecutionMode = "request-grants-v1", "replacement"
	for _, fault := range []string{"", "old pending contract", "uncertain submission", "wrong ESS activity", "wrong original", "missing app traffic", "late final result"} {
		t.Run(fault, func(t *testing.T) {
			now := time.Now().UTC()
			r := original
			v := *r.Replacement
			r.Replacement, r.Answer = &v, PgcatAcceptanceAnswer
			v.Version, v.Acceptance, v.ObservedAt = 3, "verified", now
			v.Actions = slices.Clone(v.Actions)
			v.Actions[1].Outcome, v.Actions[1].ScalingActivityID = "submitted", "ess-act"
			v.Retirement = &PgcatResultRetirement{ActionID: v.Actions[1].ActionID, ScalingActivityID: "ess-act", Status: "Successful",
				DestroyedIDs: slices.Clone(v.OldInstanceIDs), ObservedAt: now.Add(-5 * time.Second)}
			traffic := &PgcatResultTraffic{RestartActionID: "restart", ApplicationUID: v.Scope.Application.UID, Generation: v.RestartGeneration,
				ObservedAt: now.Add(-time.Minute), NoErrors: true}
			counters := `{"` + v.NewInstanceIDs[0] + `":{"queries":4,"transactions":4},"` + v.NewInstanceIDs[1] + `":{"queries":3,"transactions":3}}`
			if err := json.Unmarshal([]byte(counters), &traffic.Nodes); err != nil {
				t.Fatal(err)
			}
			v.Traffic = traffic
			final := *traffic
			final.ObservedAt = now.Add(-time.Second)
			v.FinalTraffic = &final
			switch fault {
			case "old pending contract":
				v.Version = 2
			case "uncertain submission":
				v.Actions[1].Outcome = "unknown"
			case "wrong ESS activity":
				v.Retirement.ScalingActivityID = "unrelated"
			case "wrong original":
				v.Retirement.DestroyedIDs[1] = v.NewInstanceIDs[0]
			case "missing app traffic":
				delete(final.Nodes, v.NewInstanceIDs[1])
			case "late final result":
				v.ObservedAt = now.Add(time.Minute)
			}
			if err := ValidateLiveResult(r, b); (err != nil) != (fault != "") {
				t.Fatalf("validation of %q: %v", fault, err)
			}
		})
	}
}
