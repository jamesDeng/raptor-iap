package approvals

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/domain"
)

type eligibilityQuery interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

// Exact approval eligibility is checked without holding a connection during
// provider resolution, and again through the final transaction's connection.
func approvalEligibility(ctx context.Context, q eligibilityQuery, in ApprovalInput, claim bool) (bool, string, error) {
	canonicalBinding, e := canonical(in)
	if e != nil {
		return false, "", e
	}
	var stored, definition []byte
	var state, kind, env, control, approved string
	var consumed bool
	e = q.QueryRow(ctx, `SELECT r.status,coalesce(r.definition->>'type',''),coalesce(r.definition->>'envCode',''),r.control_state,a.binding,a.state,r.definition,EXISTS(SELECT 1 FROM raptor.approval_submissions u WHERE u.request_id=a.request_id AND u.action_id=a.action_id) FROM raptor.requests r JOIN raptor.approvals a ON a.request_id=r.id AND a.action_id=$2 WHERE r.id=$1`, in.RequestID, in.ActionID).Scan(&state, &kind, &env, &control, &stored, &approved, &definition, &consumed)
	if errors.Is(e, pgx.ErrNoRows) {
		return false, "", nil
	}
	if e != nil {
		return false, "", domain.ErrUnavailable
	}
	if kind != "agent" || env != in.EnvCode || requestInactive(state, control) || approved != "approved" || !equalJSON(stored, canonicalBinding) || (claim && consumed) {
		return false, "", nil
	}
	raw, _ := json.Marshal([]any{in, claim, state, kind, env, control, approved, json.RawMessage(stored), json.RawMessage(definition), consumed})
	hash := sha256.Sum256(raw)
	return true, hex.EncodeToString(hash[:]), nil
}

// PrepareResource grants no authorization: it only binds a private operation to
// its current eligible exact action before trusted Infra performs provider work.
func (s *Service) PrepareResource(ctx context.Context, in ApprovalInput, operation string) (string, error) {
	if in.Interface != "db-proxy.scale-in" || (operation != "check" && operation != "claim") {
		return "", domain.ErrInvalid
	}
	ok, snapshot, e := approvalEligibility(ctx, s.Pool, in, operation == "claim")
	if e != nil || !ok {
		return "", e
	}
	return snapshot, nil
}
