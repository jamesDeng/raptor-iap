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
	var slot *string
	var unresolved bool
	if v.Kind == "continue" {
		// Keep the same lock order as claim/finalize before changing a terminal
		// qualification execution back to queued.
		if e = tx.QueryRow(ctx, "SELECT request_id::text,unresolved FROM gateway.runtime_slot WHERE id=1 FOR UPDATE").Scan(&slot, &unresolved); e != nil {
			return ErrUnavailable
		}
	}
	var mode, status string
	if e := tx.QueryRow(ctx, "SELECT runtime_mode,status FROM gateway.executions WHERE request_id=$1 FOR UPDATE", v.RequestID).Scan(&mode, &status); e != nil {
		return ErrInvalid
	}
	var binding AttemptBinding
	var attemptState string
	var attemptEnded bool
	if mode == "live" && v.Kind != "cancel" {
		if v.Kind != "approval" && v.Kind != "continue" && v.Kind != "block" && v.Kind != "pause" {
			return ErrInvalid
		}
		var raw []byte
		if e = tx.QueryRow(ctx, "SELECT binding,state,ended_at IS NOT NULL FROM gateway.attempts WHERE id=(SELECT attempt_id FROM gateway.executions WHERE request_id=$1)", v.RequestID).Scan(&raw, &attemptState, &attemptEnded); e != nil {
			return ErrInvalid
		}
		if json.Unmarshal(raw, &binding) != nil || !binding.journalValid() || binding.Operation != "db-proxy.replace-nodes" || binding.RequestID != v.RequestID {
			return ErrInvalid
		}
		if v.Kind == "pause" {
			var body struct {
				Reason string `json:"reason"`
			}
			if json.Unmarshal(v.Payload, &body) != nil || body.Reason != "waiting_approval" {
				return ErrInvalid
			}
			if status != body.Reason {
				return ErrUnavailable
			}
			var phase string
			if e = tx.QueryRow(ctx, "SELECT phase FROM gateway.live_waits WHERE request_id=$1 AND attempt_id=(SELECT attempt_id FROM gateway.executions WHERE request_id=$1)", v.RequestID).Scan(&phase); e != nil || phase != "released" {
				return ErrUnavailable
			}
			// ReleaseLiveWait atomically persists the checkpoint and cleanup
			// before setting this phase. Raptor's pause is now an acknowledgement.
			return tx.Commit(ctx)
		}
	}
	switch v.Kind {
	case "cancel", "block", "interrupt", "continue", "approval", "review", "merged", "skills", "pause":
	default:
		return ErrInvalid
	}

	lateCancel := v.Kind == "cancel" && mode == "live" && (status == "completed" || status == "failed" || status == "cancelled")
	if lateCancel {
		var cleanup []byte
		var recovery bool
		if e = tx.QueryRow(ctx, "SELECT cleanup_status,recovery_needed FROM gateway.executions WHERE request_id=$1", v.RequestID).Scan(&cleanup, &recovery); e != nil {
			return ErrUnavailable
		}
		var confirmed struct {
			SandboxAbsent bool `json:"sandboxAbsent"`
			KeyAbsent     bool `json:"keyAbsent"`
			AccessRevoked bool `json:"accessRevoked"`
		}
		if json.Unmarshal(cleanup, &confirmed) != nil || recovery || !confirmed.SandboxAbsent || !confirmed.KeyAbsent || !confirmed.AccessRevoked {
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
	if mode == "live" && v.Kind == "continue" && binding.ExecutionMode == "qualification" {
		var hint struct {
			AttemptID  string `json:"attemptId"`
			ApprovalID string `json:"approvalId"`
		}
		if json.Unmarshal(v.Payload, &hint) != nil {
			return ErrInvalid
		}
		// An approval wakeup belongs to ResumeLive. A plain Continue can retry
		// only the exact cleaned-up failed attempt named by Raptor.
		if hint.ApprovalID == "" && (hint.AttemptID != "" || status == "failed") {
			var attempt string
			var cleanup []byte
			var recovery bool
			if e = tx.QueryRow(ctx, "SELECT attempt_id::text,cleanup_status,recovery_needed FROM gateway.executions WHERE request_id=$1", v.RequestID).Scan(&attempt, &cleanup, &recovery); e != nil {
				return ErrUnavailable
			}
			var confirmed struct {
				SandboxAbsent bool `json:"sandboxAbsent"`
				KeyAbsent     bool `json:"keyAbsent"`
				AccessRevoked bool `json:"accessRevoked"`
			}
			clean := json.Unmarshal(cleanup, &confirmed) == nil && confirmed.SandboxAbsent && confirmed.KeyAbsent && confirmed.AccessRevoked
			slotSafe := slot == nil && !unresolved || slot != nil && *slot != v.RequestID
			if status == "failed" && attemptState == "failed" && attemptEnded && hint.AttemptID == attempt && hint.AttemptID != "" && clean && !recovery && slotSafe {
				if _, e = tx.Exec(ctx, "UPDATE gateway.executions SET status='queued',stage='preparing',failure_code='',cleanup='pending',cleanup_status='{}',checkpoint_status='pending',checkpoint='{}',result=NULL,queued_at=now(),updated_at=now() WHERE request_id=$1", v.RequestID); e != nil {
					return ErrUnavailable
				}
			}
			// Consume prior plain clicks and duplicates, including signals that
			// arrived before this release. They must never retry a later attempt.
			if _, e = tx.Exec(ctx, "UPDATE gateway.signals SET consumed=true WHERE request_id=$1 AND kind='continue' AND NOT consumed AND payload->>'approvalId' IS NULL", v.RequestID); e != nil {
				return ErrUnavailable
			}
		}
	}
	if lateCancel {
		if _, e = tx.Exec(ctx, "UPDATE gateway.executions SET status='cancelled',failure_code='Cancelled',updated_at=now() WHERE request_id=$1", v.RequestID); e != nil {
			return ErrUnavailable
		}
		if _, e = tx.Exec(ctx, "UPDATE gateway.attempts SET state='cancelled' WHERE id=(SELECT attempt_id FROM gateway.executions WHERE request_id=$1)", v.RequestID); e != nil {
			return ErrUnavailable
		}
		if _, e = tx.Exec(ctx, "UPDATE gateway.signals SET consumed=true WHERE id=$1", v.ID); e != nil {
			return ErrUnavailable
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
