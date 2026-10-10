package runtime

import (
	"encoding/json"
	"github.com/jamesDeng/raptor-iap/components/agent-gateway/internal/execution"
	"strings"
	"testing"
)

func TestOperationHarnessModulesArePackaged(t *testing.T) {
	files := map[string]bool{}
	for _, name := range harnessFiles {
		files[name] = true
	}
	for _, name := range []string{"operation-profile.mjs", "replacement-evidence.mjs", "operation-session.mjs", "skills-loader.mjs", "live-operation.mjs", "approval-pause.mjs", "live-replacement.mjs", "skills-bundle.mjs"} {
		if !files[name] {
			t.Errorf("runtime dependency missing: %s", name)
		}
	}
}
func TestLiveJobCarriesOnlyMatchingRequestSessionReference(t *testing.T) {
	b := execution.AttemptBinding{RequestID: "request", DefinitionSHA256: strings.Repeat("a", 64), SkillsCommit: strings.Repeat("b", 40), Model: "gpt-5.6-luna"}
	cp := execution.SessionCheckpoint{RequestID: b.RequestID, DefinitionSHA256: b.DefinitionSHA256, SkillsCommit: b.SkillsCommit, Model: b.Model, SessionID: "session", SessionFile: "session.jsonl", SessionSHA256: strings.Repeat("c", 64)}
	raw, e := liveJob(LiveConfig{}, execution.LiveStart{Binding: b, SessionCheckpoint: &cp})
	if e != nil {
		t.Fatal(e)
	}
	var job map[string]any
	json.Unmarshal(raw, &job)
	ref, ok := job["session_reference"].(map[string]any)
	if !ok || ref["requestId"] != b.RequestID || ref["version"] != float64(1) {
		t.Fatal("missing session reference")
	}
	cp.RequestID = "foreign"
	if _, e = liveJob(LiveConfig{}, execution.LiveStart{Binding: b, SessionCheckpoint: &cp}); e == nil {
		t.Fatal("foreign session reference admitted")
	}
}
