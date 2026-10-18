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
	if _, _, e = ParseLiveOperation(in, "attempt"); e != nil {
		t.Fatal("configured GPT-6 Luna replacement rejected", e)
	}
	d["model"] = "gpt-6-sol"
	in.Definition, _ = json.Marshal(d)
	h = sha256.Sum256(in.Definition)
	in.DefinitionSHA256 = hex.EncodeToString(h[:])
	if _, _, e = ParseLiveOperation(in, "attempt"); e == nil {
		t.Fatal("unapproved replacement model accepted")
	}
}

func TestReplacementAcceptsPinnedGPT6CodexModels(t *testing.T) {
	for _, model := range []string{"gpt-6-luna", "gpt-6.1-sol"} {
		t.Run(model, func(t *testing.T) {
			in := replacementInput(t)
			var d map[string]any
			json.Unmarshal(in.Definition, &d)
			d["model"], d["providerId"], d["connectionVersion"] = model, "codex", float64(3)
			d["operations"].([]any)[0].(map[string]any)["parameters"] = map[string]any{"mode": "replacement"}
			in.Definition, _ = json.Marshal(d)
			h := sha256.Sum256(in.Definition)
			in.DefinitionSHA256 = hex.EncodeToString(h[:])
			b, _, err := ParseLiveOperation(in, "attempt")
			if err != nil || !b.journalValid() || !b.SessionModelAllowed() {
				t.Fatalf("pinned replacement model rejected: %v", err)
			}
			b.ConnectionVersion = 0
			if b.journalValid() || b.SessionModelAllowed() {
				t.Fatal("unbound model accepted")
			}
			b.ConnectionVersion, b.Model = 3, "gpt-6-sol"
			if b.journalValid() || b.SessionModelAllowed() {
				t.Fatal("unapproved replacement model accepted")
			}
		})
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
	cp.Model = b.Model
	if !b.SessionModelAllowed() || !cp.validFor(b) {
		t.Fatal("configured GPT-6 Luna replacement checkpoint rejected")
	}
	b.Model = "gpt-6-sol"
	if b.SessionModelAllowed() || cp.validFor(b) {
		t.Fatal("unapproved replacement checkpoint accepted")
	}
}
