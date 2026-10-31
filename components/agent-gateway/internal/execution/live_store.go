package execution

import (
	"context"
	"encoding/json"
	"github.com/jackc/pgx/v5"
	"time"
)

func (s *Store) SaveLiveInput(ctx context.Context, attempt, owner string, in ExecutionInput) error {
	tx, id, e := s.liveTx(ctx, attempt, owner)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	raw, _ := json.Marshal(in)
	skills, _ := json.Marshal(in.Skills)
	if in.RequestID != id || containsSensitive(string(raw), s.KnownSecrets) {
		return ErrInvalid
	}
	if _, e = tx.Exec(ctx, "UPDATE gateway.executions SET input=$2,applied_skills=$3 WHERE request_id=$1", id, raw, skills); e != nil {
		return e
	}
	return tx.Commit(ctx)
}
func (s *Store) SetLiveStage(ctx context.Context, attempt, owner, stage string) error {
	switch stage {
	case "preparing", "running", "checkpointing", "cleaning", "reconciling":
	default:
		return ErrInvalid
	}
	tx, id, e := s.liveTx(ctx, attempt, owner)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	if _, e = tx.Exec(ctx, "UPDATE gateway.executions SET stage=$2,updated_at=now() WHERE request_id=$1", id, stage); e != nil {
		return e
	}
	return tx.Commit(ctx)
}
func (s *Store) ActiveOwner(ctx context.Context) (string, string, error) {
	var id, owner *string
	e := s.Pool.QueryRow(ctx, "SELECT request_id::text,owner FROM gateway.runtime_slot WHERE id=1").Scan(&id, &owner)
	if e != nil || id == nil || owner == nil {
		return "", "", e
	}
	return *id, *owner, nil
}
func (s *Store) LiveCancelled(ctx context.Context, id string) (bool, error) {
	var found bool
	e := s.Pool.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM gateway.signals WHERE request_id=$1 AND kind='cancel')", id).Scan(&found)
	return found, e
}
func (s *Store) RecoveryRecord(ctx context.Context, attempt string) (RecoveryRecord, error) {
	var r RecoveryRecord
	var binding, result, checkpoint []byte
	var started time.Time
	if e := s.Pool.QueryRow(ctx, "SELECT binding,live_result,verified_checkpoint,started_at FROM gateway.attempts WHERE id=$1", attempt).Scan(&binding, &result, &checkpoint, &started); e != nil {
		return r, e
	}
	if json.Unmarshal(binding, &r.Binding) != nil || !r.Binding.journalValid() {
		return r, ErrInvalid
	}
	var issued struct {
		ReplacementScope *ReplacementScope `json:"replacementScope"`
	}
	if json.Unmarshal(binding, &issued) != nil {
		return r, ErrInvalid
	}
	r.ReplacementScope = issued.ReplacementScope
	if r.ReplacementScope != nil && !r.ReplacementScope.ResourceValidFor(r.Binding) {
		return r, ErrInvalid
	}
	if len(checkpoint) > 0 {
		if json.Unmarshal(checkpoint, &r.Checkpoint) != nil || !r.Checkpoint.valid() {
			return r, ErrInvalid
		}
	}
	var waitRaw, waitCheckpoint []byte
	waitErr := s.Pool.QueryRow(ctx, "SELECT wait,checkpoint FROM gateway.live_waits WHERE attempt_id=$1 AND phase='checkpointed'", attempt).Scan(&waitRaw, &waitCheckpoint)
	if waitErr != nil && waitErr != pgx.ErrNoRows {
		return r, waitErr
	}
	if waitErr == nil {
		var wait LiveWait
		var cp SessionCheckpoint
		if json.Unmarshal(waitRaw, &wait) != nil || !wait.Valid() || json.Unmarshal(waitCheckpoint, &cp) != nil || !cp.validFor(r.Binding) {
			return r, ErrInvalid
		}
		r.Wait = &wait
		r.Checkpoint = cp.Archive
	}
	r.Deadline = started.Add(600 * time.Second)
	if len(result) > 0 && json.Unmarshal(result, &r.Result) != nil {
		return r, ErrInvalid
	}
	rows, e := s.Pool.Query(ctx, "SELECT kind,name,resource_id FROM gateway.runtime_intents WHERE attempt_id=$1 ORDER BY created_at,kind", attempt)
	if e != nil {
		return r, e
	}
	defer rows.Close()
	for rows.Next() {
		var in RuntimeIntentRecord
		if e = rows.Scan(&in.Kind, &in.Name, &in.ResourceID); e != nil {
			return r, e
		}
		r.Intents = append(r.Intents, in)
	}
	return r, rows.Err()
}
func (s *Store) SelectedCheckpoint(ctx context.Context, bootstrap VerifiedCheckpoint) (VerifiedCheckpoint, error) {
	var raw []byte
	e := s.Pool.QueryRow(ctx, "SELECT reference FROM gateway.selected_checkpoint WHERE id=1").Scan(&raw)
	if e == pgx.ErrNoRows {
		if !bootstrap.valid() {
			return VerifiedCheckpoint{}, ErrInvalid
		}
		return bootstrap, nil
	}
	var r VerifiedCheckpoint
	if e != nil {
		return r, e
	}
	if json.Unmarshal(raw, &r) != nil || !r.valid() {
		return r, ErrInvalid
	}
	return r, nil
}

