package execution

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"testing"
)

func replacementInput(t *testing.T) ExecutionInput {
	t.Helper()
	in := producerInput(t)
	var d map[string]any
	if err := json.Unmarshal(in.Definition, &d); err != nil {
		t.Fatal(err)
	}
	d["object"] = map[string]any{"kind": "db-proxy", "code": "proxy"}
	d["operations"] = []any{map[string]any{"name": "db-proxy.replace-nodes", "parameters": map[string]any{}}}
	in.Definition, _ = json.Marshal(d)
	sum := sha256.Sum256(in.Definition)
	in.DefinitionSHA256 = hex.EncodeToString(sum[:])
	return in
}

func TestParseLiveReplacementOperation(t *testing.T) {
	in := replacementInput(t)
	b, op, err := ParseLiveOperation(in, "attempt")
	if err != nil || b.Operation != "db-proxy.replace-nodes" || b.ObjectKind != "db-proxy" || op.Question != "" || len(op.Parameters) != 0 {
		t.Fatalf("replacement rejected: %+v %+v %v", b, op, err)
	}
	if _, _, _, err := ParseLiveQuestion(in, "attempt"); err == nil {
		t.Fatal("question adapter accepted mutation")
	}
}

func TestParseLiveOperationPreservesQuestion(t *testing.T) {
	in := producerInput(t)
	b, op, err := ParseLiveOperation(in, "attempt")
	old, q, _, oldErr := ParseLiveQuestion(in, "attempt")
	if err != nil || oldErr != nil || b != old || op.Question != q {
		t.Fatal("question contract changed", err, oldErr)
	}
}

func TestParseLiveOperationRejectsInvalidDefinition(t *testing.T) {
	for _, which := range []string{"hash", "skills", "environment", "kind", "operation", "model", "parameters", "two", "definition-field"} {
		t.Run(which, func(t *testing.T) {
			in := replacementInput(t)
			var d map[string]any
			json.Unmarshal(in.Definition, &d)
			operation := d["operations"].([]any)[0].(map[string]any)
			switch which {
			case "hash":
				in.DefinitionSHA256 = "bad"
			case "skills":
				in.Skills.CommitSHA = "bad"
			case "environment":
				in.Environment = json.RawMessage(`{}`)
			case "kind":
				d["object"].(map[string]any)["kind"] = "database"
			case "operation":
				operation["name"] = "db-proxy.delete"
			case "model":
				d["model"] = "other"
			case "parameters":
				operation["parameters"] = map[string]any{"groupId": "foreign"}
			case "two":
				d["operations"] = append(d["operations"].([]any), operation)
			case "definition-field":
				d["untrusted"] = "extra"
			}
			in.Definition, _ = json.Marshal(d)
			if which != "hash" {
				sum := sha256.Sum256(in.Definition)
				in.DefinitionSHA256 = hex.EncodeToString(sum[:])
			}
			if _, _, err := ParseLiveOperation(in, "attempt"); err == nil {
				t.Fatal("invalid operation accepted")
			}
		})
	}
}

func TestReplacementRejectsUnsupportedModelCredentialBinding(t *testing.T) {
	for _, provider := range []string{"foreign", "codex", ""} {
		t.Run(provider, func(t *testing.T) {
			in := replacementInput(t)
			var d map[string]any
			json.Unmarshal(in.Definition, &d)
			d["providerId"] = provider
			d["connectionVersion"] = 0
			if provider == "" {
				d["connectionVersion"] = 1
			}
			in.Definition, _ = json.Marshal(d)
			sum := sha256.Sum256(in.Definition)
			in.DefinitionSHA256 = hex.EncodeToString(sum[:])
			if _, _, err := ParseLiveOperation(in, "attempt"); err == nil {
				t.Fatal("unsupported credential binding admitted")
			}
		})
	}
}
