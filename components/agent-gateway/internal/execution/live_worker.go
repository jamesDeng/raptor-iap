package execution

import (
	"context"
	"github.com/jackc/pgx/v5"
	"time"
)

type WorkerLease interface{ Valid(context.Context) bool }
type AgentAccess struct {
	ReplacementScope *ReplacementScope `json:"replacementScope,omitempty"`
	Credential       string            `json:"-"`
	ExpiresAt        time.Time         `json:"expiresAt"`
	Binding          AttemptBinding    `json:"binding"`
	RaptorMcpURL     string            `json:"raptorMcpUrl"`
	InfraMcpURL      string            `json:"infraMcpUrl"`
}

func (a AgentAccess) String() string   { return "[request-bound agent access]" }
func (a AgentAccess) GoString() string { return a.String() }

type AgentAccessClient interface {
	Issue(context.Context, AttemptBinding, string, time.Time) (AgentAccess, error)
	Revoke(context.Context, AttemptBinding) error
}
type ConversationRuntime interface {
	DeliverMessage(context.Context, RuntimeHandle, ConversationInput) error
}
type LiveStart struct {
	ConversationEnabled bool
	Conversation        *ConversationSession
	InitialMessage      *ConversationInput
	Skills              SkillsVersion
	DecisionContext     *ContinuationContext
	SessionCheckpoint   *SessionCheckpoint
	Binding             AttemptBinding
	Question            string
	Checkpoint          VerifiedCheckpoint
	Access              AgentAccess
	Deadline            time.Time
}
type LiveObservation struct {
	SessionID   string
	SessionFile string
	Turns       []ConversationTurn
	Paused      *LiveWait
	Usage       []Usage
	Events      []RuntimeEvent
	Terminal    bool
	Result      *LiveResult
	FailureCode string
}
type LiveCleanup struct{ SandboxAbsent, KeyAbsent bool }
type RecoveryRecord struct {
	Wait       *LiveWait
	Binding    AttemptBinding
	Intents    []RuntimeIntentRecord
	Result     *LiveResult
	Checkpoint VerifiedCheckpoint
	Deadline   time.Time
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
	ReplacementScope *ReplacementScope
	Store            *Store
	Owner            string
	Lease            WorkerLease
	Runtime          LiveRuntime
	Access           AgentAccessClient
	Raptor           RaptorClient
	Bootstrap        VerifiedCheckpoint
}

func (w *LiveWorker) authorized(ctx context.Context) error {
	if w.Lease == nil || !w.Lease.Valid(ctx) {
		return ErrUnavailable
	}
	return nil
}

// ParseLiveQuestion preserves the existing question-only live admission.
func ParseLiveQuestion(in ExecutionInput, attempt string) (AttemptBinding, string, string, error) {
	b, operation, err := ParseLiveOperation(in, attempt)
	if err != nil || b.Operation != "application.question" || !b.valid() {
		return AttemptBinding{}, "", "", ErrInvalid
	}
	return b, operation.Question, b.DefinitionSHA256, nil
}
func (w *LiveWorker) admit(in ExecutionInput, attempt string) (AttemptBinding, string, string, error) {
	b, operation, e := ParseLiveOperation(in, attempt)
	if e != nil {
		return AttemptBinding{}, "", "", ErrInvalid
	}
	if b.Operation == "application.question" {
		return ParseLiveQuestion(in, attempt)
	}
	if w.ReplacementScope == nil || !w.ReplacementScope.ValidFor(b) {
		return AttemptBinding{}, "", "", ErrInvalid
	}
	return b, operation.Question, b.DefinitionSHA256, nil
}

