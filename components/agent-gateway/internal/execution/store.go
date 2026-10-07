package execution

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"time"
)

var ErrUnavailable = errors.New("Unavailable")
var ErrInvalid = errors.New("InvalidInput")

type Store struct {
	Pool         *pgxpool.Pool
	KnownSecrets []string
}

func (s *Store) Get(ctx context.Context, id string) (Execution, error) {
	var x Execution
	var input, skills, checkpoint, result []byte
	var attempt *string
	e := s.Pool.QueryRow(ctx, "SELECT request_id::text,status,attempt_id::text,input,applied_skills,checkpoint,cleanup,recovery_needed,updated_at,runtime_mode,stage,result,checkpoint_status,cleanup_status,failure_code FROM gateway.executions WHERE request_id=$1", id).Scan(&x.RequestID, &x.Status, &attempt, &input, &skills, &checkpoint, &x.Cleanup, &x.RecoveryNeeded, &x.UpdatedAt, &x.RuntimeMode, &x.Stage, &result, &x.CheckpointStatus, &x.CleanupStatus, &x.FailureCode)
	if e != nil {
		return x, e
	}
	if attempt != nil {
		x.AttemptID = *attempt
	}
	json.Unmarshal(input, &x.Input)
	json.Unmarshal(skills, &x.AppliedSkills)
	json.Unmarshal(checkpoint, &x.Checkpoint)
	if len(result) > 0 {
		if err := json.Unmarshal(result, &x.Result); err != nil {
			return x, ErrUnavailable
		}
	}
	return x, nil
}
func (s *Store) Receive(ctx context.Context, id string) (Execution, error) {
	if _, e := s.Pool.Exec(ctx, "INSERT INTO gateway.executions(request_id) VALUES($1) ON CONFLICT DO NOTHING", id); e != nil {
		return Execution{}, ErrInvalid
	}
	return s.Get(ctx, id)
}
func (s *Store) ClaimNext(ctx context.Context, owner string) (*Execution, error) {
	if owner == "" {
		return nil, ErrInvalid
	}
	tx, e := s.Pool.Begin(ctx)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback(ctx)
	var active *string
	var unresolved bool
	if e = tx.QueryRow(ctx, "SELECT request_id::text,unresolved FROM gateway.runtime_slot WHERE id=1 FOR UPDATE").Scan(&active, &unresolved); e != nil {
		return nil, e
	}
	if active != nil || unresolved {
		return nil, nil
	}
	var id string
	e = tx.QueryRow(ctx, "SELECT request_id::text FROM gateway.executions WHERE status='queued' AND NOT recovery_needed ORDER BY queued_at,request_id LIMIT 1 FOR UPDATE").Scan(&id)
	if errors.Is(e, pgx.ErrNoRows) {
		return nil, nil
	}
	if e != nil {
		return nil, e
	}
	attempt := NewID()
	if _, e = tx.Exec(ctx, "UPDATE gateway.runtime_slot SET request_id=$1,owner=$2,unresolved=true WHERE id=1", id, owner); e != nil {
		return nil, e
	}
	if _, e = tx.Exec(ctx, "UPDATE gateway.executions SET status='running',attempt_id=$2,cleanup='pending',updated_at=now() WHERE request_id=$1", id, attempt); e != nil {
		return nil, e
	}
	if _, e = tx.Exec(ctx, "INSERT INTO gateway.attempts(id,request_id,state) VALUES($1,$2,'running')", attempt, id); e != nil {
		return nil, e
	}
	if e = tx.Commit(ctx); e != nil {
		return nil, e
	}
	x, e := s.Get(ctx, id)
	return &x, e
}
func (s *Store) AppendEvent(ctx context.Context, v ProgressEvent) error {
	var sanitationError error
	v, sanitationError = sanitizeEvent(v, s.KnownSecrets...)
	if sanitationError != nil {
		return sanitationError
	}
	if v.EventID == "" || v.RequestID == "" || len(v.Summary) > 4096 || len(v.Details) > 4096 {
		return ErrInvalid
	}
	if v.EvidenceMode != "simulated" {
		return ErrInvalid
	}
	tx, e := s.Pool.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	var n int64
	if e = tx.QueryRow(ctx, "SELECT next_sequence FROM gateway.executions WHERE request_id=$1 FOR UPDATE", v.RequestID).Scan(&n); e != nil {
		return e
	}
	var exists bool
	if e = tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM gateway.events WHERE request_id=$1 AND event_id=$2)", v.RequestID, v.EventID).Scan(&exists); e != nil {
		return e
	}
	if exists {
		return tx.Commit(ctx)
	}
	v.Sequence = n + 1
	v.OccurredAt = time.Now().UTC()
	b, e := json.Marshal(v)
	if e != nil || len(b) > 16384 {
		return ErrInvalid
	}
	if _, e = tx.Exec(ctx, "INSERT INTO gateway.events(request_id,sequence,event_id,payload) VALUES($1,$2,$3,$4)", v.RequestID, v.Sequence, v.EventID, b); e != nil {
		return e
	}
	if _, e = tx.Exec(ctx, "UPDATE gateway.executions SET next_sequence=$2 WHERE request_id=$1", v.RequestID, v.Sequence); e != nil {
		return e
	}
	return tx.Commit(ctx)
}
func (s *Store) Events(ctx context.Context, id string, after int64) ([]ProgressEvent, error) {
	rows, e := s.Pool.Query(ctx, "SELECT payload FROM gateway.events WHERE request_id=$1 AND sequence>$2 ORDER BY sequence LIMIT 101", id, after)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []ProgressEvent{}
	for rows.Next() {
		var b []byte
		var v ProgressEvent
		if e = rows.Scan(&b); e != nil {
			return nil, e
		}
		if e = json.Unmarshal(b, &v); e != nil {
			return nil, e
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
