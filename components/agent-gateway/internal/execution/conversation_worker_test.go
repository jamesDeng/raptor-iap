package execution

import (
	"context"
	"testing"
	"time"
)

func TestConversationSendAtSettleBoundary(t *testing.T) {
	s, x, b := liveAttempt(t)
	s.ConversationEnabled = true
	s.ConversationRuntime = true
	ctx := context.Background()
	if e := s.BeginConversationAttempt(ctx, x.AttemptID, "owner"); e != nil {
		t.Fatal(e)
	}
	one := conversationInput(b.RequestID, 1)
	s.QueueMessage(ctx, one)
	v, e := s.PendingMessage(ctx, b.RequestID, x.AttemptID, "owner")
	if e != nil || v == nil {
		t.Fatal(e)
	}
	s.RecordMessageReceipt(ctx, x.AttemptID, "owner", ConversationReceipt{MessageID: one.MessageID, InputSequence: 1, Status: "delivered"})
	if e = s.CloseConversationInput(ctx, x.AttemptID, "owner", 1); e != nil {
		t.Fatal(e)
	}
	two := conversationInput(b.RequestID, 2)
	if _, e = s.QueueMessage(ctx, two); e != nil {
		t.Fatal(e)
	}
	v, e = s.PendingMessage(ctx, b.RequestID, x.AttemptID, "owner")
	if e != nil || v != nil {
		t.Fatal("settled attempt got late input", e)
	}
}
func TestConversationFreshAttemptAndExactSession(t *testing.T) {
	s, x, b := liveAttempt(t)
	s.ConversationEnabled = true
	s.ConversationRuntime = true
	ctx := context.Background()
	s.BeginConversationAttempt(ctx, x.AttemptID, "owner")
	sessionID := NewID()
	e := s.AppendRuntimeEvents(ctx, x.AttemptID, "owner", []RuntimeEvent{{RuntimeSequence: 1, Kind: "session", Outcome: "confirmed", SessionID: sessionID, SessionFile: "fixture.jsonl", OccurredAt: time.Now()}})
	if e != nil {
		t.Fatal(e)
	}
	if e = s.SaveLiveResult(ctx, x.AttemptID, "owner", validLiveResult(b)); e != nil {
		t.Fatal(e)
	}
	cp := verifiedCheckpoint()
	if e = s.FinalizeLive(ctx, x.AttemptID, "owner", LiveOutcome{Status: "completed", Checkpoint: cp, SandboxAbsent: true, KeyAbsent: true, AccessRevoked: true}); e != nil {
		t.Fatal(e)
	}
	session, e := s.RequestConversationSession(ctx, b.RequestID)
	if e != nil || session.SessionID != sessionID || session.Checkpoint != cp {
		t.Fatal(e, session)
	}
	v, e := s.QueueMessage(ctx, conversationInput(b.RequestID, 1))
	if e != nil || v.Status != "queued" {
		t.Fatal(e, v)
	}
	next, e := s.ClaimNext(ctx, "owner")
	if e != nil || next == nil || next.AttemptID == x.AttemptID {
		t.Fatal(e)
	}
	if _, e = s.RequestConversationSession(ctx, NewID()); e == nil {
		t.Fatal("foreign Request resumed")
	}
}
func TestConversationInterruptedReceiptNoReplay(t *testing.T) {
	s, x, b := liveAttempt(t)
	s.ConversationEnabled = true
	s.ConversationRuntime = true
	ctx := context.Background()
	s.BeginConversationAttempt(ctx, x.AttemptID, "owner")
	msg := conversationInput(b.RequestID, 1)
	s.QueueMessage(ctx, msg)
	s.PendingMessage(ctx, b.RequestID, x.AttemptID, "owner")
	s.RecordMessageReceipt(ctx, x.AttemptID, "owner", ConversationReceipt{MessageID: msg.MessageID, InputSequence: 1, Status: "delivered"})
	if e := s.InterruptConversation(ctx, x.AttemptID, "owner"); e != nil {
		t.Fatal(e)
	}
	var status string
	s.Pool.QueryRow(ctx, "SELECT status FROM gateway.conversation_messages WHERE message_id=$1", msg.MessageID).Scan(&status)
	if status != "interrupted" {
		t.Fatal(status)
	}
}