// TakeOverLive must only be called by the process holding the exclusive live
// driver advisory lease. It fences the old owner before any resource mutation.
func (s *Store) TakeOverLive(ctx context.Context, attempt, owner string) error {
	if owner == "" {
		return ErrInvalid
	}
	tx, e := s.beginLive(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	var id *string
	if e = tx.QueryRow(ctx, "SELECT request_id::text FROM gateway.runtime_slot WHERE id=1 FOR UPDATE").Scan(&id); e != nil {
		return e
	}
	if id == nil {
		return ErrInvalid
	}
	var matches bool
	if e = tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM gateway.executions WHERE request_id=$1 AND attempt_id=$2 AND runtime_mode='live')", *id, attempt).Scan(&matches); e != nil {
		return e
	}
	if !matches {
		return ErrInvalid
	}
	if _, e = tx.Exec(ctx, "UPDATE gateway.runtime_slot SET owner=$1,unresolved=true WHERE id=1", owner); e != nil {
		return e
	}
	if _, e = tx.Exec(ctx, "UPDATE gateway.executions SET status='blocked',stage='reconciling',recovery_needed=true WHERE request_id=$1", *id); e != nil {
		return e
	}
	return tx.Commit(ctx)
}
func (s *Store) RejectLiveClaim(ctx context.Context, attempt, owner, code string) error {
	if code != "InvalidResult" {
		return ErrInvalid
	}
	tx, id, e := s.liveTx(ctx, attempt, owner)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	var n int
	if e = tx.QueryRow(ctx, "SELECT count(*) FROM gateway.runtime_intents WHERE attempt_id=$1", attempt).Scan(&n); e != nil {
		return e
	}
	if n != 0 {
		return ErrInvalid
	}
	if _, e = tx.Exec(ctx, "UPDATE gateway.executions SET status='failed',runtime_mode='live',stage='finished',cleanup='confirmed',failure_code=$2,recovery_needed=false WHERE request_id=$1", id, code); e != nil {
		return e
	}
	if _, e = tx.Exec(ctx, "UPDATE gateway.attempts SET state='failed',ended_at=now() WHERE id=$1", attempt); e != nil {
		return e
	}
	if _, e = tx.Exec(ctx, "UPDATE gateway.runtime_slot SET request_id=NULL,owner=NULL,unresolved=false WHERE id=1"); e != nil {
		return e
	}
	return tx.Commit(ctx)
}

