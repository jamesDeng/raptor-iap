package execution

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5"
	"time"
)

func (w *LiveWorker) drainResourceWaits(ctx context.Context) error {
	rows, e := w.Store.Pool.Query(ctx, `SELECT w.attempt_id::text,w.request_id::text,w.wait FROM gateway.live_waits w JOIN gateway.executions e ON e.request_id=w.request_id WHERE e.runtime_mode='live' AND e.status='waiting_resource' AND e.attempt_id=w.attempt_id AND w.phase='released' AND w.wait->>'kind'='resource' ORDER BY e.updated_at LIMIT 50`)
	if e != nil {
		return e
	}
	type wake struct {
		attempt, request string
		raw              []byte
	}
	var pending []wake
	for rows.Next() {
		var p wake
		if e = rows.Scan(&p.attempt, &p.request, &p.raw); e != nil {
			rows.Close()
			return e
		}
		pending = append(pending, p)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return e
	}
	for _, p := range pending {
		if e = w.authorized(ctx); e != nil {
			return e
		}
		var wait LiveWait
		if json.Unmarshal(p.raw, &wait) != nil || !wait.Valid() {
			return ErrInvalid
		}
		if time.Now().Before(wait.StartedAt.Add(time.Duration(wait.WakeAfterSeconds) * time.Second)) {
			continue
		}
		signalID := "resource-wake:" + p.attempt
		if _, e = w.Store.Pool.Exec(ctx, `INSERT INTO gateway.signals(id,request_id,kind,payload) VALUES($1,$2,'continue','{"approvalId":""}') ON CONFLICT(id) DO NOTHING`, signalID, p.request); e != nil {
			return e
		}
		_, e = w.Store.ResumeLive(ctx, p.request, signalID)
		if errors.Is(e, pgx.ErrNoRows) || errors.Is(e, ErrInvalid) || errors.Is(e, ErrUnavailable) {
			continue
		}
		if e != nil {
			return e
		}
	}
	return nil
}