func TestConversationAmbiguousInputNoReplay(t *testing.T) {
	s, x, b := liveAttempt(t)
	s.ConversationEnabled = true
	s.ConversationRuntime = true
	ctx := context.Background()
	if e := s.BeginConversationAttempt(ctx, x.AttemptID, "owner"); e != nil {
		t.Fatal(e)
	}
	msg := conversationInput(b.RequestID, 1)
	if _, e := s.QueueMessage(ctx, msg); e != nil {
		t.Fatal(e)
	}
	if _, e := s.PendingMessage(ctx, b.RequestID, x.AttemptID, "owner"); e != nil {
		t.Fatal(e)
	}
	if e := s.InterruptConversation(ctx, x.AttemptID, "owner"); e != nil {
		t.Fatal(e)
	}
	var status string
	s.Pool.QueryRow(ctx, "SELECT status FROM gateway.conversation_messages WHERE message_id=$1", msg.MessageID).Scan(&status)
	if status != "interrupted" {
		t.Fatalf("ambiguous model input would replay: %s", status)
	}
}
func TestConversationCleanupBlocksContinuation(t *testing.T) {
	s, x, b := liveAttempt(t)
	s.ConversationEnabled = true
	s.ConversationRuntime = true
	ctx := context.Background()
	s.BeginConversationAttempt(ctx, x.AttemptID, "owner")
	s.AppendRuntimeEvents(ctx, x.AttemptID, "owner", []RuntimeEvent{{RuntimeSequence: 1, Kind: "session", Outcome: "confirmed", SessionID: NewID(), SessionFile: "fixture.jsonl", OccurredAt: time.Now()}})
	s.SaveLiveResult(ctx, x.AttemptID, "owner", validLiveResult(b))
	if e := s.FinalizeLive(ctx, x.AttemptID, "owner", LiveOutcome{Status: "completed", Checkpoint: verifiedCheckpoint(), SandboxAbsent: true, KeyAbsent: true, AccessRevoked: true}); e != nil {
		t.Fatal(e)
	}
	s.Pool.Exec(ctx, "UPDATE gateway.executions SET cleanup='pending' WHERE request_id=$1", b.RequestID)
	s.QueueContinuation(ctx, b.RequestID)
	v, e := s.Conversation(ctx, b.RequestID)
	if e != nil || v.CanContinue {
		t.Fatal("cleanup-uncertain continuation", e, v)
	}
}
func TestConversationSettledUnconsumedInputContinues(t *testing.T) {
	s, x, b := liveAttempt(t)
	s.ConversationEnabled = true
	s.ConversationRuntime = true
	ctx := context.Background()
	s.BeginConversationAttempt(ctx, x.AttemptID, "owner")
	msg := conversationInput(b.RequestID, 1)
	s.QueueMessage(ctx, msg)
	s.PendingMessage(ctx, b.RequestID, x.AttemptID, "owner")
	if e := s.CloseConversationInput(ctx, x.AttemptID, "owner", 0); e != nil {
		t.Fatal(e)
	}
	if e := s.InterruptConversation(ctx, x.AttemptID, "owner"); e != nil {
		t.Fatal(e)
	}
	var status, bound string
	s.Pool.QueryRow(ctx, "SELECT status,COALESCE(attempt_id::text,'') FROM gateway.conversation_messages WHERE message_id=$1", msg.MessageID).Scan(&status, &bound)
	if status != "queued" || bound != "" {
		t.Fatalf("known unsent input lost: %s/%s", status, bound)
	}
}
func TestConversationCompletedGapDefersContinuation(t *testing.T) {
	s, x, b := liveAttempt(t)
	s.ConversationEnabled = true
	s.ConversationRuntime = true
	ctx := context.Background()
	s.BeginConversationAttempt(ctx, x.AttemptID, "owner")
	s.AppendRuntimeEvents(ctx, x.AttemptID, "owner", []RuntimeEvent{{RuntimeSequence: 1, Kind: "session", Outcome: "confirmed", SessionID: NewID(), SessionFile: "fixture.jsonl", OccurredAt: time.Now()}})
	s.SaveLiveResult(ctx, x.AttemptID, "owner", validLiveResult(b))
	if e := s.FinalizeLive(ctx, x.AttemptID, "owner", LiveOutcome{Status: "completed", Checkpoint: verifiedCheckpoint(), SandboxAbsent: true, KeyAbsent: true, AccessRevoked: true}); e != nil {
		t.Fatal(e)
	}
	if _, e := s.QueueMessage(ctx, conversationInput(b.RequestID, 2)); e != nil {
		t.Fatal(e)
	}
	v, e := s.Get(ctx, b.RequestID)
	if e != nil || v.Status != "completed" {
		t.Fatal("gap was scheduled", v.Status, e)
	}
	if _, e = s.QueueMessage(ctx, conversationInput(b.RequestID, 1)); e != nil {
		t.Fatal(e)
	}
	v, e = s.Get(ctx, b.RequestID)
	if e != nil || v.Status != "queued" {
		t.Fatal("contiguous message not scheduled", v.Status, e)
	}
}
