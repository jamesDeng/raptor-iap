package execution

import (
	"context"
	"encoding/json"
	"time"
)

type Clock interface{ Now() time.Time }

func (w *Worker) now() time.Time {
	if w.Clock != nil {
		return w.Clock.Now()
	}
	return time.Now()
}
func (w *Worker) handle(ctx context.Context, x Execution) (RuntimeHandle, error) {
	var id string
	e := w.Store.Pool.QueryRow(ctx, "SELECT runtime_id FROM gateway.attempts WHERE id=$1 AND ended_at IS NULL", x.AttemptID).Scan(&id)
	return RuntimeHandle{ID: id, RequestID: x.RequestID}, e
}
func (w *Worker) Pause(ctx context.Context, x Execution, reason string) error {
	if reason != "waiting_approval" && reason != "waiting_review" && reason != "blocked" && reason != "cancelled" && reason != "interrupted" {
		return ErrInvalid
	}
	var pending bool
	if e := w.Store.Pool.QueryRow(ctx, "SELECT pending_skills<>'{}' FROM gateway.executions WHERE request_id=$1", x.RequestID).Scan(&pending); e != nil {
		return e
	}
	if reason == "waiting_approval" && !pending {
		_, e := w.Store.Pool.Exec(ctx, "UPDATE gateway.executions SET status=$2,updated_at=$3 WHERE request_id=$1 AND status='running'", x.RequestID, reason, w.now())
		return e
	}
	h, e := w.handle(ctx, x)
	if e != nil {
		return e
	}
	ref, e := w.Runtime.Checkpoint(ctx, h)
	if e != nil {
		return w.Store.BlockRecovery(ctx, x.RequestID)
	}
	out, e := w.Runtime.Stop(ctx, h)
	if e != nil || !out.Confirmed {
		return w.Store.BlockRecovery(ctx, x.RequestID)
	}
	if e = w.Store.Release(ctx, x.RequestID, w.Owner, reason, ref); e != nil {
		return e
	}
	if reason == "waiting_review" || reason == "waiting_approval" || reason == "interrupted" {
		return w.applyPendingSkills(ctx, x.RequestID)
	}
	return nil
}
func (w *Worker) ExpireApproval(ctx context.Context, id string) error {
	x, e := w.Store.Get(ctx, id)
	if e != nil {
		return e
	}
	if x.Status != "waiting_approval" || w.now().Before(x.UpdatedAt.Add(120*time.Second)) {
		return nil
	}
	var held *string
	if e = w.Store.Pool.QueryRow(ctx, "SELECT request_id::text FROM gateway.runtime_slot WHERE id=1").Scan(&held); e != nil {
		return e
	}
	if held == nil || *held != id {
		return nil
	}
	h, e := w.handle(ctx, x)
	if e != nil {
		return e
	}
	ref, e := w.Runtime.Checkpoint(ctx, h)
	if e != nil {
		return w.Store.BlockRecovery(ctx, id)
	}
	out, e := w.Runtime.Stop(ctx, h)
	if e != nil || !out.Confirmed {
		return w.Store.BlockRecovery(ctx, id)
	}
	return w.Store.Release(ctx, id, w.Owner, "waiting_approval", ref)
}
func (w *Worker) ApplySignal(ctx context.Context, x Execution, signal Signal) error {
	if signal.RequestID != x.RequestID || x.Status == "cancelled" || x.Status == "completed" || x.RecoveryNeeded {
		return ErrInvalid
	}
	switch signal.Kind {
	case "skills":
		return w.ApplySkillsChange(ctx, x, signal)
	case "pause":
		var p struct {
			Reason string `json:"reason"`
		}
		if json.Unmarshal(signal.Payload, &p) != nil {
			return ErrInvalid
		}
		return w.Pause(ctx, x, p.Reason)
	case "cancel", "block", "interrupt":
		status := map[string]string{"cancel": "cancelled", "block": "blocked", "interrupt": "interrupted"}[signal.Kind]
		var held *string
		if e := w.Store.Pool.QueryRow(ctx, "SELECT request_id::text FROM gateway.runtime_slot WHERE id=1").Scan(&held); e != nil {
			return e
		}
		if held != nil && *held == x.RequestID {
			return w.Pause(ctx, x, status)
		}
		_, e := w.Store.Pool.Exec(ctx, "UPDATE gateway.executions SET status=$2,updated_at=now() WHERE request_id=$1", x.RequestID, status)
		return e
	case "continue", "approval", "review", "merged":
		if signal.Kind == "continue" {
			var p struct {
				Instructions string `json:"instructions"`
			}
			if json.Unmarshal(signal.Payload, &p) != nil || p.Instructions == "" {
				return ErrInvalid
			}
		}
		tx, e := w.Store.Pool.Begin(ctx)
		if e != nil {
			return e
		}
		defer tx.Rollback(ctx)
		var held *string
		var unresolved bool
		if e = tx.QueryRow(ctx, "SELECT request_id::text,unresolved FROM gateway.runtime_slot WHERE id=1 FOR UPDATE").Scan(&held, &unresolved); e != nil {
			return e
		}
		if held != nil && *held == x.RequestID {
			if x.Status != "waiting_approval" {
				return ErrUnavailable
			}
			_, e = tx.Exec(ctx, "UPDATE gateway.executions SET status='running',updated_at=now() WHERE request_id=$1", x.RequestID)
		} else {
			_, e = tx.Exec(ctx, "UPDATE gateway.executions SET status='queued',queued_at=now(),updated_at=now() WHERE request_id=$1 AND status NOT IN ('running','completed','cancelled')", x.RequestID)
		}
		if e != nil {
			return e
		}
		return tx.Commit(ctx)
	default:
		return ErrInvalid
	}
}
