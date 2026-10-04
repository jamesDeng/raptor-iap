package execution

import (
	"context"
	"encoding/json"
)

type RuntimeHandle struct{ ID, RequestID string }
type Runtime interface {
	Start(context.Context, ExecutionInput) (RuntimeHandle, error)
	Checkpoint(context.Context, RuntimeHandle) (CheckpointRef, error)
	Stop(context.Context, RuntimeHandle) (CleanupOutcome, error)
	Restore(context.Context, ExecutionInput, CheckpointRef) (RuntimeHandle, error)
}
type RaptorClient interface {
	Context(context.Context, string) (ExecutionInput, error)
}
type Worker struct {
	Store   *Store
	Runtime Runtime
	Raptor  RaptorClient
	Owner   string
}

func (w *Worker) RunNext(ctx context.Context) error {
	x, e := w.Store.ClaimNext(ctx, w.Owner)
	if e != nil || x == nil {
		return e
	}
	in, e := w.Raptor.Context(ctx, x.RequestID)
	if e != nil {
		return w.Store.BlockRecovery(ctx, x.RequestID)
	}
	if in.RequestID != x.RequestID {
		return w.Store.BlockRecovery(ctx, x.RequestID)
	}
	b, e := json.Marshal(in)
	if e != nil {
		return w.Store.BlockRecovery(ctx, x.RequestID)
	}
	if _, e = w.Store.Pool.Exec(ctx, "UPDATE gateway.executions SET input=$2,applied_skills=$3 WHERE request_id=$1", x.RequestID, b, in.Skills); e != nil {
		return e
	}
	h, e := w.Runtime.Start(ctx, in)
	if e != nil {
		return w.Store.BlockRecovery(ctx, x.RequestID)
	}
	if _, e = w.Store.Pool.Exec(ctx, "UPDATE gateway.attempts SET runtime_id=$2 WHERE id=$1", x.AttemptID, h.ID); e != nil {
		return w.Store.BlockRecovery(ctx, x.RequestID)
	}
	if e = w.Store.AppendEvent(ctx, ProgressEvent{EventID: NewID(), RequestID: x.RequestID, AttemptID: x.AttemptID, Kind: "status", Summary: "Simulated runtime started; no infrastructure change executed", EvidenceMode: "simulated"}); e != nil {
		return w.Store.BlockRecovery(ctx, x.RequestID)
	}
	checkpoint, e := w.Runtime.Checkpoint(ctx, h)
	if e != nil {
		return w.Store.BlockRecovery(ctx, x.RequestID)
	}
	cleanup, e := w.Runtime.Stop(ctx, h)
	if e != nil || !cleanup.Confirmed {
		return w.Store.BlockRecovery(ctx, x.RequestID)
	}
	return w.Store.Release(ctx, x.RequestID, w.Owner, "completed", checkpoint)
}
func (s *Store) BlockRecovery(ctx context.Context, id string) error {
	_, e := s.Pool.Exec(ctx, "UPDATE gateway.executions SET status='blocked',recovery_needed=true,updated_at=now() WHERE request_id=$1", id)
	return e
}
func (s *Store) Release(ctx context.Context, id, owner, status string, checkpoint CheckpointRef) error {
	tx, e := s.Pool.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	var current, heldBy *string
	if e = tx.QueryRow(ctx, "SELECT request_id::text,owner FROM gateway.runtime_slot WHERE id=1 FOR UPDATE").Scan(&current, &heldBy); e != nil {
		return e
	}
	if current == nil || heldBy == nil || *current != id || *heldBy != owner {
		return ErrInvalid
	}
	b, e := json.Marshal(checkpoint)
	if e != nil {
		return e
	}
	if _, e = tx.Exec(ctx, "UPDATE gateway.executions SET status=$2,checkpoint=$3,cleanup='confirmed',recovery_needed=false,updated_at=now() WHERE request_id=$1", id, status, b); e != nil {
		return e
	}
	if _, e = tx.Exec(ctx, "UPDATE gateway.attempts SET state=$2,ended_at=now() WHERE request_id=$1 AND ended_at IS NULL", id, status); e != nil {
		return e
	}
	if _, e = tx.Exec(ctx, "UPDATE gateway.runtime_slot SET request_id=NULL,owner=NULL,unresolved=false WHERE id=1"); e != nil {
		return e
	}
	return tx.Commit(ctx)
}
