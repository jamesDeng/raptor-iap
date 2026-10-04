package requests

import (
	"context"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/domain"
)

func (s *Service) Pause(ctx context.Context, id, reason string) error {
	if reason != "waiting_approval" && reason != "waiting_review" {
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
	if status == "cancelled" || status == "completed" {
		return domain.ErrConflict
	}
	if e = QueueSignal(ctx, tx, id, "pause", map[string]string{"reason": reason}); e != nil {
		return domain.ErrUnavailable
	}
	if _, e = tx.Exec(ctx, "UPDATE raptor.requests SET status=$2 WHERE id=$1", id, reason); e != nil {
		return domain.ErrUnavailable
	}
	return tx.Commit(ctx)
}
