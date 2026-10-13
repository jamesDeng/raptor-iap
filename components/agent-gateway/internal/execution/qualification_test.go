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

func TestQualificationAcceptsConfiguredCurrentModel(t *testing.T) {
	in := qualificationInput(t)
	var d map[string]any
	json.Unmarshal(in.Definition, &d)
	d["model"] = "gpt-6-luna"
	d["providerId"] = "codex"
	d["connectionVersion"] = float64(3)
	in.Definition, _ = json.Marshal(d)
	h := sha256.Sum256(in.Definition)
	in.DefinitionSHA256 = hex.EncodeToString(h[:])
	b, _, e := ParseLiveOperation(in, "attempt")
	if e != nil || b.Model != "gpt-6-luna" || !b.journalValid() {
		t.Fatal("configured qualification model rejected", e)
	}
	d["operations"].([]any)[0].(map[string]any)["parameters"] = map[string]any{"mode": "replacement"}
	in.Definition, _ = json.Marshal(d)
	h = sha256.Sum256(in.Definition)
	in.DefinitionSHA256 = hex.EncodeToString(h[:])
	if _, _, e = ParseLiveOperation(in, "attempt"); e == nil {
		t.Fatal("replacement model restriction broadened")
	}
}

func TestQualificationCheckpointAcceptsSelectedConfiguredModel(t *testing.T) {
	b, _ := qualificationResultFixture()
	b.Model = "gpt-6-luna"
	b.ProviderID = "codex"
	b.ConnectionVersion = 3
	cp := SessionCheckpoint{RequestID: b.RequestID, DefinitionSHA256: b.DefinitionSHA256, SkillsCommit: b.SkillsCommit, Model: b.Model, SessionID: "session", SessionFile: "session.jsonl", SessionSHA256: stringRepeat("b", 64), Archive: verifiedCheckpoint()}
	if !b.SessionModelAllowed() || !cp.validFor(b) {
		t.Fatal("qualification checkpoint rejects selected model")
	}
	cp.Model = "gpt-6.1-sol"
	if cp.validFor(b) {
		t.Fatal("changed model checkpoint accepted")
	}
	b.ExecutionMode = "replacement"
	if b.SessionModelAllowed() {
		t.Fatal("replacement session restriction broadened")
	}
}
