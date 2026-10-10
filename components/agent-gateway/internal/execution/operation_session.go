package execution

import (
	"context"
	"encoding/json"
	"github.com/jackc/pgx/v5"
	"path"
	"strings"
)

type SessionCheckpoint struct {
	RequestID        string             `json:"requestId"`
	DefinitionSHA256 string             `json:"definitionSha256"`
	SkillsCommit     string             `json:"skillsCommit"`
	Model            string             `json:"model"`
	SessionID        string             `json:"sessionId"`
	SessionFile      string             `json:"file"`
	SessionSHA256    string             `json:"sha256"`
	Archive          VerifiedCheckpoint `json:"archive"`
}

func (c SessionCheckpoint) validFor(b AttemptBinding) bool {
	return c.RequestID == b.RequestID && shaPattern.MatchString(c.DefinitionSHA256) && c.DefinitionSHA256 == b.DefinitionSHA256 && c.SkillsCommit == b.SkillsCommit && commitPattern.MatchString(c.SkillsCommit) && c.Model == b.Model && c.Model == "gpt-5.6-luna" && journalName.MatchString(c.SessionID) && path.Base(c.SessionFile) == c.SessionFile && strings.HasSuffix(c.SessionFile, ".jsonl") && journalName.MatchString(c.SessionFile) && shaPattern.MatchString(c.SessionSHA256) && c.Archive.valid()
}
func (s *Store) SaveOperationSession(ctx context.Context, attempt, owner string, c SessionCheckpoint) error {
	tx, id, e := s.liveTx(ctx, attempt, owner)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	b, e := bindingFrom(ctx, tx, attempt)
	if e != nil {
		return e
	}
	if e = tx.QueryRow(ctx, "SELECT definition_sha256 FROM gateway.attempts WHERE id=$1", attempt).Scan(&b.DefinitionSHA256); e != nil {
		return e
	}
	raw, e := json.Marshal(c)
	if e != nil || id != c.RequestID || !c.validFor(b) || containsSensitive(string(raw), s.KnownSecrets) {
		return ErrInvalid
	}
	_, e = tx.Exec(ctx, `INSERT INTO gateway.operation_sessions(request_id,attempt_id,checkpoint) VALUES($1,$2,$3) ON CONFLICT(request_id) DO UPDATE SET attempt_id=excluded.attempt_id,checkpoint=excluded.checkpoint,updated_at=now()`, id, attempt, raw)
	if e != nil {
		return e
	}
	return tx.Commit(ctx)
}
func (s *Store) OperationSession(ctx context.Context, b AttemptBinding) (*SessionCheckpoint, error) {
	var raw []byte
	e := s.Pool.QueryRow(ctx, "SELECT checkpoint FROM gateway.operation_sessions WHERE request_id=$1", b.RequestID).Scan(&raw)
	if e == pgx.ErrNoRows {
		return nil, nil
	}
	if e != nil {
		return nil, e
	}
	var c SessionCheckpoint
	if json.Unmarshal(raw, &c) != nil || !c.validFor(b) {
		return nil, ErrInvalid
	}
	return &c, nil
}

func (c SessionCheckpoint) ReferenceFor(b AttemptBinding) (map[string]any, error) {
	if c.RequestID != b.RequestID || c.DefinitionSHA256 != b.DefinitionSHA256 || !shaPattern.MatchString(c.DefinitionSHA256) || c.SkillsCommit != b.SkillsCommit || !commitPattern.MatchString(c.SkillsCommit) || c.Model != b.Model || c.Model != "gpt-5.6-luna" || !journalName.MatchString(c.SessionID) || path.Base(c.SessionFile) != c.SessionFile || !strings.HasSuffix(c.SessionFile, ".jsonl") || !journalName.MatchString(c.SessionFile) || !shaPattern.MatchString(c.SessionSHA256) {
		return nil, ErrInvalid
	}
	return map[string]any{"version": 1, "requestId": c.RequestID, "definitionSha256": c.DefinitionSHA256, "skillsCommit": c.SkillsCommit, "model": c.Model, "sessionId": c.SessionID, "file": c.SessionFile, "sha256": c.SessionSHA256}, nil
}
