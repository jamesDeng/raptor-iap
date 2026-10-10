package runtime

import (
	"encoding/json"
	"github.com/jamesDeng/raptor-iap/components/agent-gateway/internal/execution"
	"strings"
	"testing"
	"time"
)

func TestPausedTerminalRequiresBoundSessionAndExclusiveOutcome(t *testing.T) {
	b := execution.AttemptBinding{RequestID: "request", AttemptID: "attempt", Operation: "db-proxy.replace-nodes", ObjectKind: "db-proxy", DefinitionSHA256: strings.Repeat("a", 64), SkillsCommit: strings.Repeat("b", 40), Model: "gpt-5.6-luna"}
	base := func() map[string]any {
		return map[string]any{"phase": "live-inference", "passed": false, "paused": map[string]any{"kind": "approval", "approvalId": "33333333-3333-4333-8333-333333333333", "actionId": "scale-three", "bindingDigest": strings.Repeat("c", 64), "requestId": b.RequestID, "definitionSha256": b.DefinitionSHA256, "startedAt": time.Now().UTC().Format(time.RFC3339Nano)}, "sessionReference": map[string]any{"version": 1, "requestId": b.RequestID, "definitionSha256": b.DefinitionSHA256, "skillsCommit": b.SkillsCommit, "model": b.Model, "sessionId": "session", "file": "session.jsonl", "sha256": strings.Repeat("d", 64)}, "usage": []map[string]any{{"input": 3, "output": 2, "totalTokens": 5}}}
	}
	raw, _ := json.Marshal(base())
	if _, err := decodeTerminal(raw, b, nil); err != nil {
		t.Fatalf("valid pause rejected: %v", err)
	}
	for _, kind := range []string{"foreign", "missing-session", "error", "passed", "question", "negative-usage", "future", "bad-action"} {
		row := base()
		binding := b
		switch kind {
		case "foreign":
			row["paused"].(map[string]any)["requestId"] = "other"
		case "missing-session":
			delete(row, "sessionReference")
		case "error":
			row["error"] = "Timeout"
		case "passed":
			row["passed"] = true
		case "question":
			binding.Operation = "application.question"
		case "negative-usage":
			row["usage"] = []map[string]any{{"input": -1, "output": 2, "totalTokens": 1}}
		case "future":
			row["paused"].(map[string]any)["startedAt"] = time.Now().Add(time.Hour).Format(time.RFC3339Nano)
		case "bad-action":
			row["paused"].(map[string]any)["actionId"] = "../foreign"
		}
		raw, _ = json.Marshal(row)
		if _, err := decodeTerminal(raw, binding, nil); err == nil {
			t.Errorf("accepted %s pause", kind)
		}
	}
}

func TestPauseObservationCannotBeTerminalCompletion(t *testing.T) {
	paused := &pausedFile{LiveWait: execution.LiveWait{Kind: "approval", ApprovalID: "33333333-3333-4333-8333-333333333333"}}
	out := terminalFile{Paused: paused, Usage: []execution.Usage{{Input: 3, Output: 2, TotalTokens: 5}}}.observation()
	if out.Terminal || out.Result != nil || out.FailureCode != "" || out.Paused == nil || out.Paused.ApprovalID != paused.ApprovalID || len(out.Usage) != 1 {
		t.Fatalf("pause misclassified: %+v", out)
	}
	failure := terminalFile{Error: "Timeout"}.observation()
	if !failure.Terminal || failure.FailureCode != "Timeout" || failure.Paused != nil {
		t.Fatal("failure classification changed")
	}
}

func TestResourceTerminalBindsWaitToRequestDefinition(t *testing.T) {
	b := execution.AttemptBinding{RequestID: "request", AttemptID: "attempt", Operation: "db-proxy.replace-nodes", ObjectKind: "db-proxy", DefinitionSHA256: strings.Repeat("a", 64), SkillsCommit: strings.Repeat("b", 40), Model: "gpt-5.6-luna"}
	row := map[string]any{"phase": "live-inference", "passed": false, "paused": map[string]any{"kind": "resource", "approvalId": "", "actionId": "capacity-ready", "bindingDigest": b.DefinitionSHA256, "requestId": b.RequestID, "definitionSha256": b.DefinitionSHA256, "startedAt": time.Now().UTC().Format(time.RFC3339Nano), "wakeAfterSeconds": 60}, "sessionReference": map[string]any{"version": 1, "requestId": b.RequestID, "definitionSha256": b.DefinitionSHA256, "skillsCommit": b.SkillsCommit, "model": b.Model, "sessionId": "session", "file": "session.jsonl", "sha256": strings.Repeat("d", 64)}}
	raw, _ := json.Marshal(row)
	if _, err := decodeTerminal(raw, b, nil); err != nil {
		t.Fatal(err)
	}
	row["paused"].(map[string]any)["bindingDigest"] = strings.Repeat("f", 64)
	raw, _ = json.Marshal(row)
	if _, err := decodeTerminal(raw, b, nil); err == nil {
		t.Fatal("accepted foreign definition digest")
	}
}
