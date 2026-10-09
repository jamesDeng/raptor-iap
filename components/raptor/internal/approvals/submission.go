package approvals

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/domain"
)

type SubmissionClaimResult struct {
	Claimed bool `json:"claimed"`
}
type SubmissionRecordInput struct {
	Binding           ApprovalInput `json:"binding"`
	Outcome           string        `json:"outcome"`
	ProviderRequestID string        `json:"providerRequestId"`
}

// Claim consumes the exact approval before any provider submission. A crash or
// unknown response does not release the action; reconciliation needs a new action.
func (s *Service) Claim(ctx context.Context, in ApprovalInput) (SubmissionClaimResult, error) {
	out := SubmissionClaimResult{}
	b, e := canonical(in)
	if e != nil || in.Interface != "ess.scale-in" {
		return out, domain.ErrInvalid
	}
	tx, e := s.Pool.Begin(ctx)
	if e != nil {
		return out, domain.ErrUnavailable
	}
	defer tx.Rollback(ctx)
	var state, kind, env, control string
	e = tx.QueryRow(ctx, `SELECT status,coalesce(definition->>'type',''),coalesce(definition->>'envCode',''),control_state FROM raptor.requests WHERE id=$1 FOR UPDATE`, in.RequestID).Scan(&state, &kind, &env, &control)
	if errors.Is(e, pgx.ErrNoRows) {
		return out, nil
	}
	if e != nil {
		return out, domain.ErrUnavailable
	}
	if kind != "agent" || env != in.EnvCode || control != "" || state == "blocked" || state == "cancelled" || state == "completed" {
		return out, nil
	}
	var binding []byte
	var approved string
	e = tx.QueryRow(ctx, `SELECT binding,state FROM raptor.approvals WHERE request_id=$1 AND action_id=$2 FOR UPDATE`, in.RequestID, in.ActionID).Scan(&binding, &approved)
	if errors.Is(e, pgx.ErrNoRows) {
		return out, nil
	}
	if e != nil {
		return out, domain.ErrUnavailable
	}
	if approved != "approved" || !equalJSON(binding, b) {
		return out, nil
	}
	tag, e := tx.Exec(ctx, `INSERT INTO raptor.approval_submissions(request_id,action_id,binding) VALUES($1,$2,$3) ON CONFLICT DO NOTHING`, in.RequestID, in.ActionID, b)
	if e != nil {
		return out, domain.ErrUnavailable
	}
	if e = tx.Commit(ctx); e != nil {
		return out, domain.ErrUnavailable
	}
	out.Claimed = tag.RowsAffected() == 1
	return out, nil
}

// RecordSubmission records one immutable acknowledgement. An identical retry is
// safe; neither not_submitted nor unknown permits this action to be claimed again.
func (s *Service) RecordSubmission(ctx context.Context, in SubmissionRecordInput) error {
	b, e := canonical(in.Binding)
	if e != nil || in.Binding.Interface != "ess.scale-in" {
		return domain.ErrInvalid
	}
	if len(in.ProviderRequestID) > 512 {
		return domain.ErrInvalid
	}
	switch in.Outcome {
	case "submitted":
		if in.ProviderRequestID == "" {
			return domain.ErrInvalid
		}
	case "unknown":
	case "not_submitted":
		if in.ProviderRequestID != "" {
			return domain.ErrInvalid
		}
	default:
		return domain.ErrInvalid
	}
	tx, e := s.Pool.Begin(ctx)
	if e != nil {
		return domain.ErrUnavailable
	}
	defer tx.Rollback(ctx)
	var binding []byte
	var outcome, providerID string
	var recorded bool
	e = tx.QueryRow(ctx, `SELECT binding,outcome,provider_request_id,recorded FROM raptor.approval_submissions WHERE request_id=$1 AND action_id=$2 FOR UPDATE`, in.Binding.RequestID, in.Binding.ActionID).Scan(&binding, &outcome, &providerID, &recorded)
	if errors.Is(e, pgx.ErrNoRows) {
		return domain.ErrConflict
	}
	if e != nil {
		return domain.ErrUnavailable
	}
	if !equalJSON(binding, b) {
		return domain.ErrConflict
	}
	if recorded {
		if outcome != in.Outcome || providerID != in.ProviderRequestID {
			return domain.ErrConflict
		}
		return nil
	}
	if _, e = tx.Exec(ctx, `UPDATE raptor.approval_submissions SET outcome=$3,provider_request_id=$4,recorded=true,recorded_at=now() WHERE request_id=$1 AND action_id=$2`, in.Binding.RequestID, in.Binding.ActionID, in.Outcome, in.ProviderRequestID); e != nil {
		return domain.ErrUnavailable
	}
	if e = tx.Commit(ctx); e != nil {
		return domain.ErrUnavailable
	}
	return nil
}
