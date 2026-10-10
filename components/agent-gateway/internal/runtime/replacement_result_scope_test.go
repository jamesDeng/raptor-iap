package runtime

import (
	"github.com/jamesDeng/raptor-iap/components/agent-gateway/internal/execution"
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
