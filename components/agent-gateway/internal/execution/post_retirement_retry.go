package execution

import (
	"context"
	"encoding/json"

	"github.com/jackc/pgx/v5"
)

// Runtime events select a candidate. The runtime authenticates its archive
// and checks the submitted scale-in and traffic receipts before opening Pi.
func retryablePostRetirement(ctx context.Context, tx pgx.Tx, b AttemptBinding) (string, error) {
	if b.ApprovalProtocol != "request-grants-v1" || b.ExecutionMode != "replacement" {
		return "", nil
	}
	var source string
	var archive []byte
	err := tx.QueryRow(ctx, `WITH failed_cloud_reads AS (
  SELECT r.attempt_id,r.runtime_sequence
  FROM gateway.runtime_events r JOIN gateway.attempts a ON a.id=r.attempt_id
  JOIN gateway.runtime_events f ON f.attempt_id=r.attempt_id AND f.runtime_sequence=r.runtime_sequence+1
  WHERE a.request_id=$1 AND r.payload->>'kind'='progress' AND r.payload->>'summary'='Observation cloud_read http-503'
    AND f.payload->>'kind'='tool_result' AND f.payload->>'tool'='cloud_read' AND f.payload->>'outcome'='failed'
 )
 SELECT COALESCE((
  SELECT origin.id::text FROM gateway.attempts origin
  JOIN gateway.runtime_events s ON s.attempt_id=origin.id
  JOIN failed_cloud_reads failure ON failure.attempt_id=origin.id AND failure.runtime_sequence>s.runtime_sequence
  WHERE origin.request_id=a.request_id AND origin.definition_sha256=a.definition_sha256 AND origin.state='failed'
    AND (origin.id=a.id OR origin.ended_at<a.started_at)
    AND s.payload->>'kind'='tool_result' AND s.payload->>'tool'='mcp__infra__db_proxy_scale' AND s.payload->>'outcome'='succeeded'
    AND NOT EXISTS (SELECT 1 FROM gateway.runtime_events other JOIN gateway.attempts segment ON segment.id=other.attempt_id
      WHERE segment.request_id=a.request_id AND ((segment.id=origin.id AND other.runtime_sequence>s.runtime_sequence)
        OR (segment.started_at>origin.started_at AND segment.started_at<=a.started_at))
        AND other.payload->>'kind'='tool_start' AND other.payload->>'tool' IN
        ('mcp__infra__db_proxy_scale','mcp__infra__db_proxy_node_protection_set','mcp__infra__db_proxy_nodes_deregister','mcp__infra__deployment_restart'))
  ORDER BY origin.started_at DESC LIMIT 1), ''), a.definition_sha256,a.verified_checkpoint
 FROM gateway.executions e JOIN gateway.attempts a ON a.id=e.attempt_id
 WHERE e.request_id=$1 AND a.id=$2 AND e.failure_code='ToolFailed' AND e.checkpoint_status='verified' AND a.runtime_id<>''
   AND EXISTS (SELECT 1 FROM failed_cloud_reads latest WHERE latest.attempt_id=a.id)`, b.RequestID, b.AttemptID).Scan(&source, &b.DefinitionSHA256, &archive)
	if err == pgx.ErrNoRows {
		return "", nil
	}
	if err != nil || source == "" {
		return "", err
	}
	var cp VerifiedCheckpoint
	if json.Unmarshal(archive, &cp) != nil || !cp.valid() {
		return "", nil
	}
	matches, err := matchesResumedResourceWait(ctx, tx, b)
	if !matches || err != nil {
		return "", err
	}
	return source, nil
}
