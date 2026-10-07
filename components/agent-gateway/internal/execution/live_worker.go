package execution

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"time"
	"unicode/utf8"
)

type WorkerLease interface{ Valid(context.Context) bool }
type AgentAccess struct {
	Credential   string         `json:"-"`
	ExpiresAt    time.Time      `json:"expiresAt"`
	Binding      AttemptBinding `json:"binding"`
	RaptorMcpURL string         `json:"raptorMcpUrl"`
	InfraMcpURL  string         `json:"infraMcpUrl"`
}

func (a AgentAccess) String() string   { return "[request-bound agent access]" }
func (a AgentAccess) GoString() string { return a.String() }

type AgentAccessClient interface {
	Issue(context.Context, AttemptBinding, string, time.Time) (AgentAccess, error)
	Revoke(context.Context, AttemptBinding) error
}
type LiveStart struct {
	Binding    AttemptBinding
	Question   string
	Checkpoint VerifiedCheckpoint
	Access     AgentAccess
	Deadline   time.Time
}
type LiveObservation struct {
	Events      []RuntimeEvent
	Terminal    bool
	Result      *LiveResult
	FailureCode string
}
type LiveCleanup struct{ SandboxAbsent, KeyAbsent bool }
type RecoveryRecord struct {
	Binding  AttemptBinding
	Intents  []RuntimeIntentRecord
	Result   *LiveResult
	Deadline time.Time
}
type RuntimeIntentRecord struct{ Kind, Name, ResourceID string }
type RecoveredRun struct {
	Result     *LiveResult
	Checkpoint VerifiedCheckpoint
	Cleanup    LiveCleanup
}
type LiveRuntime interface {
	Start(context.Context, LiveStart) (RuntimeHandle, error)
	Poll(context.Context, RuntimeHandle, int64) (LiveObservation, error)
	Checkpoint(context.Context, RuntimeHandle) (VerifiedCheckpoint, error)
	Cancel(context.Context, RuntimeHandle) error
	Stop(context.Context, RuntimeHandle) (LiveCleanup, error)
	Reconcile(context.Context, RecoveryRecord) (RecoveredRun, error)
}
type LiveWorker struct {
	Store     *Store
	Owner     string
	Lease     WorkerLease
	Runtime   LiveRuntime
	Access    AgentAccessClient
	Raptor    RaptorClient
	Bootstrap VerifiedCheckpoint
}

