package execution

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"unicode/utf8"
)

// LiveOperationInput contains validated catalog data, never runtime credentials.
// Resolving authoritative deployment identities is a separate preflight step.
type LiveOperationInput struct {
	Definition  json.RawMessage            `json:"definition"`
	Parameters  map[string]json.RawMessage `json:"parameters"`
	Environment json.RawMessage            `json:"environment"`
	Question    string                     `json:"question,omitempty"`
}

func ParseLiveOperation(in ExecutionInput, attempt string) (AttemptBinding, LiveOperationInput, error) {
	var d struct {
		Type              string `json:"type"`
		Model             string `json:"model"`
		ProviderID        string `json:"providerId,omitempty"`
		ConnectionVersion int64  `json:"connectionVersion,omitempty"`
		Object            struct {
			Kind string `json:"kind"`
			Code string `json:"code"`
		} `json:"object"`
		EnvCode    string `json:"envCode"`
		Operations []struct {
			Name       string                     `json:"name"`
			Parameters map[string]json.RawMessage `json:"parameters"`
		} `json:"operations"`
		Skills SkillsVersion `json:"skills"`
	}
	decode := json.NewDecoder(bytes.NewReader(in.Definition))
	decode.DisallowUnknownFields()
	var extra any
	if decode.Decode(&d) != nil || decode.Decode(&extra) != io.EOF || d.Type != "agent" || len(d.Operations) != 1 || d.Skills != in.Skills {
		return AttemptBinding{}, LiveOperationInput{}, ErrInvalid
	}
	var env struct {
		Code   string `json:"code"`
		Config struct {
			ClusterID string `json:"ackClusterId"`
		} `json:"config"`
	}
	if json.Unmarshal(in.Environment, &env) != nil || env.Code != d.EnvCode || env.Config.ClusterID == "" {
		return AttemptBinding{}, LiveOperationInput{}, ErrInvalid
	}
	var canonical any
	if json.Unmarshal(in.Definition, &canonical) != nil {
		return AttemptBinding{}, LiveOperationInput{}, ErrInvalid
	}
	raw, err := json.Marshal(canonical)
	if err != nil {
		return AttemptBinding{}, LiveOperationInput{}, ErrInvalid
	}
	sum := sha256.Sum256(raw)
	hash := hex.EncodeToString(sum[:])
	if !shaPattern.MatchString(in.DefinitionSHA256) || hash != in.DefinitionSHA256 {
		return AttemptBinding{}, LiveOperationInput{}, ErrInvalid
	}
	b := AttemptBinding{RequestID: in.RequestID, AttemptID: attempt, Operation: d.Operations[0].Name, ObjectKind: d.Object.Kind, ObjectCode: d.Object.Code, EnvCode: d.EnvCode, SkillsCommit: d.Skills.CommitSHA, Model: d.Model, ProviderID: d.ProviderID, ConnectionVersion: d.ConnectionVersion, DefinitionSHA256: hash, ClusterID: env.Config.ClusterID}
	// Do not broaden the existing question-only live worker's admission here.
	if b.RequestID == "" || b.AttemptID == "" || !journalName.MatchString(b.ObjectCode) || !journalName.MatchString(b.EnvCode) || !commitPattern.MatchString(b.SkillsCommit) || (b.Operation == "db-proxy.replace-nodes" && b.Model != "gpt-5.6-luna") {
		return AttemptBinding{}, LiveOperationInput{}, ErrInvalid
	}
	op := LiveOperationInput{Definition: append(json.RawMessage(nil), raw...), Parameters: d.Operations[0].Parameters, Environment: append(json.RawMessage(nil), in.Environment...)}
	switch b.Operation {
	case "application.question":
		if !b.valid() || b.ObjectKind != "application" || len(op.Parameters) != 1 || json.Unmarshal(op.Parameters["question"], &op.Question) != nil || op.Question == "" || !utf8.ValidString(op.Question) || utf8.RuneCountInString(op.Question) > 2000 || len(op.Question) > 8192 {
			return AttemptBinding{}, LiveOperationInput{}, ErrInvalid
		}
	case "db-proxy.replace-nodes":
		if !b.journalValid() || b.ObjectKind != "db-proxy" || op.Parameters == nil || len(op.Parameters) != 0 {
			return AttemptBinding{}, LiveOperationInput{}, ErrInvalid
		}
	default:
		return AttemptBinding{}, LiveOperationInput{}, ErrInvalid
	}
	return b, op, nil
}
