package execution

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"testing"
)

func qualificationInput(t *testing.T) ExecutionInput {
	in := replacementInput(t)
	var d map[string]any
	json.Unmarshal(in.Definition, &d)
	d["operations"].([]any)[0].(map[string]any)["parameters"] = map[string]any{"mode": "qualification"}
	in.Definition, _ = json.Marshal(d)
	h := sha256.Sum256(in.Definition)
	in.DefinitionSHA256 = hex.EncodeToString(h[:])
	return in
}
func TestQualificationHasIndependentReadinessAndImmutableBinding(t *testing.T) {
	in := qualificationInput(t)
	b, _, e := ParseLiveOperation(in, "attempt")
	if e != nil || b.ExecutionMode != "qualification" {
		t.Fatal("mode not bound", e)
	}
	scope := ReplacementScope{EnvCode: b.EnvCode, ProxyCode: b.ObjectCode, GroupID: "group", ServerGroupID: "backend", TargetDBCode: "db", Application: ApplicationScope{EnvCode: b.EnvCode, AppCode: "client", ClusterID: b.ClusterID, Namespace: "ns", Name: "client", UID: "uid"}}
	w := LiveWorker{ReplacementScope: &scope}
	if _, _, _, e = w.admit(in, "attempt"); e == nil {
		t.Fatal("replacement enablement allowed qualification")
	}
	w.QualificationScope = &scope
	w.ReplacementScope = nil
	if _, _, _, e = w.admit(in, "attempt"); e != nil {
		t.Fatal("qualification disabled", e)
	}
	if _, _, _, e = w.admit(replacementInput(t), "attempt"); e == nil {
		t.Fatal("qualification enabled mutations")
	}
	old := in.DefinitionSHA256
	var d map[string]any
	json.Unmarshal(in.Definition, &d)
	d["operations"].([]any)[0].(map[string]any)["parameters"] = map[string]any{"mode": "replacement"}
	in.Definition, _ = json.Marshal(d)
	in.DefinitionSHA256 = old
	if _, _, e = ParseLiveOperation(in, "attempt"); e == nil {
		t.Fatal("mode tampering accepted")
	}
}
