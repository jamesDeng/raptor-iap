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
	var status string
	if e = tx.QueryRow(ctx, "SELECT status FROM raptor.requests WHERE id=$1 FOR UPDATE", id).Scan(&status); e != nil {
		return domain.ErrNotFound
	}
	if status == "completed" || status == "cancelled" {
		return domain.ErrConflict
	}
	if e = QueueSignal(ctx, tx, id, action, map[string]string{"instructions": instructions}); e != nil {
		return domain.ErrUnavailable
	}
	if action == "cancel" {
		if _, e = tx.Exec(ctx, "UPDATE raptor.requests SET status='cancelled' WHERE id=$1", id); e != nil {
			return domain.ErrUnavailable
		}
	}
	return tx.Commit(ctx)
}