func (w *LiveWorker) RunNext(ctx context.Context) error {
	if e := w.authorized(ctx); e != nil {
		return e
	}
	x, e := w.Store.ClaimLiveNext(ctx, w.Owner)
	if e != nil || x == nil {
		return e
	}
	in, e := w.Raptor.Context(ctx, x.RequestID)
	if e != nil || in.RequestID != x.RequestID {
		return w.Store.RejectLiveClaim(ctx, x.AttemptID, w.Owner, "InvalidResult")
	}
	b, question, hash, e := w.admit(in, x.AttemptID)
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

	var conversation *ConversationSession
	var initial *ConversationInput
	if b.Operation == "application.question" && w.Store.ConversationEnabled && w.Store.ConversationRuntime {
		session, err := w.Store.RequestConversationSession(ctx, b.RequestID)
		if err == nil {
			conversation = &session
			checkpoint = session.Checkpoint
		} else if err != pgx.ErrNoRows {
			return w.finish(ctx, b, RuntimeHandle{RequestID: b.RequestID}, "failed", "CheckpointFailed")
		}
		if e = w.Store.BeginConversationAttempt(ctx, b.AttemptID, w.Owner); e != nil {
			return e
		}
		if conversation != nil {
			initial, e = w.Store.PendingMessage(ctx, b.RequestID, b.AttemptID, w.Owner)
			if e != nil || initial == nil {
				return w.finish(ctx, b, RuntimeHandle{RequestID: b.RequestID}, "failed", "InvalidResult")
			}
		}
	}
	sessionCheckpoint, e := w.Store.OperationSession(ctx, b)
	if e != nil {
		return w.finish(ctx, b, RuntimeHandle{RequestID: b.RequestID}, "failed", "CheckpointFailed")
	}
	if sessionCheckpoint != nil {
		checkpoint = sessionCheckpoint.Archive
	}
	continuation, e := w.Store.LiveContinuationContext(ctx, b)
	if e != nil {
		return w.finish(ctx, b, RuntimeHandle{RequestID: b.RequestID}, "failed", "InvalidResult")
	}
	if e = w.authorized(ctx); e != nil {
		return e
	}
	if e = w.Store.RecordIntent(ctx, b.AttemptID, w.Owner, RuntimeIntent{Kind: "access", Name: b.AttemptID}); e != nil {
		return e
	}
	access, e := w.Access.Issue(ctx, b, hash, record.Deadline)
	if e != nil {
		return w.finish(ctx, b, RuntimeHandle{RequestID: b.RequestID}, "failed", "ProviderUnavailable")
	}
	if b.Operation == "db-proxy.replace-nodes" && (access.Binding != b || access.ReplacementScope == nil || w.ReplacementScope == nil || *access.ReplacementScope != *w.ReplacementScope) {
		return w.finish(ctx, b, RuntimeHandle{RequestID: b.RequestID}, "failed", "ProviderUnavailable")
	}
	if access.ExpiresAt.Before(time.Now()) || access.ExpiresAt.After(record.Deadline) {
		return w.finish(ctx, b, RuntimeHandle{RequestID: b.RequestID}, "failed", "ProviderUnavailable")
	}
	if e = w.authorized(ctx); e != nil {
		return e
	}
	h, e := w.Runtime.Start(ctx, LiveStart{Skills: in.Skills, DecisionContext: continuation, SessionCheckpoint: sessionCheckpoint, Binding: b, Question: question, Checkpoint: checkpoint, Access: access, Deadline: record.Deadline, ConversationEnabled: b.Operation == "application.question" && w.Store.ConversationEnabled && w.Store.ConversationRuntime, Conversation: conversation, InitialMessage: initial})
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
	if e = w.Store.AppendRuntimeEvents(ctx, x.AttemptID, w.Owner, o.Events, o.Turns...); e != nil {
		return e
	}
	if o.Paused != nil || o.Terminal {
		usage := o.Usage
		if o.Result != nil {
			usage = o.Result.Usage
		}
		if e = w.Store.SaveSegmentUsage(ctx, x.AttemptID, w.Owner, usage); e != nil {
			return w.finish(ctx, b, h, "failed", "InvalidResult")
		}
	}
	if o.Paused != nil {
		if cancel {
			return w.finish(ctx, b, h, "cancelled", "Cancelled")
		}
		return w.pause(ctx, b, h, *o.Paused)
	}
	if !o.Terminal {
		return w.deliverConversation(ctx, h, b)
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

// A settled Pi segment may be checkpointed immediately (within the 120-second
// maximum hold). Never release the slot until sandbox, key and access cleanup
// are confirmed. Replacement live admission is still disabled separately.
func (w *LiveWorker) pause(ctx context.Context, b AttemptBinding, h RuntimeHandle, wait LiveWait) error {
	if e := w.authorized(ctx); e != nil {
		return e
	}
	if b.Operation != "db-proxy.replace-nodes" || !wait.Valid() {
		return w.finish(ctx, b, h, "failed", "InvalidResult")
	}
	checkpoint, e := w.Runtime.Checkpoint(ctx, h)
	if e != nil {
		return w.finish(ctx, b, h, "failed", "CheckpointFailed")
	}
	cp, e := w.Store.OperationSession(ctx, b)
	if e != nil || cp == nil || cp.Archive.SHA256 != checkpoint.SHA256 || cp.Archive.ArchiveKey != checkpoint.ArchiveKey {
		return w.finish(ctx, b, h, "failed", "CheckpointFailed")
	}
	if e = w.Store.SaveLiveCheckpoint(ctx, b.AttemptID, w.Owner, checkpoint); e != nil {
		return e
	}
	if e = w.Store.SaveLiveWait(ctx, b.AttemptID, w.Owner, wait, *cp); e != nil {
		return e
	}
	cleanup, stopErr := w.Runtime.Stop(ctx, h)
	if e = w.authorized(ctx); e != nil {
		return e
	}
	revokeErr := w.Access.Revoke(ctx, b)
	if e = w.authorized(ctx); e != nil {
		return e
	}
	if stopErr != nil {
		cleanup = LiveCleanup{}
	}
	return w.Store.ReleaseLiveWait(ctx, b.AttemptID, w.Owner, cleanup, revokeErr == nil)
}
func (w *LiveWorker) finish(ctx context.Context, b AttemptBinding, h RuntimeHandle, status, code string) error {
	if e := w.authorized(ctx); e != nil {
		return e
	}
	if w.Store.ConversationEnabled {
		if e := w.Store.InterruptConversation(ctx, b.AttemptID, w.Owner); e != nil {
			return e
		}
	}
	w.Store.SetLiveStage(ctx, b.AttemptID, w.Owner, "checkpointing")
	checkpoint, checkpointErr := w.Runtime.Checkpoint(ctx, h)
	if checkpointErr != nil && status == "completed" {
		status, code = "failed", "CheckpointFailed"
	}

	if checkpointErr == nil {
		if e := w.Store.SaveLiveCheckpoint(ctx, b.AttemptID, w.Owner, checkpoint); e != nil {
			return e
		}
	}
	w.Store.SetLiveStage(ctx, b.AttemptID, w.Owner, "cleaning")
	cleanup, stopErr := w.Runtime.Stop(ctx, h)
	if e := w.authorized(ctx); e != nil {
		return e
	}
	revokeErr := w.Access.Revoke(ctx, b)
	if e := w.authorized(ctx); e != nil {
		return e
	}
	if stopErr != nil || revokeErr != nil || !cleanup.SandboxAbsent || !cleanup.KeyAbsent {
		if code == "" {
			code = "CleanupUnconfirmed"
		}
	}
	if status == "completed" && (!cleanup.SandboxAbsent || !cleanup.KeyAbsent || revokeErr != nil) {
		status = "failed"
	}
	e := w.Store.FinalizeLive(ctx, b.AttemptID, w.Owner, LiveOutcome{Status: status, FailureCode: code, Checkpoint: checkpoint, SandboxAbsent: cleanup.SandboxAbsent, KeyAbsent: cleanup.KeyAbsent, AccessRevoked: revokeErr == nil})
	if e == nil && status == "completed" && w.Store.ConversationEnabled {
		return w.Store.QueueContinuation(ctx, b.RequestID)
	}
	return e
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
	if done, e := w.Store.ReconcileUnbound(ctx, x.AttemptID, w.Owner); e != nil || done {
		return e
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
	if record.Wait != nil {
		if e = w.authorized(ctx); e != nil {
			return e
		}
		revokeErr := w.Access.Revoke(ctx, record.Binding)
		if e = w.authorized(ctx); e != nil {
			return e
		}
		return w.Store.ReleaseLiveWait(ctx, x.AttemptID, w.Owner, recovered.Cleanup, revokeErr == nil)
	}
	status, code := "failed", "Interrupted"
	if recovered.Result != nil && recovered.Checkpoint.valid() && (record.Binding.Operation != "db-proxy.replace-nodes" || (w.ReplacementScope != nil && w.ReplacementScope.ValidFor(record.Binding) && recovered.Result.Replacement != nil && recovered.Result.Replacement.Scope == *w.ReplacementScope)) {
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
	if w.Store.ConversationEnabled {
		if e = w.Store.InterruptConversation(ctx, x.AttemptID, w.Owner); e != nil {
			return e
		}
	}
	e = w.Store.FinalizeLive(ctx, x.AttemptID, w.Owner, LiveOutcome{Status: status, FailureCode: code, Checkpoint: recovered.Checkpoint, SandboxAbsent: recovered.Cleanup.SandboxAbsent, KeyAbsent: recovered.Cleanup.KeyAbsent, AccessRevoked: revokeErr == nil})
	if e == nil && status == "completed" && w.Store.ConversationEnabled {
		return w.Store.QueueContinuation(ctx, id)
	}
	return e
}
