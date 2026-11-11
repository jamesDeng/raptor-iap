package execution

import (
	"context"
	"encoding/json"
	"time"

	"github.com/jackc/pgx/v5"
)

// Admission only selects a candidate. The runtime separately verifies the
// archive, Raptor receipts, and current routing before starting the guest.
func retryablePostDrain(ctx context.Context, tx pgx.Tx, b AttemptBinding) (bool, error) {
	if b.ApprovalProtocol != "request-grants-v1" || b.ExecutionMode != "replacement" {
		return false, nil
	}
	var candidate bool
	var archive []byte
	err := tx.QueryRow(ctx, `SELECT e.failure_code='MissingEvidence' AND e.checkpoint_status='verified'
		AND a.runtime_id<>'' AND EXISTS (
			SELECT 1 FROM (SELECT payload FROM gateway.runtime_events WHERE attempt_id=a.id ORDER BY runtime_sequence DESC LIMIT 1) last
			WHERE last.payload->>'kind'='tool_result' AND last.payload->>'tool'='pgcat_connection_continuity'
				AND last.payload->>'outcome'='succeeded')
		AND COALESCE(e.resume_context->>'postDrainAttempt','')='', a.definition_sha256, a.verified_checkpoint
		FROM gateway.executions e JOIN gateway.attempts a ON a.id=e.attempt_id
		WHERE e.request_id=$1 AND a.id=$2`, b.RequestID, b.AttemptID).Scan(&candidate, &b.DefinitionSHA256, &archive)
	if err != nil || !candidate {
		return false, err
	}
	var checkpoint VerifiedCheckpoint
	if json.Unmarshal(archive, &checkpoint) != nil || !checkpoint.valid() {
		return false, nil
	}
	return matchesResumedResourceWait(ctx, tx, b)
}

func (s *Store) PostDrainCheckpoint(ctx context.Context, b AttemptBinding, attempt string) (VerifiedCheckpoint, time.Time, error) {
	var archived, binding []byte
	var ended time.Time
	var hash string
	err := s.Pool.QueryRow(ctx, `SELECT verified_checkpoint,binding,ended_at,definition_sha256 FROM gateway.attempts
		WHERE id=$1 AND request_id=$2 AND state='failed' AND runtime_id<>'' AND id<>$3`,
		attempt, b.RequestID, b.AttemptID).Scan(&archived, &binding, &ended, &hash)
	if err != nil {
		return VerifiedCheckpoint{}, time.Time{}, err
	}
	var previous AttemptBinding
	var checkpoint VerifiedCheckpoint
	if json.Unmarshal(binding, &previous) != nil || json.Unmarshal(archived, &checkpoint) != nil || !checkpoint.valid() {
		return VerifiedCheckpoint{}, time.Time{}, ErrInvalid
	}
	previous.AttemptID, previous.DefinitionSHA256 = b.AttemptID, hash
	if previous != b {
		return VerifiedCheckpoint{}, time.Time{}, ErrInvalid
	}
	return checkpoint, ended, nil
}