func (s *Store) ReconcileUnbound(ctx context.Context, attempt, owner string) (bool, error) {
	tx, id, e := s.liveTx(ctx, attempt, owner)
	if e != nil {
		return false, e
	}
	defer tx.Rollback(ctx)
	var binding []byte
	var intents int
	if e = tx.QueryRow(ctx, "SELECT binding,(SELECT count(*) FROM gateway.runtime_intents WHERE attempt_id=$1) FROM gateway.attempts WHERE id=$1", attempt).Scan(&binding, &intents); e != nil {
		return false, e
	}
	if len(binding) > 0 {
		return false, nil
	}
	if intents != 0 {
		return false, ErrUnavailable
	}
	if _, e = tx.Exec(ctx, "UPDATE gateway.executions SET status='failed',stage='finished',failure_code='Interrupted',cleanup='confirmed',cleanup_status='{\"sandboxAbsent\":true,\"keyAbsent\":true,\"accessRevoked\":true}',recovery_needed=false WHERE request_id=$1", id); e != nil {
		return false, e
	}
	if _, e = tx.Exec(ctx, "UPDATE gateway.attempts SET state='failed',ended_at=now() WHERE id=$1", attempt); e != nil {
		return false, e
	}
	if _, e = tx.Exec(ctx, "UPDATE gateway.runtime_slot SET request_id=NULL,owner=NULL,unresolved=false WHERE id=1"); e != nil {
		return false, e
	}
	return true, tx.Commit(ctx)
}

// SaveIssuedScope freezes the trusted access proof inside the existing private
// attempt binding JSON, before runtime dispatch. It never stores credentials.
func (s *Store) SaveIssuedScope(ctx context.Context, attempt, owner string, scope ReplacementScope) error {
	tx, id, e := s.liveTx(ctx, attempt, owner)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	b, e := bindingFrom(ctx, tx, attempt)
	if e != nil {
		return e
	}
	if !scope.ResourceValidFor(b) {
		return ErrInvalid
	}
	rows, e := tx.Query(ctx, "SELECT id::text,binding FROM gateway.attempts WHERE request_id=$1 ORDER BY started_at,id", id)
	if e != nil {
		return e
	}
	var current map[string]json.RawMessage
	for rows.Next() {
		var other string
		var raw []byte
		if e = rows.Scan(&other, &raw); e != nil {
			rows.Close()
			return e
		}
		if len(raw) == 0 {
			if other != attempt {
				rows.Close()
				return ErrInvalid
			}
			continue
		}
		var saved struct {
			ReplacementScope *ReplacementScope `json:"replacementScope"`
		}
		var binding AttemptBinding
		if json.Unmarshal(raw, &saved) != nil || json.Unmarshal(raw, &binding) != nil {
			rows.Close()
			return ErrInvalid
		}
		if other == attempt {
			if json.Unmarshal(raw, &current) != nil {
				rows.Close()
				return ErrInvalid
			}
		} else if saved.ReplacementScope == nil {
			rows.Close()
			return ErrInvalid
		}
		if saved.ReplacementScope != nil && (!saved.ReplacementScope.ResourceValidFor(binding) || *saved.ReplacementScope != scope || binding.DefinitionSHA256 != b.DefinitionSHA256 || binding.SkillsCommit != b.SkillsCommit || binding.Model != b.Model) {
			rows.Close()
			return ErrInvalid
		}
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return e
	}
	if current == nil {
		return ErrInvalid
	}
	raw, _ := json.Marshal(scope)
	current["replacementScope"] = raw
	encoded, _ := json.Marshal(current)
	if _, e = tx.Exec(ctx, "UPDATE gateway.attempts SET binding=$2 WHERE id=$1", attempt, encoded); e != nil {
		return e
	}
	return tx.Commit(ctx)
}

func persistedResultScope(ctx context.Context, tx pgx.Tx, attempt string, b AttemptBinding, r LiveResult) error {
	if b.Operation != "db-proxy.replace-nodes" {
		return nil
	}
	if b.ExecutionMode == "qualification" && (r.Qualification == nil || r.Qualification.Version != 2) {
		return ErrInvalid
	}
	var raw []byte
	if e := tx.QueryRow(ctx, "SELECT binding FROM gateway.attempts WHERE id=$1", attempt).Scan(&raw); e != nil {
		return e
	}
	var saved struct {
		ReplacementScope *ReplacementScope `json:"replacementScope"`
	}
	if json.Unmarshal(raw, &saved) != nil || saved.ReplacementScope == nil || !saved.ReplacementScope.ResourceValidFor(b) || resultScope(&r) == nil || *resultScope(&r) != *saved.ReplacementScope {
		return ErrInvalid
	}
	return nil
}
