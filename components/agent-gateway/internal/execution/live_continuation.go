package execution

import (
	"context"
	"encoding/json"
	"github.com/jackc/pgx/v5"
)

type ContinuationContext struct {
	ApprovalID    string `json:"approvalId"`
	ActionID      string `json:"actionId"`
	BindingDigest string `json:"bindingDigest"`
	Decision      string `json:"decision"`
	Guidance      string `json:"guidance"`
}

// LiveContinuationContext validates persisted scheduling against the same
// request session and a fresh Raptor receipt before credential issuance.
func (s *Store) LiveContinuationContext(ctx context.Context, b AttemptBinding) (*ContinuationContext, error) {
	var raw []byte
	if e := s.Pool.QueryRow(ctx, "SELECT resume_context FROM gateway.executions WHERE request_id=$1", b.RequestID).Scan(&raw); e != nil {
		return nil, e
	}
	if len(raw) == 0 || string(raw) == "null" || string(raw) == "{}" {
		return nil, nil
	}
	var saved ContinuationContext
	if json.Unmarshal(raw, &saved) != nil {
		return nil, ErrInvalid
	}
	cp, e := s.OperationSession(ctx, b)
	if e != nil || cp == nil {
		return nil, ErrInvalid
	}
	var waitRaw []byte
	var phase string
	e = s.Pool.QueryRow(ctx, `SELECT w.wait,w.phase FROM gateway.live_waits w JOIN gateway.attempts a ON a.id=w.attempt_id WHERE w.request_id=$1 ORDER BY a.started_at DESC LIMIT 1`, b.RequestID).Scan(&waitRaw, &phase)
	if e == pgx.ErrNoRows {
		return nil, ErrInvalid
	}
	if e != nil {
		return nil, e
	}
	var wait LiveWait
	if json.Unmarshal(waitRaw, &wait) != nil || !wait.Valid() || phase != "resumed" || saved.ApprovalID != wait.ApprovalID || saved.ActionID != wait.ActionID || saved.BindingDigest != wait.BindingDigest || s.ResolveLiveDecision == nil {
		return nil, ErrInvalid
	}
	decision, e := s.ResolveLiveDecision(ctx, b.RequestID, wait)
	if e != nil {
		return nil, e
	}
	if (decision.Next != "resume" && decision.Next != "continue") || saved.Decision != decision.Next || saved.Guidance != decision.Guidance {
		return nil, ErrInvalid
	}
	return &saved, nil
}
