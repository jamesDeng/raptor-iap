package execution

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
)

func (s *Store) DeliverSignal(ctx context.Context, v Signal) error {
	if v.ID == "" || v.RequestID == "" || len(v.Payload) > 4096 || !json.Valid(v.Payload) {
		return ErrInvalid
	}
	tx, e := s.Pool.Begin(ctx)
	if e != nil {
		return ErrUnavailable
	}
	defer tx.Rollback(ctx)
	var mode, status string
	if e := tx.QueryRow(ctx, "SELECT runtime_mode,status FROM gateway.executions WHERE request_id=$1 FOR UPDATE", v.RequestID).Scan(&mode, &status); e != nil {
		return ErrInvalid
	}
	if mode == "live" && v.Kind != "cancel" {
		return ErrInvalid
	}
	switch v.Kind {
	case "cancel", "block", "interrupt", "continue", "approval", "review", "merged", "skills", "pause":
	default:
		return ErrInvalid
	}
	if mode == "live" && (status == "completed" || status == "failed" || status == "cancelled") {
		var duplicate bool
		if e = tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM gateway.signals WHERE id=$1)", v.ID).Scan(&duplicate); e != nil || !duplicate {
			return ErrInvalid
		}
	}
	tag, e := tx.Exec(ctx, "INSERT INTO gateway.signals(id,request_id,kind,payload) VALUES($1,$2,$3,$4) ON CONFLICT DO NOTHING", v.ID, v.RequestID, v.Kind, v.Payload)
	if e != nil {
		return ErrUnavailable
	}
	if tag.RowsAffected() == 0 {
		var id, kind string
		var body []byte
		if e = tx.QueryRow(ctx, "SELECT request_id::text,kind,payload FROM gateway.signals WHERE id=$1", v.ID).Scan(&id, &kind, &body); e != nil {
			return e
		}
		var a, b any
		json.Unmarshal(body, &a)
		json.Unmarshal(v.Payload, &b)
		aa, _ := json.Marshal(a)
		bb, _ := json.Marshal(b)
		if id != v.RequestID || kind != v.Kind || !bytes.Equal(aa, bb) {
			return ErrInvalid
		}
	}
	return tx.Commit(ctx)
}
func (w *Worker) DrainSignals(ctx context.Context) error {
	if e := w.reconcileOwnership(ctx); e != nil {
		return e
	}
	rows, e := w.Store.Pool.Query(ctx, "SELECT id,request_id::text,kind,payload FROM gateway.signals WHERE NOT consumed ORDER BY sequence LIMIT 50")
	if e != nil {
		return e
	}
	var signals []Signal
	for rows.Next() {
		var v Signal
		if e = rows.Scan(&v.ID, &v.RequestID, &v.Kind, &v.Payload); e != nil {
			rows.Close()
			return e
		}
		signals = append(signals, v)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return e
	}
	for _, v := range signals {
		x, e := w.Store.Get(ctx, v.RequestID)
		if e != nil {
			return e
		}
		if x.RecoveryNeeded {
			continue
		}
		if x.Status == "completed" || x.Status == "cancelled" {
			e = nil
		} else {
			e = w.ApplySignal(ctx, x, v)
		}
		if errors.Is(e, ErrUnavailable) {
			continue
		}
		if e != nil {
			return e
		}
		if _, e = w.Store.Pool.Exec(ctx, "UPDATE gateway.signals SET consumed=true WHERE id=$1", v.ID); e != nil {
			return e
		}
	}
	return nil
}
