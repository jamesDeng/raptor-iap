package execution

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

func liveAttempt(t *testing.T) (*Store, *Execution, AttemptBinding) {
	t.Helper()
	s := testStore(t)
	ctx := context.Background()
	id := NewID()
	if _, e := s.Receive(ctx, id); e != nil {
		t.Fatal(e)
	}
	x, e := s.ClaimNext(ctx, "owner")
	if e != nil || x == nil {
		t.Fatal(e)
	}
	b := AttemptBinding{RequestID: id, AttemptID: x.AttemptID, Operation: "application.question", ObjectKind: "application", ObjectCode: "app", EnvCode: "rdev.ali", SkillsCommit: "0123456789012345678901234567890123456789", Model: "gpt-5.6-luna"}
	if e = s.BindLive(ctx, x.AttemptID, "owner", b, "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"); e != nil {
		t.Fatal(e)
	}
	return s, x, b
}
func TestLiveJournalRequiresIntentAndFencesStaleOwner(t *testing.T) {
	s, x, _ := liveAttempt(t)
	ctx := context.Background()
	if e := s.RecordResource(ctx, x.AttemptID, "owner", RuntimeResource{Kind: "sandbox", ID: "sb"}); e == nil {
		t.Fatal("resource accepted without intent")
	}
	intent := RuntimeIntent{Kind: "sandbox", Name: x.AttemptID}
	if e := s.RecordIntent(ctx, x.AttemptID, "stale", intent); e == nil {
		t.Fatal("stale owner wrote intent")
	}
	if e := s.RecordIntent(ctx, x.AttemptID, "owner", intent); e != nil {
		t.Fatal(e)
	}
	if e := s.RecordResource(ctx, x.AttemptID, "owner", RuntimeResource{Kind: "sandbox", ID: "sb"}); e != nil {
		t.Fatal(e)
	}
	if e := s.RecordResource(ctx, x.AttemptID, "owner", RuntimeResource{Kind: "sandbox", ID: "different"}); e == nil {
		t.Fatal("resource overwritten")
	}
	if _, e := s.Pool.Exec(ctx, "UPDATE gateway.runtime_slot SET owner='new-owner'"); e != nil {
		t.Fatal(e)
	}
	if e := s.RecordIntent(ctx, x.AttemptID, "owner", RuntimeIntent{Kind: "command", Name: "generation"}); e == nil {
		t.Fatal("lost owner wrote intent")
	}
}
func TestLiveBindingIsImmutable(t *testing.T) {
	s, x, b := liveAttempt(t)
	ctx := context.Background()
	hash := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	if e := s.BindLive(ctx, x.AttemptID, "owner", b, hash); e != nil {
		t.Fatal(e)
	}
	b.EnvCode = "other"
	if e := s.BindLive(ctx, x.AttemptID, "owner", b, hash); e == nil {
		t.Fatal("binding changed")
	}
	b.EnvCode = "rdev.ali"
	if e := s.BindLive(ctx, x.AttemptID, "owner", b, "0000000000000000000000000000000000000000000000000000000000000000"); e == nil {
		t.Fatal("context changed")
	}
}
func TestLiveProgressDeduplicationAndRestartHistory(t *testing.T) {
	s, x, _ := liveAttempt(t)
	ctx := context.Background()
	events := []RuntimeEvent{{RuntimeSequence: 1, Kind: "tool_start", Tool: "mcp__infra__deployments_list", Outcome: "started", OccurredAt: time.Now().UTC()}}
	for i := 0; i < 2; i++ {
		if e := s.AppendRuntimeEvents(ctx, x.AttemptID, "owner", events); e != nil {
			t.Fatal(e)
		}
	}
	all, e := (&Store{Pool: s.Pool}).Events(ctx, x.RequestID, 0)
	if e != nil || len(all) != 1 || all[0].Sequence != 1 || all[0].AttemptID != x.AttemptID || all[0].EvidenceMode != "live" {
		t.Fatalf("history: %+v %v", all, e)
	}
	events[0].Outcome = "failed"
	if e := s.AppendRuntimeEvents(ctx, x.AttemptID, "owner", events); e == nil {
		t.Fatal("conflicting duplicate accepted")
	}
	if e := s.AppendRuntimeEvents(ctx, x.AttemptID, "stale", events); e == nil {
		t.Fatal("stale owner appended")
	}
}
func validLiveResult(b AttemptBinding) LiveResult {
	now := time.Now().UTC()
	return LiveResult{RequestID: b.RequestID, AttemptID: b.AttemptID, SelectedModel: b.Model, ActualModel: b.Model, Answer: "Application is ready at this observation.", GeneratedAt: now, Evidence: []ToolEvidence{
		{Server: "raptor", Tool: "request_get", ObservedAt: now, EvidenceMode: "catalog", Identity: EvidenceIdentity{RequestID: b.RequestID, ObjectCode: b.ObjectCode, EnvCode: b.EnvCode}, State: json.RawMessage(`{"available":true}`)},
		{Server: "infra", Tool: "cloud_identity_get", ObservedAt: now, EvidenceMode: "live", Identity: EvidenceIdentity{EnvCode: b.EnvCode}, State: json.RawMessage(`{"accountMatches":true}`)},
		{Server: "infra", Tool: "deployments_list", ObservedAt: now, EvidenceMode: "live", Identity: EvidenceIdentity{ObjectCode: b.ObjectCode, EnvCode: b.EnvCode, ClusterID: "cluster", Namespace: "ns", Name: "gateway", UID: "uid"}, State: json.RawMessage(`{"state":"1/1 ready"}`)},
		{Server: "infra", Tool: "deployment_status_get", ObservedAt: now, EvidenceMode: "live", Identity: EvidenceIdentity{ObjectCode: b.ObjectCode, EnvCode: b.EnvCode, ClusterID: "cluster", Namespace: "ns", Name: "gateway", UID: "uid"}, State: json.RawMessage(`{"readyReplicas":1}`)}}}
}
func verifiedCheckpoint() VerifiedCheckpoint {
	return VerifiedCheckpoint{ArchiveKey: "auth/lifecycle/generation.tgz", ChecksumKey: "auth/lifecycle/generation.sha256", SHA256: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef", Bytes: 100, PiVersion: "0.99.2", Encryption: "AES256", VerifiedAt: time.Now().UTC()}
}
func TestLiveSuccessRequiresResultCheckpointAndCleanup(t *testing.T) {
	s, x, b := liveAttempt(t)
	ctx := context.Background()
	r := validLiveResult(b)
	if e := s.SaveLiveResult(ctx, x.AttemptID, "owner", r); e != nil {
		t.Fatal(e)
	}
	for _, o := range []LiveOutcome{{Status: "completed"}, {Status: "completed", Checkpoint: verifiedCheckpoint()}, {Status: "completed", Checkpoint: verifiedCheckpoint(), SandboxAbsent: true}, {Status: "completed", Checkpoint: verifiedCheckpoint(), SandboxAbsent: true, KeyAbsent: true}} {
		if e := s.FinalizeLive(ctx, x.AttemptID, "owner", o); e == nil {
			t.Fatal("incomplete success released slot")
		}
	}
	if e := s.FinalizeLive(ctx, x.AttemptID, "owner", LiveOutcome{Status: "completed", Checkpoint: verifiedCheckpoint(), SandboxAbsent: true, KeyAbsent: true, AccessRevoked: true}); e != nil {
		t.Fatal(e)
	}
	got, e := s.Get(ctx, x.RequestID)
	if e != nil || got.Status != "completed" || got.Result == nil || got.Result.Answer != r.Answer || got.RuntimeMode != "live" {
		t.Fatalf("result lost: %+v %v", got, e)
	}
	if e := s.RecordIntent(ctx, x.AttemptID, "owner", RuntimeIntent{Kind: "sandbox", Name: "again"}); e == nil {
		t.Fatal("ended attempt reused")
	}
}
func TestLiveInvalidEvidenceCannotPersist(t *testing.T) {
	mutations := map[string]func(*LiveResult){"empty": func(r *LiveResult) { r.Answer = "" }, "model": func(r *LiveResult) { r.ActualModel = "other" }, "missing": func(r *LiveResult) { r.Evidence = r.Evidence[:1] }, "uid": func(r *LiveResult) { r.Evidence[3].Identity.UID = "changed" }, "simulated": func(r *LiveResult) { r.Evidence[3].EvidenceMode = "simulated" }, "secret": func(r *LiveResult) { r.Answer = "Bearer sensitive" }, "time": func(r *LiveResult) { r.Evidence[3].ObservedAt = time.Time{} }}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			s, x, b := liveAttempt(t)
			r := validLiveResult(b)
			mutate(&r)
			if e := s.SaveLiveResult(context.Background(), x.AttemptID, "owner", r); e == nil {
				t.Fatal("invalid evidence persisted")
			}
		})
	}
}
func TestLiveFailureKeepsSlotUntilCleanup(t *testing.T) {
	s, x, _ := liveAttempt(t)
	ctx := context.Background()
	if e := s.FinalizeLive(ctx, x.AttemptID, "owner", LiveOutcome{Status: "failed", FailureCode: "CheckpointFailed"}); e != nil {
		t.Fatal(e)
	}
	got, e := s.Get(ctx, x.RequestID)
	if e != nil || got.Status != "blocked" || !got.RecoveryNeeded {
		t.Fatal("uncertain cleanup not blocked", e)
	}
	s.Receive(ctx, NewID())
	next, e := s.ClaimNext(ctx, "new-owner")
	if e != nil || next != nil {
		t.Fatal("replacement dispatched")
	}
	if e := s.FinalizeLive(ctx, x.AttemptID, "owner", LiveOutcome{Status: "failed", FailureCode: "CheckpointFailed", SandboxAbsent: true, KeyAbsent: true, AccessRevoked: true}); e != nil {
		t.Fatal(e)
	}
	got, e = s.Get(ctx, x.RequestID)
	if e != nil || got.Status != "failed" || got.RecoveryNeeded {
		t.Fatal("confirmed failure not finalized", e)
	}
}

