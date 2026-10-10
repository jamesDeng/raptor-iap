package execution

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5"
)

// DrainSignals retries durable wakeups after cleanup. Receipt resolution is the
// only authorization boundary; early events remain pending across restarts.
func (w *LiveWorker) DrainSignals(ctx context.Context) error {
	if e := w.authorized(ctx); e != nil {
		return e
	}
	if e := w.drainResourceWaits(ctx); e != nil {
		return e
	}
	rows, e := w.Store.Pool.Query(ctx, `SELECT s.id,s.request_id::text FROM gateway.signals s JOIN gateway.executions e ON e.request_id=s.request_id JOIN LATERAL (SELECT w.approval_id,w.phase FROM gateway.live_waits w JOIN gateway.attempts a ON a.id=w.attempt_id WHERE w.request_id=s.request_id ORDER BY a.started_at DESC LIMIT 1) w ON true WHERE NOT s.consumed AND e.runtime_mode='live' AND w.phase IN ('released','resumed') AND (s.kind IN ('cancel','block') OR (s.kind IN ('approval','continue') AND s.payload->>'approvalId'=w.approval_id::text)) ORDER BY s.sequence LIMIT 50`)
	if e != nil {
		return e
	}
	type wake struct{ id, request string }
	var wakes []wake
	for rows.Next() {
		var v wake
		if e = rows.Scan(&v.id, &v.request); e != nil {
			rows.Close()
			return e
		}
		wakes = append(wakes, v)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return e
	}
	for _, v := range wakes {
		if e = w.authorized(ctx); e != nil {
			return e
		}
		_, e = w.Store.ResumeLive(ctx, v.request, v.id)
		// No wait yet, unrelated approval, or an unavailable fresh reader cannot
		// consume an event or authorize a model segment. Other requests still run.
		if errors.Is(e, pgx.ErrNoRows) || errors.Is(e, ErrInvalid) || errors.Is(e, ErrUnavailable) {
			continue
		}
		if e != nil {
			return e
		}
	}
	return nil
}
