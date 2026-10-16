package execution

import (
	"context"
	"errors"
)

// CheckIdle reads one database snapshot. Unfinished attempts include approval
// pauses; recovery and unresolved ownership also prevent a safe restart.
// This is an observation, not an admission lock: operators must quiesce callers.
func (s *Store) CheckIdle(ctx context.Context) error {
	var busy bool
	err := s.Pool.QueryRow(ctx, `SELECT
 EXISTS(SELECT 1 FROM gateway.attempts WHERE ended_at IS NULL) OR
 EXISTS(SELECT 1 FROM gateway.executions WHERE status IN ('queued','running','paused','waiting_approval','waiting_resource') OR recovery_needed OR cleanup <> 'confirmed') OR
 EXISTS(SELECT 1 FROM gateway.runtime_slot WHERE request_id IS NOT NULL OR owner IS NOT NULL OR unresolved) OR
 NOT EXISTS(SELECT 1 FROM gateway.runtime_slot WHERE id=1)`).Scan(&busy)
	if err != nil || busy {
		return errors.New("gateway idle check failed")
	}
	return nil
}
