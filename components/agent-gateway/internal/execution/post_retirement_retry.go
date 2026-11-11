package execution

import (
	"context"
	"encoding/json"

	"github.com/jackc/pgx/v5"
)

// Runtime events select a candidate. The runtime authenticates its archive
// and checks the submitted scale-in and traffic receipts before opening Pi.
func retryablePostRetirement(ctx context.Context, tx pgx.Tx, b AttemptBinding) (bool, error) {
	if b.ApprovalProtocol != "request-grants-v1" || b.ExecutionMode != "replacement" {
		return false, nil
	}
	var candidate bool
	var archive []byte
	err := tx.QueryRow(ctx, `SELECT e.failure_code='ToolFailed' AND e.checkpoint_status='verified' AND a.runtime_id<>''
  AND EXISTS (
   SELECT 1 FROM gateway.runtime_events s
   JOIN gateway.runtime_events r ON r.attempt_id=s.attempt_id AND r.runtime_sequence>s.runtime_sequence
   JOIN gateway.runtime_events f ON f.attempt_id=r.attempt_id AND f.runtime_sequence=r.runtime_sequence+1
   WHERE s.attempt_id=a.id AND s.payload->>'kind'='tool_result'
    AND s.payload->>'tool'='mcp__infra__db_proxy_scale' AND s.payload->>'outcome'='succeeded'
    AND r.payload->>'kind'='progress' AND r.payload->>'summary'='Observation cloud_read http-503'
    AND f.payload->>'kind'='tool_result' AND f.payload->>'tool'='cloud_read' AND f.payload->>'outcome'='failed'
    AND NOT EXISTS (SELECT 1 FROM gateway.runtime_events other WHERE other.attempt_id=a.id
      AND other.runtime_sequence>s.runtime_sequence AND other.payload->>'kind'='tool_start'
      AND other.payload->>'tool' LIKE 'mcp__infra__%')
  ), a.definition_sha256,a.verified_checkpoint
  FROM gateway.executions e JOIN gateway.attempts a ON a.id=e.attempt_id
  WHERE e.request_id=$1 AND a.id=$2`, b.RequestID, b.AttemptID).Scan(&candidate, &b.DefinitionSHA256, &archive)
	if err != nil || !candidate {
		return false, err
	}
	var cp VerifiedCheckpoint
	if json.Unmarshal(archive, &cp) != nil || !cp.valid() {
		return false, nil
	}
	return matchesResumedResourceWait(ctx, tx, b)
}
