package execution

import "context"

// One process may drive the simulated runtime at a time. The session lock is
// released by PostgreSQL when a process dies; runtime ownership remains durable.
func (s *Store) AcquireWorker(ctx context.Context) (func(), error) {
	conn, e := s.Pool.Acquire(ctx)
	if e != nil {
		return nil, e
	}
	var acquired bool
	if e = conn.QueryRow(ctx, "SELECT pg_try_advisory_lock(762019001)").Scan(&acquired); e != nil || !acquired {
		conn.Release()
		return nil, ErrUnavailable
	}
	return func() { conn.Exec(context.Background(), "SELECT pg_advisory_unlock(762019001)"); conn.Release() }, nil
}
func (w *Worker) reconcileOwnership(ctx context.Context) error {
	_, e := w.Store.Pool.Exec(ctx, `UPDATE gateway.executions SET status='blocked',recovery_needed=true,updated_at=now() WHERE request_id=(SELECT request_id FROM gateway.runtime_slot WHERE id=1 AND owner<>$1) AND NOT recovery_needed`, w.Owner)
	return e
}
