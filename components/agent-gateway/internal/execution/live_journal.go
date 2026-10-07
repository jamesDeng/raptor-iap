package execution

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/jackc/pgx/v5"
	"time"
)

// Lock the global slot first, consistently with ClaimNext. Every live mutation
// checks both the durable attempt and current owner; ended attempts never write.
func (s *Store) liveTx(ctx context.Context, attempt, owner string) (pgx.Tx, string, error) {
	tx, e := s.Pool.Begin(ctx)
	if e != nil {
		return nil, "", e
	}
	fail := func(err error) (pgx.Tx, string, error) { tx.Rollback(ctx); return nil, "", err }
	var id, held *string
	if e = tx.QueryRow(ctx, "SELECT request_id::text,owner FROM gateway.runtime_slot WHERE id=1 FOR UPDATE").Scan(&id, &held); e != nil {
		return fail(e)
	}
	if id == nil || held == nil || *held != owner || owner == "" {
		return fail(ErrInvalid)
	}
	var active bool
	if e = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM gateway.attempts a JOIN gateway.executions x ON x.request_id=a.request_id WHERE a.id=$1 AND a.request_id=$2 AND a.ended_at IS NULL AND x.attempt_id=a.id)`, attempt, *id).Scan(&active); e != nil {
		return fail(e)
	}
	if !active {
		return fail(ErrInvalid)
	}
	return tx, *id, nil
}
func bindingFrom(ctx context.Context, tx pgx.Tx, attempt string) (AttemptBinding, error) {
	var raw []byte
	var b AttemptBinding
	if e := tx.QueryRow(ctx, "SELECT binding FROM gateway.attempts WHERE id=$1", attempt).Scan(&raw); e != nil {
		return b, e
	}
	if json.Unmarshal(raw, &b) != nil || !b.valid() {
		return b, ErrInvalid
	}
	return b, nil
}
func (s *Store) BindLive(ctx context.Context, attempt, owner string, b AttemptBinding, hash string) error {
	if !b.valid() || b.AttemptID != attempt || !shaPattern.MatchString(hash) {
		return ErrInvalid
	}
	tx, id, e := s.liveTx(ctx, attempt, owner)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	if id != b.RequestID {
		return ErrInvalid
	}
	var old []byte
	var fingerprint *string
	if e = tx.QueryRow(ctx, "SELECT binding,definition_sha256 FROM gateway.attempts WHERE id=$1", attempt).Scan(&old, &fingerprint); e != nil {
		return e
	}
	raw, _ := json.Marshal(b)
	if len(old) > 0 {
		var previous AttemptBinding
		if json.Unmarshal(old, &previous) != nil || previous != b || fingerprint == nil || *fingerprint != hash {
			return ErrInvalid
		}
		return tx.Commit(ctx)
	}
	if _, e = tx.Exec(ctx, "UPDATE gateway.attempts SET binding=$2,definition_sha256=$3 WHERE id=$1", attempt, raw, hash); e != nil {
		return e
	}
	if _, e = tx.Exec(ctx, "UPDATE gateway.executions SET runtime_mode='live',stage='preparing' WHERE request_id=$1", id); e != nil {
		return e
	}
	return tx.Commit(ctx)
}
func (s *Store) RecordIntent(ctx context.Context, attempt, owner string, in RuntimeIntent) error {
	if !validJournalKind(in.Kind) || !journalName.MatchString(in.Name) || containsSensitive(in.Name, s.KnownSecrets) {
		return ErrInvalid
	}
	tx, _, e := s.liveTx(ctx, attempt, owner)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	if _, e = bindingFrom(ctx, tx, attempt); e != nil {
		return e
	}
	if _, e = tx.Exec(ctx, "INSERT INTO gateway.runtime_intents(attempt_id,kind,name) VALUES($1,$2,$3) ON CONFLICT DO NOTHING", attempt, in.Kind, in.Name); e != nil {
		return e
	}
	var name string
	if e = tx.QueryRow(ctx, "SELECT name FROM gateway.runtime_intents WHERE attempt_id=$1 AND kind=$2", attempt, in.Kind).Scan(&name); e != nil {
		return e
	}
	if name != in.Name {
		return ErrInvalid
	}
	return tx.Commit(ctx)
}
func (s *Store) RecordResource(ctx context.Context, attempt, owner string, r RuntimeResource) error {
	if !validJournalKind(r.Kind) || !journalName.MatchString(r.ID) || containsSensitive(r.ID, s.KnownSecrets) {
		return ErrInvalid
	}
	tx, _, e := s.liveTx(ctx, attempt, owner)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	var old string
	if e = tx.QueryRow(ctx, "SELECT resource_id FROM gateway.runtime_intents WHERE attempt_id=$1 AND kind=$2 FOR UPDATE", attempt, r.Kind).Scan(&old); e != nil {
		return ErrInvalid
	}
	if old != "" && old != r.ID {
		return ErrInvalid
	}
	if _, e = tx.Exec(ctx, "UPDATE gateway.runtime_intents SET resource_id=$3 WHERE attempt_id=$1 AND kind=$2", attempt, r.Kind, r.ID); e != nil {
		return e
	}
	if r.Kind == "sandbox" {
		if _, e = tx.Exec(ctx, "UPDATE gateway.attempts SET runtime_id=$2 WHERE id=$1", attempt, r.ID); e != nil {
			return e
		}
	}
	return tx.Commit(ctx)
}
func (s *Store) AppendRuntimeEvents(ctx context.Context, attempt, owner string, events []RuntimeEvent) error {
	if len(events) > 100 {
		return ErrInvalid
	}
	tx, id, e := s.liveTx(ctx, attempt, owner)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	if _, e = bindingFrom(ctx, tx, attempt); e != nil {
		return e
	}
	for _, v := range events {
		if v.RuntimeSequence < 1 || v.OccurredAt.IsZero() || v.OccurredAt.After(time.Now().Add(time.Minute)) {
			return ErrInvalid
		}
		switch v.Kind {
		case "status", "progress", "checkpoint", "cleanup":
			if v.Tool != "" {
				return ErrInvalid
			}
		case "tool_start", "tool_result":
			if !liveTools[v.Tool] {
				return ErrInvalid
			}
		default:
			return ErrInvalid
		}
		switch v.Outcome {
		case "started", "running", "succeeded", "failed", "cancelled", "pending", "confirmed":
		default:
			return ErrInvalid
		}
		raw, _ := json.Marshal(v)
		var previous []byte
		e = tx.QueryRow(ctx, "SELECT payload FROM gateway.runtime_events WHERE attempt_id=$1 AND runtime_sequence=$2", attempt, v.RuntimeSequence).Scan(&previous)
		if e == nil {
			var old RuntimeEvent
			if json.Unmarshal(previous, &old) != nil || old != v {
				return ErrInvalid
			}
			continue
		}
		if e != pgx.ErrNoRows {
			return e
		}
		var highest int64
		if e = tx.QueryRow(ctx, "SELECT COALESCE(MAX(runtime_sequence),0) FROM gateway.runtime_events WHERE attempt_id=$1", attempt).Scan(&highest); e != nil {
			return e
		}
		if v.RuntimeSequence != highest+1 {
			return ErrInvalid
		}
		var n int64
		if e = tx.QueryRow(ctx, "UPDATE gateway.executions SET next_sequence=next_sequence+1,updated_at=now() WHERE request_id=$1 RETURNING next_sequence", id).Scan(&n); e != nil {
			return e
		}
		details, _ := json.Marshal(map[string]any{"tool": v.Tool, "status": v.Outcome, "runtimeSequence": v.RuntimeSequence})
		event := ProgressEvent{EventID: fmt.Sprintf("%s:%d", attempt, v.RuntimeSequence), RequestID: id, AttemptID: attempt, Sequence: n, OccurredAt: v.OccurredAt, Kind: v.Kind, Summary: v.Kind + ": " + v.Outcome, Details: details, EvidenceMode: "live"}
		payload, _ := json.Marshal(event)
		if _, e = tx.Exec(ctx, "INSERT INTO gateway.runtime_events(attempt_id,runtime_sequence,payload) VALUES($1,$2,$3)", attempt, v.RuntimeSequence, raw); e != nil {
			return e
		}
		if _, e = tx.Exec(ctx, "INSERT INTO gateway.events(request_id,sequence,event_id,payload) VALUES($1,$2,$3,$4)", id, n, event.EventID, payload); e != nil {
			return e
		}
	}
	return tx.Commit(ctx)
}
func (s *Store) SaveLiveResult(ctx context.Context, attempt, owner string, r LiveResult) error {
	tx, id, e := s.liveTx(ctx, attempt, owner)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	b, e := bindingFrom(ctx, tx, attempt)
	if e != nil {
		return e
	}
	if e = ValidateLiveResult(r, b, s.KnownSecrets...); e != nil {
		return e
	}
	raw, _ := json.Marshal(r)
	var previous []byte
	if e = tx.QueryRow(ctx, "SELECT live_result FROM gateway.attempts WHERE id=$1", attempt).Scan(&previous); e != nil {
		return e
	}
	if len(previous) > 0 {
		var old LiveResult
		if json.Unmarshal(previous, &old) != nil {
			return ErrInvalid
		}
		canonical, _ := json.Marshal(old)
		if !bytes.Equal(canonical, raw) {
			return ErrInvalid
		}
	}
	if _, e = tx.Exec(ctx, "UPDATE gateway.attempts SET live_result=$2 WHERE id=$1", attempt, raw); e != nil {
		return e
	}
	if _, e = tx.Exec(ctx, "UPDATE gateway.executions SET result=$2,stage='checkpointing',updated_at=now() WHERE request_id=$1", id, raw); e != nil {
		return e
	}
	return tx.Commit(ctx)
}
func (s *Store) FinalizeLive(ctx context.Context, attempt, owner string, o LiveOutcome) error {
	switch o.Status {
	case "completed", "failed", "cancelled":
	default:
		return ErrInvalid
	}
	switch o.FailureCode {
	case "", "CheckpointFailed", "NeedsSignIn", "ModelFailed", "Timeout", "InvalidResult", "Interrupted", "Cancelled", "ProviderUnavailable", "PreparationFailed", "CleanupUnconfirmed":
	default:
		return ErrInvalid
	}
	tx, id, e := s.liveTx(ctx, attempt, owner)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	b, e := bindingFrom(ctx, tx, attempt)
	if e != nil {
		return e
	}
	clean := o.SandboxAbsent && o.KeyAbsent && o.AccessRevoked
	if o.Status == "completed" {
		if !clean || !o.Checkpoint.valid() || o.FailureCode != "" {
			return ErrInvalid
		}
		var raw []byte
		var result LiveResult
		if e = tx.QueryRow(ctx, "SELECT live_result FROM gateway.attempts WHERE id=$1", attempt).Scan(&raw); e != nil {
			return e
		}
		if json.Unmarshal(raw, &result) != nil || ValidateLiveResult(result, b, s.KnownSecrets...) != nil {
			return ErrInvalid
		}
	}
	cleanup, _ := json.Marshal(map[string]bool{"sandboxAbsent": o.SandboxAbsent, "keyAbsent": o.KeyAbsent, "accessRevoked": o.AccessRevoked})
	checkpoint, _ := json.Marshal(o.Checkpoint)
	checkpointStatus := "failed"
	if o.Checkpoint.valid() {
		checkpointStatus = "verified"
		if _, e = tx.Exec(ctx, "INSERT INTO gateway.selected_checkpoint(id,reference,attempt_id) VALUES(1,$1,$2) ON CONFLICT(id) DO UPDATE SET reference=EXCLUDED.reference,attempt_id=EXCLUDED.attempt_id,updated_at=now()", checkpoint, attempt); e != nil {
			return e
		}
	}
	status, stage, legacyCleanup := o.Status, "finished", "confirmed"
	if !clean {
		status, stage, legacyCleanup = "blocked", "reconciling", "pending"
	}
	if _, e = tx.Exec(ctx, "UPDATE gateway.executions SET status=$2,stage=$3,cleanup=$4,cleanup_status=$5,checkpoint_status=$6,failure_code=$7,recovery_needed=$8,updated_at=now() WHERE request_id=$1", id, status, stage, legacyCleanup, cleanup, checkpointStatus, o.FailureCode, !clean); e != nil {
		return e
	}
	if o.Checkpoint.valid() {
		ref, _ := json.Marshal(CheckpointRef{Path: o.Checkpoint.ArchiveKey, RequestID: id})
		if _, e = tx.Exec(ctx, "UPDATE gateway.executions SET checkpoint=$2 WHERE request_id=$1", id, ref); e != nil {
			return e
		}
	}
	if clean {
		if _, e = tx.Exec(ctx, "UPDATE gateway.attempts SET state=$2,ended_at=now() WHERE id=$1", attempt, status); e != nil {
			return e
		}
		if _, e = tx.Exec(ctx, "UPDATE gateway.runtime_slot SET request_id=NULL,owner=NULL,unresolved=false WHERE id=1"); e != nil {
			return e
		}
	}
	return tx.Commit(ctx)
}
