package approvals

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/domain"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/requests"
)

type Service struct{ Pool *pgxpool.Pool }
type ApprovalInput struct {
	RequestID  string         `json:"requestId"`
	ActionID   string         `json:"actionId"`
	Interface  string         `json:"interface"`
	EnvCode    string         `json:"envCode"`
	Target     map[string]any `json:"target"`
	Parameters map[string]any `json:"parameters"`
}
type ApprovalCheckInput = ApprovalInput
type ApprovalCheckResult struct {
	Allowed bool   `json:"allowed"`
	Reason  string `json:"reason"`
}
type DecisionInput struct {
	Decision string `json:"decision"`
	Next     string `json:"next,omitempty"`
	Guidance string `json:"guidance,omitempty"`
}

func canonical(in ApprovalInput) ([]byte, error) {
	if in.RequestID == "" || in.ActionID == "" || in.Interface == "" || in.EnvCode == "" || len(in.Target) == 0 || in.Parameters == nil {
		return nil, domain.ErrInvalid
	}
	b, e := json.Marshal(in)
	if e != nil || len(b) > 8192 {
		return nil, domain.ErrInvalid
	}
	return b, nil
}
func scan(row pgx.Row) (domain.Approval, error) {
	var a domain.Approval
	e := row.Scan(&a.ID, &a.RequestID, &a.ActionID, &a.Binding, &a.State, &a.Guidance)
	if errors.Is(e, pgx.ErrNoRows) {
		e = domain.ErrNotFound
	}
	return a, e
}
func (s *Service) Get(ctx context.Context, id string) (domain.Approval, error) {
	return scan(s.Pool.QueryRow(ctx, "SELECT id::text,request_id::text,action_id,binding,state,guidance FROM raptor.approvals WHERE id=$1", id))
}
func (s *Service) Request(ctx context.Context, in ApprovalInput) (domain.Approval, error) {
	b, e := canonical(in)
	if e != nil {
		return domain.Approval{}, e
	}
	tx, e := s.Pool.Begin(ctx)
	if e != nil {
		return domain.Approval{}, domain.ErrUnavailable
	}
	defer tx.Rollback(ctx)
	var state, kind, env string
	if e = tx.QueryRow(ctx, "SELECT status,coalesce(definition->>'type',''),coalesce(definition->>'envCode','') FROM raptor.requests WHERE id=$1 FOR UPDATE", in.RequestID).Scan(&state, &kind, &env); e != nil {
		return domain.Approval{}, domain.ErrNotFound
	}
	if kind != "agent" || env != in.EnvCode {
		return domain.Approval{}, domain.ErrInvalid
	}
	if state == "cancelled" || state == "completed" {
		return domain.Approval{}, domain.ErrConflict
	}
	if _, e = tx.Exec(ctx, "INSERT INTO raptor.approvals(id,request_id,action_id,binding) VALUES($1,$2,$3,$4) ON CONFLICT(request_id,action_id) DO NOTHING", domain.NewID(), in.RequestID, in.ActionID, b); e != nil {
		return domain.Approval{}, domain.ErrUnavailable
	}
	a, e := scan(tx.QueryRow(ctx, "SELECT id::text,request_id::text,action_id,binding,state,guidance FROM raptor.approvals WHERE request_id=$1 AND action_id=$2", in.RequestID, in.ActionID))
	if e != nil {
		return a, e
	}
	if !equalJSON(a.Binding, b) {
		return a, domain.ErrConflict
	}
	if e = tx.Commit(ctx); e != nil {
		return a, domain.ErrUnavailable
	}
	return a, nil
}
func equalJSON(a, b []byte) bool {
	var x, y any
	da, db := json.NewDecoder(bytes.NewReader(a)), json.NewDecoder(bytes.NewReader(b))
	da.UseNumber()
	db.UseNumber()
	if da.Decode(&x) != nil || db.Decode(&y) != nil {
		return false
	}
	aa, _ := json.Marshal(x)
	bb, _ := json.Marshal(y)
	return bytes.Equal(aa, bb)
}
func (s *Service) Decide(ctx context.Context, user domain.User, id string, in DecisionInput) (domain.Approval, error) {
	if user.ID == "" || (user.Role != "admin" && user.Role != "user") {
		return domain.Approval{}, domain.ErrForbidden
	}
	if in.Decision != "approve" && in.Decision != "deny" {
		return domain.Approval{}, domain.ErrInvalid
	}
	if in.Decision == "deny" && (in.Next != "block" && in.Next != "cancel" && !(in.Next == "continue" && in.Guidance != "")) {
		return domain.Approval{}, domain.ErrInvalid
	}
	if len(in.Guidance) > 4000 {
		return domain.Approval{}, domain.ErrInvalid
	}
	tx, e := s.Pool.Begin(ctx)
	if e != nil {
		return domain.Approval{}, domain.ErrUnavailable
	}
	defer tx.Rollback(ctx)
	a, e := scan(tx.QueryRow(ctx, "SELECT id::text,request_id::text,action_id,binding,state,guidance FROM raptor.approvals WHERE id=$1 FOR UPDATE", id))
	if e != nil {
		return a, e
	}
	if a.State != "pending" {
		return a, domain.ErrConflict
	}
	a.State = map[string]string{"approve": "approved", "deny": "denied"}[in.Decision]
	a.Guidance, _ = json.Marshal(in)
	if _, e = tx.Exec(ctx, "UPDATE raptor.approvals SET state=$2,guidance=$3,decided_by=$4 WHERE id=$1", id, a.State, a.Guidance, user.ID); e != nil {
		return a, domain.ErrUnavailable
	}
	kind := "approval"
	if in.Decision == "deny" {
		kind = in.Next
		if in.Next == "block" || in.Next == "cancel" {
			state := map[string]string{"block": "blocked", "cancel": "cancelled"}[in.Next]
			if _, e = tx.Exec(ctx, "UPDATE raptor.requests SET status=$2,control_state=$2 WHERE id=$1", a.RequestID, state); e != nil {
				return a, domain.ErrUnavailable
			}
		}
	}
	if e = requests.QueueSignal(ctx, tx, a.RequestID, kind, map[string]string{"approvalId": id, "decision": a.State, "instructions": in.Guidance}); e != nil {
		return a, domain.ErrUnavailable
	}
	if e = tx.Commit(ctx); e != nil {
		return a, domain.ErrUnavailable
	}
	return a, nil
}
func (s *Service) Check(ctx context.Context, in ApprovalCheckInput) (ApprovalCheckResult, error) {
	out := ApprovalCheckResult{Reason: "not approved"}
	b, e := canonical(in)
	if e != nil {
		return out, e
	}
	var binding []byte
	var state, requestState, kind, env, control string
	e = s.Pool.QueryRow(ctx, "SELECT a.binding,a.state,r.status,coalesce(r.definition->>'type',''),coalesce(r.definition->>'envCode',''),r.control_state FROM raptor.approvals a JOIN raptor.requests r ON r.id=a.request_id WHERE a.request_id=$1 AND a.action_id=$2", in.RequestID, in.ActionID).Scan(&binding, &state, &requestState, &kind, &env, &control)
	if errors.Is(e, pgx.ErrNoRows) {
		return out, nil
	}
	if e != nil {
		return out, domain.ErrUnavailable
	}
	out.Allowed = kind == "agent" && env == in.EnvCode && control == "" && state == "approved" && requestState != "blocked" && requestState != "cancelled" && requestState != "completed" && equalJSON(binding, b)
	if out.Allowed {
		out.Reason = "approved"
	}
	return out, nil
}
