package execution

import (
	"context"
	"encoding/json"
	"github.com/jackc/pgx/v5"
	"regexp"
	"time"
)

type LiveWait struct {
	WakeAfterSeconds int       `json:"wakeAfterSeconds,omitempty"`
	Kind             string    `json:"kind"`
	ApprovalID       string    `json:"approvalId"`
	ActionID         string    `json:"actionId"`
	BindingDigest    string    `json:"bindingDigest"`
	StartedAt        time.Time `json:"startedAt"`
}

func (w LiveWait) Valid() bool {
	if !journalName.MatchString(w.ActionID) || !shaPattern.MatchString(w.BindingDigest) || w.StartedAt.IsZero() || w.StartedAt.After(time.Now().Add(time.Second)) {
		return false
	}
	if w.Kind == "resource" {
		return w.ApprovalID == "" && w.WakeAfterSeconds == 60
	}
	return w.Kind == "approval" && w.WakeAfterSeconds == 0 && regexp.MustCompile(`^[a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12}$`).MatchString(w.ApprovalID)
}

func (s *Store) SaveLiveWait(ctx context.Context, attempt, owner string, w LiveWait, c SessionCheckpoint) error {
	if !w.Valid() {
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
	if e = tx.QueryRow(ctx, "SELECT definition_sha256 FROM gateway.attempts WHERE id=$1", attempt).Scan(&b.DefinitionSHA256); e != nil {
		return e
	}
	if id != c.RequestID || !c.validFor(b) {
		return ErrInvalid
	}
	raw, _ := json.Marshal(w)
	checkpoint, _ := json.Marshal(c)
	if containsSensitive(string(raw)+string(checkpoint), s.KnownSecrets) {
		return ErrInvalid
	}
	var old, previous []byte
	e = tx.QueryRow(ctx, "SELECT wait,checkpoint FROM gateway.live_waits WHERE attempt_id=$1 FOR UPDATE", attempt).Scan(&old, &previous)
	if e == nil {
		var prior LiveWait
		var saved SessionCheckpoint
		if json.Unmarshal(old, &prior) != nil || json.Unmarshal(previous, &saved) != nil {
			return ErrInvalid
		}
		sameTime := saved.Archive.VerifiedAt.Equal(c.Archive.VerifiedAt)
		saved.Archive.VerifiedAt = c.Archive.VerifiedAt
		if !sameTime || prior.Kind != w.Kind || prior.ApprovalID != w.ApprovalID || prior.ActionID != w.ActionID || prior.BindingDigest != w.BindingDigest || !prior.StartedAt.Equal(w.StartedAt) || prior.WakeAfterSeconds != w.WakeAfterSeconds || saved != c {
			return ErrInvalid
		}
		return tx.Commit(ctx)
	}
	if e != pgx.ErrNoRows {
		return e
	}
	var approvalID any = w.ApprovalID
	stage := "waiting_approval"
	if w.Kind == "resource" {
		if b.Operation != "db-proxy.replace-nodes" || w.BindingDigest != b.DefinitionSHA256 {
			return ErrInvalid
		}
		var count int
		if e = tx.QueryRow(ctx, "SELECT count(*) FROM gateway.live_waits WHERE request_id=$1 AND wait->>'kind'='resource'", id).Scan(&count); e != nil {
			return e
		}
		if count >= 10 {
			return ErrInvalid
		}
		approvalID = nil
		stage = "waiting_resource"
	}
	if _, e = tx.Exec(ctx, "INSERT INTO gateway.live_waits(attempt_id,request_id,approval_id,action_id,wait,checkpoint) VALUES($1,$2,$3,$4,$5,$6)", attempt, id, approvalID, w.ActionID, raw, checkpoint); e != nil {
		return e
	}
	if _, e = tx.Exec(ctx, "UPDATE gateway.executions SET stage=$2,updated_at=now() WHERE request_id=$1", id, stage); e != nil {
		return e
	}
	return tx.Commit(ctx)
}

// Release only after provider absence and access revocation are confirmed. A
// failed cleanup keeps the global slot fenced for explicit recovery.
func (s *Store) ReleaseLiveWait(ctx context.Context, attempt, owner string, cleanup LiveCleanup, accessRevoked bool) error {
	tx, id, e := s.liveTx(ctx, attempt, owner)
	if e != nil {
		var phase string
		if s.Pool.QueryRow(ctx, "SELECT phase FROM gateway.live_waits WHERE attempt_id=$1", attempt).Scan(&phase) == nil && phase == "released" {
			return nil
		}
		return e
	}
	defer tx.Rollback(ctx)
	var raw, waitRaw []byte
	var phase string
	if e = tx.QueryRow(ctx, "SELECT checkpoint,phase,wait FROM gateway.live_waits WHERE attempt_id=$1 FOR UPDATE", attempt).Scan(&raw, &phase, &waitRaw); e != nil {
		return e
	}
	var wait LiveWait
	if json.Unmarshal(waitRaw, &wait) != nil || !wait.Valid() {
		return ErrInvalid
	}
	status := "waiting_approval"
	if wait.Kind == "resource" {
		status = "waiting_resource"
	}
	if phase != "checkpointed" {
		return ErrInvalid
	}
	b, e := bindingFrom(ctx, tx, attempt)
	if e != nil {
		return e
	}
	if e = tx.QueryRow(ctx, "SELECT definition_sha256 FROM gateway.attempts WHERE id=$1", attempt).Scan(&b.DefinitionSHA256); e != nil {
		return e
	}
	var c SessionCheckpoint
	if json.Unmarshal(raw, &c) != nil || !c.validFor(b) {
		return ErrInvalid
	}
	proof, _ := json.Marshal(map[string]bool{"sandboxAbsent": cleanup.SandboxAbsent, "keyAbsent": cleanup.KeyAbsent, "accessRevoked": accessRevoked})
	if !cleanup.SandboxAbsent || !cleanup.KeyAbsent || !accessRevoked {
		if _, e = tx.Exec(ctx, "UPDATE gateway.executions SET status='blocked',stage='reconciling',recovery_needed=true,cleanup_status=$2,updated_at=now() WHERE request_id=$1", id, proof); e != nil {
			return e
		}
		if _, e = tx.Exec(ctx, "UPDATE gateway.runtime_slot SET unresolved=true WHERE id=1"); e != nil {
			return e
		}
		if e = tx.Commit(ctx); e != nil {
			return e
		}
		return ErrUnavailable
	}
	if _, e = tx.Exec(ctx, `INSERT INTO gateway.operation_sessions(request_id,attempt_id,checkpoint) VALUES($1,$2,$3) ON CONFLICT(request_id) DO UPDATE SET attempt_id=excluded.attempt_id,checkpoint=excluded.checkpoint,updated_at=now()`, id, attempt, raw); e != nil {
		return e
	}
	if _, e = tx.Exec(ctx, "UPDATE gateway.live_waits SET phase='released' WHERE attempt_id=$1", attempt); e != nil {
		return e
	}
	if _, e = tx.Exec(ctx, "UPDATE gateway.attempts SET state=$2,ended_at=now() WHERE id=$1", attempt, status); e != nil {
		return e
	}
	if _, e = tx.Exec(ctx, "UPDATE gateway.executions SET status=$3,stage=$3,recovery_needed=false,cleanup_status=$2,updated_at=now() WHERE request_id=$1", id, proof, status); e != nil {
		return e
	}
	if _, e = tx.Exec(ctx, "UPDATE gateway.runtime_slot SET request_id=NULL,owner=NULL,unresolved=false WHERE id=1"); e != nil {
		return e
	}
	return tx.Commit(ctx)
}
