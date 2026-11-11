package execution

import (
	"context"
	"encoding/json"

	"github.com/jackc/pgx/v5"
)

// A 503 after reading the approved grant can replay its saved read-only
// checkpoint. The runtime also checks Raptor's action journal before use.
func retryablePreExpansion(ctx context.Context, tx pgx.Tx, b AttemptBinding) (bool, error) {
	if b.ApprovalProtocol != "request-grants-v1" || b.ExecutionMode != "replacement" {
		return false, nil
	}
	var eligible bool
	var raw []byte
	err := tx.QueryRow(ctx, `SELECT e.failure_code='ToolFailed' AND e.checkpoint_status='verified' AND a.runtime_id<>''
 AND EXISTS (SELECT 1 FROM gateway.runtime_events g WHERE g.attempt_id=a.id AND g.payload->>'kind'='tool_result'
   AND g.payload->>'tool'='mcp__raptor__request_grant_get' AND g.payload->>'outcome'='succeeded')
 AND EXISTS (SELECT 1 FROM gateway.runtime_events r JOIN gateway.runtime_events f ON f.attempt_id=r.attempt_id AND f.runtime_sequence=r.runtime_sequence+1
   WHERE r.attempt_id=a.id AND r.payload->>'kind'='progress' AND r.payload->>'summary'='Observation cloud_read http-503'
   AND f.payload->>'kind'='tool_result' AND f.payload->>'tool'='cloud_read' AND f.payload->>'outcome'='failed')
 AND NOT EXISTS (SELECT 1 FROM gateway.runtime_events v JOIN gateway.attempts h ON h.id=v.attempt_id
   WHERE h.request_id=a.request_id AND v.payload->>'kind' IN ('tool_start','tool_result') AND v.payload->>'tool' IN
   ('mcp__infra__db_proxy_scale','mcp__infra__db_proxy_node_protection_set','mcp__infra__db_proxy_nodes_deregister','mcp__infra__deployment_restart')),
 a.definition_sha256,a.verified_checkpoint FROM gateway.executions e JOIN gateway.attempts a ON a.id=e.attempt_id WHERE e.request_id=$1 AND a.id=$2`, b.RequestID, b.AttemptID).Scan(&eligible, &b.DefinitionSHA256, &raw)
	if err != nil || !eligible {
		return false, err
	}
	var checkpoint VerifiedCheckpoint
	if json.Unmarshal(raw, &checkpoint) != nil || !checkpoint.valid() {
		return false, nil
	}
	return matchesResumedWait(ctx, tx, b, "grant", "waiting_approval")
}
