package runtime

import (
	"encoding/json"
	"github.com/jamesDeng/raptor-iap/components/agent-gateway/internal/execution"
	"os"
	"strings"
	"testing"
)

func TestReplacementTerminalScopeMatchesController(t *testing.T) {
	scope := execution.ReplacementScope{GroupID: "group", ServerGroupID: "backend", TargetDBCode: "db", Application: execution.ApplicationScope{UID: "uid"}}
	terminal := terminalFile{Result: &execution.LiveResult{Replacement: &execution.ReplacementConvergence{Scope: scope}}}
	if checkTerminalScope(terminal, &scope) != nil {
		t.Fatal("matching scope refused")
	}
	if checkTerminalScope(terminal, nil) == nil {
		t.Fatal("missing controller scope accepted")
	}
	foreign := scope
	foreign.ServerGroupID = "foreign"
	if checkTerminalScope(terminal, &foreign) == nil {
		t.Fatal("foreign backend accepted")
	}
}

func TestNativeDirectTerminalPreservesGenerationAndControllerScope(t *testing.T) {
	raw, err := os.ReadFile("../execution/testdata/replacement-direct-v2.json")
	if err != nil {
		t.Fatal(err)
	}
	var result execution.LiveResult
	if err = json.Unmarshal(raw, &result); err != nil {
		t.Fatal(err)
	}
	b := execution.AttemptBinding{RequestID: "request", AttemptID: "attempt", DefinitionSHA256: strings.Repeat("a", 64), SkillsCommit: strings.Repeat("b", 40), ClusterID: "cluster", EnvCode: "rdev.ali", ObjectKind: "db-proxy", ObjectCode: "proxy", Operation: "db-proxy.replace-nodes", Model: "gpt-5.6-luna"}
	envelope := map[string]any{"phase": "live-inference", "passed": true, "result": result, "sessionReference": map[string]any{"version": 1, "requestId": b.RequestID, "definitionSha256": b.DefinitionSHA256, "skillsCommit": b.SkillsCommit, "model": b.Model, "sessionId": "session", "file": "session.jsonl", "sha256": strings.Repeat("d", 64)}}
	encoded, err := json.Marshal(envelope)
	if err != nil {
		t.Fatal(err)
	}
	terminal, err := decodeTerminal(encoded, b, nil)
	if err != nil {
		t.Fatal(err)
	}
	if terminal.Result.Replacement.RestartGeneration != 6 {
		t.Fatal("actual restart generation lost")
	}
	if err = checkTerminalScope(terminal, &result.Replacement.Scope); err != nil {
		t.Fatal(err)
	}
	wrong := result.Replacement.Scope
	wrong.Application.UID = "foreign"
	if checkTerminalScope(terminal, &wrong) == nil {
		t.Fatal("wrong application UID accepted")
	}
}
