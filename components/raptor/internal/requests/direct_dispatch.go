package requests

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5"
)

func (s *Service) RunDirectPending(ctx context.Context, client InfraCommands) error {
	var id string
	e := s.Pool.QueryRow(ctx, "UPDATE raptor.requests SET status='running' WHERE id=(SELECT id FROM raptor.requests WHERE definition->>'type'='direct' AND status='queued' ORDER BY created_at LIMIT 1 FOR UPDATE SKIP LOCKED) RETURNING id::text").Scan(&id)
	if errors.Is(e, pgx.ErrNoRows) {
		return nil
	}
	if e != nil {
		return e
	}
	return s.RunRestartBatch(ctx, id, client)
}
