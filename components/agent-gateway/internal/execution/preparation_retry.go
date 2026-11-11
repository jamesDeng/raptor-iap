package execution

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5"
)

// retryablePreparation is evaluated under the slot/execution locks. It permits
// resuming an intact resource-wait session, never replaying a runtime attempt.
func retryablePreparation(ctx context.Context, tx pgx.Tx, b AttemptBinding) (bool, error) {
	var code, hash string
	var untouched bool
	err := tx.QueryRow(ctx, `SELECT e.failure_code,a.definition_sha256,
 a.runtime_id=''
 AND EXISTS(SELECT 1 FROM gateway.runtime_intents i WHERE i.attempt_id=a.id AND i.kind='access')
 AND NOT EXISTS(SELECT 1 FROM gateway.runtime_intents i WHERE i.attempt_id=a.id AND i.kind<>'access'
   AND NOT (e.failure_code='PreparationFailed' AND i.kind='provider' AND i.name=i.resource_id AND i.resource_id IN ('ax','aliyun')))
 AND (e.failure_code<>'PreparationFailed' OR EXISTS(SELECT 1 FROM gateway.runtime_intents i
   WHERE i.attempt_id=a.id AND i.kind='provider' AND i.name=i.resource_id AND i.resource_id IN ('ax','aliyun')))
 AND NOT EXISTS(SELECT 1 FROM gateway.runtime_events v WHERE v.attempt_id=a.id)
 FROM gateway.executions e JOIN gateway.attempts a ON a.id=e.attempt_id WHERE e.request_id=$1 AND a.id=$2`, b.RequestID, b.AttemptID).Scan(&code, &hash, &untouched)
	if err != nil {
		return false, err
	}
	if !untouched || (code != "ProviderUnavailable" && code != "PreparationFailed") {
		return false, nil
	}
	b.DefinitionSHA256 = hash
	return matchesResumedResourceWait(ctx, tx, b)
}

func matchesResumedResourceWait(ctx context.Context, tx pgx.Tx, b AttemptBinding) (bool, error) {
	var cpRaw, waitRaw, resumeRaw, parentRaw []byte
	var phase, parentHash, parentState string
	var sameSession bool
	err := tx.QueryRow(ctx, `SELECT s.checkpoint,w.wait,w.phase,e.resume_context,s.checkpoint=w.checkpoint,a.binding,a.definition_sha256,a.state
 FROM gateway.operation_sessions s
 JOIN gateway.executions e ON e.request_id=s.request_id
 JOIN LATERAL (SELECT w.* FROM gateway.live_waits w JOIN gateway.attempts source ON source.id=w.attempt_id WHERE w.request_id=s.request_id ORDER BY source.started_at DESC,source.id DESC LIMIT 1) w ON w.attempt_id=s.attempt_id
 JOIN gateway.attempts a ON a.id=s.attempt_id
 WHERE s.request_id=$1 AND s.attempt_id<>$2`, b.RequestID, b.AttemptID).Scan(&cpRaw, &waitRaw, &phase, &resumeRaw, &sameSession, &parentRaw, &parentHash, &parentState)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	var cp SessionCheckpoint
	var wait LiveWait
	var continuation ContinuationContext
	var parent AttemptBinding
	if json.Unmarshal(cpRaw, &cp) != nil || json.Unmarshal(waitRaw, &wait) != nil || json.Unmarshal(resumeRaw, &continuation) != nil || json.Unmarshal(parentRaw, &parent) != nil {
		return false, nil
	}
	parent.AttemptID = b.AttemptID
	parent.DefinitionSHA256 = parentHash
	if parent != b || parentState != "waiting_resource" || !sameSession || !cp.validFor(b) || !wait.Valid() || wait.Kind != "resource" || phase != "resumed" || wait.BindingDigest != b.DefinitionSHA256 {
		return false, nil
	}
	return continuation == (ContinuationContext{ApprovalID: wait.ApprovalID, ActionID: wait.ActionID, BindingDigest: wait.BindingDigest, Decision: "resume"}), nil
}
