package requests

import (
	"context"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/domain"
)

func (s *Service) Action(ctx context.Context, id, action, instructions string) error {
	if action != "block" && action != "cancel" && action != "continue" {
		return domain.ErrInvalid
	}
	if len(instructions) > 4000 || action == "continue" && instructions == "" {
		return domain.ErrInvalid
	}
	tx, e := s.Pool.Begin(ctx)
	if e != nil {
		return domain.ErrUnavailable
	}
	defer tx.Rollback(ctx)
	var status, kind string
	if e = tx.QueryRow(ctx, "SELECT status,definition->>'type' FROM raptor.requests WHERE id=$1 FOR UPDATE", id).Scan(&status, &kind); e != nil {
		return domain.ErrNotFound
	}
	if status == "completed" || status == "cancelled" {
		return domain.ErrConflict
	}
	if kind == "direct" {
		next := map[string]string{"block": "blocked", "cancel": "cancelled", "continue": "queued"}[action]
		if action == "continue" {
			var retryable bool
			if e = tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM raptor.targets WHERE request_id=$1 AND state IN ('queued','failed'))", id).Scan(&retryable); e != nil {
				return domain.ErrUnavailable
			}
			if !retryable {
				return domain.ErrConflict
			}
		}
		if _, e = tx.Exec(ctx, "UPDATE raptor.requests SET status=$2 WHERE id=$1", id, next); e != nil {
			return domain.ErrUnavailable
		}
		return tx.Commit(ctx)
	}
	if e = QueueSignal(ctx, tx, id, action, map[string]string{"instructions": instructions}); e != nil {
		return domain.ErrUnavailable
	}
	if action == "cancel" || action == "block" {
		state := map[string]string{"cancel": "cancelled", "block": "blocked"}[action]
		if _, e = tx.Exec(ctx, "UPDATE raptor.requests SET status=$2,control_state=$2 WHERE id=$1", id, state); e != nil {
			return domain.ErrUnavailable
		}
	} else if action == "continue" {
		if _, e = tx.Exec(ctx, "UPDATE raptor.requests SET status='queued',control_state='' WHERE id=$1", id); e != nil {
			return domain.ErrUnavailable
		}
	}
	return tx.Commit(ctx)
}
