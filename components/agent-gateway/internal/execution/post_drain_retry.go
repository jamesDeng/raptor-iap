package execution

import (
	"context"
	"encoding/json"
	"time"

	"github.com/jackc/pgx/v5"
)

// Admission only selects a candidate. The runtime separately verifies the
// archive, Raptor receipts, and current routing before starting the guest.
func retryablePostDrain(ctx context.Context, tx pgx.Tx, b AttemptBinding) (string, error) {
	if b.ApprovalProtocol != "request-grants-v1" || b.ExecutionMode != "replacement" {
		return "", nil
	}
	var candidate bool
	var anchor string
	var archive []byte
	// The first recovery anchors to the attempt that recorded the drain proof.
	// A later failure may reuse that anchor only if it ran reads or continuity.
	// The runtime verifies the original archive, exact continuity phase, and unchanged action journal.
	err := tx.QueryRow(ctx, `SELECT e.failure_code='MissingEvidence' AND e.checkpoint_status='verified'
  AND a.runtime_id<>'' AND CASE WHEN COALESCE(e.resume_context->>'postDrainAttempt','')='' THEN EXISTS (
   SELECT 1 FROM (SELECT payload FROM gateway.runtime_events WHERE attempt_id=a.id ORDER BY runtime_sequence DESC LIMIT 1) last
   WHERE last.payload->>'kind'='tool_result' AND last.payload->>'tool'='pgcat_connection_continuity'
    AND last.payload->>'outcome'='succeeded')
  ELSE EXISTS (
   SELECT 1 FROM gateway.attempts source WHERE source.request_id=a.request_id AND source.id<>a.id
    AND source.id::text=e.resume_context->>'postDrainAttempt' AND source.state='failed'
    AND source.definition_sha256=a.definition_sha256 AND source.ended_at<a.started_at
  ) AND EXISTS (
   SELECT 1 FROM gateway.runtime_events event WHERE event.attempt_id=a.id AND
    event.payload->>'kind'='tool_result' AND event.payload->>'outcome'='succeeded'
  ) AND NOT EXISTS (
   SELECT 1 FROM gateway.runtime_events event WHERE event.attempt_id=a.id AND NOT COALESCE((
    event.payload->>'kind' IN ('progress','message','reply') OR
    (event.payload->>'kind' IN ('tool_start','tool_result') AND event.payload->>'tool' IN (
     'mcp__raptor__request_get','mcp__raptor__request_grant_get','mcp__raptor__request_grant_action_get',
     'mcp__raptor__request_grant_actions_list','mcp__raptor__environment_get','mcp__raptor__object_get',
     'mcp__raptor__deployments_list','mcp__infra__deployments_list','mcp__infra__deployment_status_get',
     'cloud_read','deployment_read','metrics_read','pgcat_connection_continuity'
    ))
   ),false)
  ) END,
  COALESCE(NULLIF(e.resume_context->>'postDrainAttempt',''),a.id::text), a.definition_sha256, a.verified_checkpoint
  FROM gateway.executions e JOIN gateway.attempts a ON a.id=e.attempt_id
  WHERE e.request_id=$1 AND a.id=$2`, b.RequestID, b.AttemptID).Scan(&candidate, &anchor, &b.DefinitionSHA256, &archive)
	if err != nil || !candidate {
		return "", err
	}
	var checkpoint VerifiedCheckpoint
	if json.Unmarshal(archive, &checkpoint) != nil || !checkpoint.valid() || !conversationUUID.MatchString(anchor) {
		return "", nil
	}
	matches, err := matchesResumedResourceWait(ctx, tx, b)
	if !matches || err != nil {
		return "", err
	}
	return anchor, nil
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