func TestLiveCannotCompleteThroughLegacyRelease(t *testing.T) {
	s, x, _ := liveAttempt(t)
	if e := s.Release(context.Background(), x.RequestID, "owner", "completed", CheckpointRef{Path: "fixture"}); e == nil {
		t.Fatal("legacy path bypassed live completion gates")
	}
}
func TestLiveCannotCompleteWithoutAnswer(t *testing.T) {
	s, x, _ := liveAttempt(t)
	if e := s.FinalizeLive(context.Background(), x.AttemptID, "owner", LiveOutcome{Status: "completed", Checkpoint: verifiedCheckpoint(), SandboxAbsent: true, KeyAbsent: true, AccessRevoked: true}); e == nil {
		t.Fatal("empty completion accepted")
	}
}
func TestLiveRejectsContinuationAndMutationSignals(t *testing.T) {
	s, x, _ := liveAttempt(t)
	for _, kind := range []string{"continue", "approval", "review", "merged", "skills", "pause", "interrupt"} {
		if e := s.DeliverSignal(context.Background(), Signal{ID: kind, RequestID: x.RequestID, Kind: kind, Payload: json.RawMessage(`{}`)}); e == nil {
			t.Fatal("unsupported live signal accepted", kind)
		}
	}
}
func TestLiveDriverLeaseLostConnectionCannotReacquire(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	lease, e := s.AcquireLiveWorker(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer lease.Close()
	if !lease.Valid(ctx) {
		t.Fatal("valid lease rejected")
	}
	other, e := s.AcquireLiveWorker(ctx)
	if e == nil {
		other.Close()
		t.Fatal("second driver accepted")
	}
	lease.conn.Conn().Close(ctx)
	if lease.Valid(ctx) {
		t.Fatal("lost session lock accepted")
	}
}
func TestLiveTerminalHarnessFailureCodesFinalize(t *testing.T) {
	for _, code := range []string{"McpUnavailable", "ToolFailed", "TurnLimit", "MissingEvidence", "UnexpectedToolCatalog", "InvalidProgress", "InvalidEvidence", "RunnerFailed"} {
		t.Run(code, func(t *testing.T) {
			s, _, b := liveAttempt(t)
			if e := s.FinalizeLive(context.Background(), b.AttemptID, "owner", LiveOutcome{Status: "failed", FailureCode: code, SandboxAbsent: true, KeyAbsent: true, AccessRevoked: true}); e != nil {
				t.Fatal(e)
			}
		})
	}
}
func TestCompatibilityClaimNeverConsumesUnrelatedRequest(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	first, selected := NewID(), NewID()
	s.Receive(ctx, first)
	s.Receive(ctx, selected)
	if _, e := s.ClaimForRequest(ctx, "compat", selected); e != ErrInvalid {
		t.Fatal(e)
	}
	x, e := s.Get(ctx, first)
	if e != nil || x.Status != "queued" || x.AttemptID != "" {
		t.Fatal("unrelated queue mutated", x, e)
	}
	id, _, e := s.ActiveOwner(ctx)
	if e != nil || id != "" {
		t.Fatal("slot changed", id, e)
	}
}