func (w *LiveWorker) authorized(ctx context.Context) error {
	if w.Lease == nil || !w.Lease.Valid(ctx) {
		return ErrUnavailable
	}
	return nil
}
func ParseLiveQuestion(in ExecutionInput, attempt string) (AttemptBinding, string, string, error) {
	var d struct {
		Operation  string                           `json:"operation"`
		Object     struct{ Kind, Code string }      `json:"object"`
		EnvCode    string                           `json:"envCode"`
		Parameters struct{ Question, Model string } `json:"parameters"`
	}
	if json.Unmarshal(in.Definition, &d) != nil || d.Parameters.Question == "" || !utf8.ValidString(d.Parameters.Question) || utf8.RuneCountInString(d.Parameters.Question) > 2000 || len(d.Parameters.Question) > 8192 {
		return AttemptBinding{}, "", "", ErrInvalid
	}
	b := AttemptBinding{RequestID: in.RequestID, AttemptID: attempt, Operation: d.Operation, ObjectKind: d.Object.Kind, ObjectCode: d.Object.Code, EnvCode: d.EnvCode, SkillsCommit: in.Skills.CommitSHA, Model: d.Parameters.Model}
	if !b.valid() {
		return b, "", "", ErrInvalid
	}
	raw, e := json.Marshal(in)
	if e != nil {
		return b, "", "", ErrInvalid
	}
	hash := sha256.Sum256(raw)
	return b, d.Parameters.Question, hex.EncodeToString(hash[:]), nil
}
func (w *LiveWorker) RunNext(ctx context.Context) error {
	if e := w.authorized(ctx); e != nil {
		return e
	}
	x, e := w.Store.ClaimNext(ctx, w.Owner)
	if e != nil || x == nil {
		return e
	}
	in, e := w.Raptor.Context(ctx, x.RequestID)
	if e != nil || in.RequestID != x.RequestID {
		return w.Store.RejectLiveClaim(ctx, x.AttemptID, w.Owner, "InvalidResult")
	}
	b, question, hash, e := ParseLiveQuestion(in, x.AttemptID)
	if e != nil {
		return w.Store.RejectLiveClaim(ctx, x.AttemptID, w.Owner, "InvalidResult")
	}
	if e = w.Store.BindLive(ctx, x.AttemptID, w.Owner, b, hash); e != nil {
		return e
	}
	if e = w.Store.SaveLiveInput(ctx, x.AttemptID, w.Owner, in); e != nil {
		return e
	}
	record, e := w.Store.RecoveryRecord(ctx, x.AttemptID)
	if e != nil {
		return e
	}
	checkpoint, e := w.Store.SelectedCheckpoint(ctx, w.Bootstrap)
	if e != nil {
		return w.finish(ctx, b, RuntimeHandle{RequestID: b.RequestID}, "failed", "CheckpointFailed")
	}
	if e = w.authorized(ctx); e != nil {
		return e
	}
	if e = w.Store.RecordIntent(ctx, b.AttemptID, w.Owner, RuntimeIntent{Kind: "access", Name: b.AttemptID}); e != nil {
		return e
	}
	access, e := w.Access.Issue(ctx, b, hash, record.Deadline)
	if e != nil {
		return w.finish(ctx, b, RuntimeHandle{RequestID: b.RequestID}, "failed", "NeedsSignIn")
	}
	if access.ExpiresAt.Before(time.Now()) || access.ExpiresAt.After(record.Deadline) {
		return w.finish(ctx, b, RuntimeHandle{RequestID: b.RequestID}, "failed", "NeedsSignIn")
	}
	if e = w.authorized(ctx); e != nil {
		return e
	}
	h, e := w.Runtime.Start(ctx, LiveStart{Binding: b, Question: question, Checkpoint: checkpoint, Access: access, Deadline: record.Deadline})
	if e != nil {
		return w.finish(ctx, b, h, "failed", "PreparationFailed")
	}
	return w.Store.SetLiveStage(ctx, b.AttemptID, w.Owner, "running")
}
func (w *LiveWorker) RunActive(ctx context.Context) error {
	if e := w.authorized(ctx); e != nil {
		return e
	}
	id, owner, e := w.Store.ActiveOwner(ctx)
	if e != nil || id == "" || owner != w.Owner {
		return e
	}
	x, e := w.Store.Get(ctx, id)
	if e != nil || x.RuntimeMode != "live" || x.RecoveryNeeded {
		return e
	}
	record, e := w.Store.RecoveryRecord(ctx, x.AttemptID)
	if e != nil {
		return e
	}
	b := record.Binding
	h := RuntimeHandle{RequestID: id}
	if e = w.Store.Pool.QueryRow(ctx, "SELECT runtime_id FROM gateway.attempts WHERE id=$1", x.AttemptID).Scan(&h.ID); e != nil {
		return e
	}
	cancel, e := w.Store.LiveCancelled(ctx, id)
	if e != nil {
		return e
	}
	if cancel {
		if e = w.Runtime.Cancel(ctx, h); e != nil {
			return w.finish(ctx, b, h, "cancelled", "Cancelled")
		}
	}
	if time.Now().After(record.Deadline) {
		w.Runtime.Cancel(ctx, h)
		return w.finish(ctx, b, h, "failed", "Timeout")
	}
	var cursor int64
	if e = w.Store.Pool.QueryRow(ctx, "SELECT COALESCE(MAX(runtime_sequence),0) FROM gateway.runtime_events WHERE attempt_id=$1", x.AttemptID).Scan(&cursor); e != nil {
		return e
	}
	o, e := w.Runtime.Poll(ctx, h, cursor)
	if e != nil {
		return w.finish(ctx, b, h, "failed", "ProviderUnavailable")
	}
	if e = w.Store.AppendRuntimeEvents(ctx, x.AttemptID, w.Owner, o.Events); e != nil {
		return e
	}
	if !o.Terminal {
		return nil
	}
	status, code := "completed", ""
	if o.FailureCode != "" {
		status, code = "failed", o.FailureCode
	}
	if cancel {
		status, code = "cancelled", "Cancelled"
	}
	if o.Result != nil {
		if e = w.Store.SaveLiveResult(ctx, x.AttemptID, w.Owner, *o.Result); e != nil {
			status, code = "failed", "InvalidResult"
		}
	} else if status == "completed" {
		status, code = "failed", "InvalidResult"
	}
	return w.finish(ctx, b, h, status, code)
}
func (w *LiveWorker) finish(ctx context.Context, b AttemptBinding, h RuntimeHandle, status, code string) error {
	if e := w.authorized(ctx); e != nil {
		return e
	}
	w.Store.SetLiveStage(ctx, b.AttemptID, w.Owner, "checkpointing")
	checkpoint, checkpointErr := w.Runtime.Checkpoint(ctx, h)
	if checkpointErr != nil && status == "completed" {
		status, code = "failed", "CheckpointFailed"
	}
	w.Store.SetLiveStage(ctx, b.AttemptID, w.Owner, "cleaning")
	cleanup, stopErr := w.Runtime.Stop(ctx, h)
	if e := w.authorized(ctx); e != nil {
		return e
	}
	revokeErr := w.Access.Revoke(ctx, b)
	if stopErr != nil || revokeErr != nil || !cleanup.SandboxAbsent || !cleanup.KeyAbsent {
		if code == "" {
			code = "CleanupUnconfirmed"
		}
	}
	if status == "completed" && (!cleanup.SandboxAbsent || !cleanup.KeyAbsent || revokeErr != nil) {
		status = "failed"
	}
	return w.Store.FinalizeLive(ctx, b.AttemptID, w.Owner, LiveOutcome{Status: status, FailureCode: code, Checkpoint: checkpoint, SandboxAbsent: cleanup.SandboxAbsent, KeyAbsent: cleanup.KeyAbsent, AccessRevoked: revokeErr == nil})
}
func (w *LiveWorker) Reconcile(ctx context.Context) error {
	if e := w.authorized(ctx); e != nil {
		return e
	}
	id, owner, e := w.Store.ActiveOwner(ctx)
	if e != nil || id == "" {
		return e
	}
	x, e := w.Store.Get(ctx, id)
	if e != nil {
		return e
	}
	if x.RuntimeMode != "live" {
		return ErrUnavailable
	}
	if owner != w.Owner {
		if e = w.Store.TakeOverLive(ctx, x.AttemptID, w.Owner); e != nil {
			return e
		}
	}
	record, e := w.Store.RecoveryRecord(ctx, x.AttemptID)
	if e != nil {
		return e
	}
	recovered, e := w.Runtime.Reconcile(ctx, record)
	if e != nil {
		if e = w.authorized(ctx); e != nil {
			return e
		}
		revokeErr := w.Access.Revoke(ctx, record.Binding)
		return w.Store.FinalizeLive(ctx, x.AttemptID, w.Owner, LiveOutcome{Status: "failed", FailureCode: "CleanupUnconfirmed", AccessRevoked: revokeErr == nil})
	}
	status, code := "failed", "Interrupted"
	if recovered.Result != nil && recovered.Checkpoint.valid() {
		if e = w.Store.SaveLiveResult(ctx, x.AttemptID, w.Owner, *recovered.Result); e == nil {
			status, code = "completed", ""
		}
	}
	cancel, e := w.Store.LiveCancelled(ctx, id)
	if e != nil {
		return e
	}
	if cancel {
		status, code = "cancelled", "Cancelled"
	}
	if e = w.authorized(ctx); e != nil {
		return e
	}
	revokeErr := w.Access.Revoke(ctx, record.Binding)
	if status == "completed" && (!recovered.Cleanup.KeyAbsent || !recovered.Cleanup.SandboxAbsent || revokeErr != nil) {
		status, code = "failed", "CleanupUnconfirmed"
	}
	return w.Store.FinalizeLive(ctx, x.AttemptID, w.Owner, LiveOutcome{Status: status, FailureCode: code, Checkpoint: recovered.Checkpoint, SandboxAbsent: recovered.Cleanup.SandboxAbsent, KeyAbsent: recovered.Cleanup.KeyAbsent, AccessRevoked: revokeErr == nil})
}
