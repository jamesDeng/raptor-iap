package requests

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5"
)

func (s *Service) RunDirectPending(ctx context.Context, client InfraCommands) error {
	// Hold a session lock for the entire mutation/observation batch. A new process
	// can only reconcile abandoned work after the former driver's connection ends.
	conn, e := s.Pool.Acquire(ctx)
	if e != nil {
		return e
	}
	defer conn.Release()
	var acquired bool
	if e = conn.QueryRow(ctx, "SELECT pg_try_advisory_lock(762019002)").Scan(&acquired); e != nil {
		return e
	}
	if !acquired {
		return nil
	}
	defer conn.Exec(context.Background(), "SELECT pg_advisory_unlock(762019002)")
	tx, e := s.Pool.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	if _, e = tx.Exec(ctx, `UPDATE raptor.targets SET state='unknown',details=details || '{"reason":"backend interrupted; reconcile deployment status before any retry","recoveryNeeded":true,"evidenceMode":"simulated"}'::jsonb WHERE request_id IN (SELECT id FROM raptor.requests WHERE definition->>'type'='direct' AND status='running') AND state IN ('submitting','observing')`); e != nil {
		return e
	}
	if _, e = tx.Exec(ctx, `UPDATE raptor.requests SET status='failed' WHERE definition->>'type'='direct' AND status='running'`); e != nil {
		return e
	}
	if e = tx.Commit(ctx); e != nil {
		return e
	}

	var id string
	e = s.Pool.QueryRow(ctx, "UPDATE raptor.requests SET status='running' WHERE id=(SELECT id FROM raptor.requests WHERE definition->>'type'='direct' AND status='queued' ORDER BY created_at LIMIT 1 FOR UPDATE SKIP LOCKED) RETURNING id::text").Scan(&id)
	if errors.Is(e, pgx.ErrNoRows) {
		return nil
	}
	if e != nil {
		return e
	}
	return s.RunRestartBatch(ctx, id, client)
}
