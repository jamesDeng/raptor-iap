package execution

import (
	"context"
	"strings"
	"testing"
	"time"
)

func conversationFixture(t *testing.T) (*Store, string) {
	s := testStore(t)
	s.RuntimeMode = "live"
	s.ConversationEnabled = true
	s.ConversationRuntime = true
	id := NewID()
	if _, e := s.Receive(context.Background(), id); e != nil {
		t.Fatal(e)
	}
	return s, id
}
func conversationInput(id string, n int64) ConversationInput {
	return ConversationInput{RequestID: id, MessageID: NewID(), ActorID: NewID(), InputSequence: n, Text: "check again", AcceptedAt: time.Now().UTC()}
}
func TestConversationQueueOrderingAndGaps(t *testing.T) {
	s, id := conversationFixture(t)
	ctx := context.Background()
	two := conversationInput(id, 2)
	if _, e := s.QueueMessage(ctx, two); e != nil {
		t.Fatal(e)
	}
	var next int64
	s.Pool.QueryRow(ctx, "SELECT COALESCE(min(input_sequence),0) FROM gateway.conversation_messages WHERE request_id=$1", id).Scan(&next)
	if next != 2 {
		t.Fatal(next)
	}
	one := conversationInput(id, 1)
	if _, e := s.QueueMessage(ctx, one); e != nil {
		t.Fatal(e)
	}
	if _, e := s.QueueMessage(ctx, one); e != nil {
		t.Fatal(e)
	}
	events, e := s.Events(ctx, id, 0)
	if e != nil || len(events) != 2 {
		t.Fatal(e, events)
	}
}
func TestConversationRejectedSequenceAdvances(t *testing.T) {
	s, id := conversationFixture(t)
	ctx := context.Background()
	s.KnownSecrets = []string{"opaque-secret"}
	in := conversationInput(id, 1)
	in.Text = "echo opaque-secret"
	r, e := s.QueueMessage(ctx, in)
	if e != nil || r.Status != "rejected" {
		t.Fatal(e, r)
	}
	events, e := s.Events(ctx, id, 0)
	if e != nil || len(events) != 1 || strings.Contains(events[0].Summary, "opaque-secret") {
		t.Fatal(e)
	}
	var text string
	s.Pool.QueryRow(ctx, "SELECT text FROM gateway.conversation_messages WHERE request_id=$1", id).Scan(&text)
	if strings.Contains(text, "opaque-secret") {
		t.Fatal("secret persisted")
	}
}
func TestConversationDedupAndCapacity(t *testing.T) {
	s, id := conversationFixture(t)
	ctx := context.Background()
	in := conversationInput(id, 1)
	if _, e := s.QueueMessage(ctx, in); e != nil {
		t.Fatal(e)
	}
	changed := in
	changed.Text = "changed"
	if _, e := s.QueueMessage(ctx, changed); e == nil {
		t.Fatal("collision accepted")
	}
	changed = in
	changed.MessageID = NewID()
	if _, e := s.QueueMessage(ctx, changed); e == nil {
		t.Fatal("sequence collision accepted")
	}
	for n := int64(2); n <= 20; n++ {
		if _, e := s.QueueMessage(ctx, conversationInput(id, n)); e != nil {
			t.Fatal(e)
		}
	}
	r, e := s.QueueMessage(ctx, conversationInput(id, 21))
	if e != nil || r.Status != "rejected" {
		t.Fatal(e, r)
	}
}
func TestConversationStatusReplay(t *testing.T) {
	s, id := conversationFixture(t)
	ctx := context.Background()
	v, e := s.Conversation(ctx, id)
	if e != nil || v.Version != 1 || !v.Enabled || !v.CanContinue {
		t.Fatal(e, v)
	}
	if _, e = s.QueueMessage(ctx, conversationInput(NewID(), 1)); e == nil {
		t.Fatal("unknown Request created")
	}
	s.Pool.Exec(ctx, "UPDATE gateway.executions SET status='completed' WHERE request_id=$1", id)
	v, e = s.Conversation(ctx, id)
	if e != nil || v.CanContinue {
		t.Fatal(e, v)
	}
	s.ConversationEnabled = false
	v, _ = s.Conversation(ctx, id)
	if v.Enabled {
		t.Fatal("disabled feature enabled")
	}
}
func TestConversationPendingReceiptFences(t *testing.T) {
	s, x, b := liveAttempt(t)
	s.ConversationEnabled = true
	s.ConversationRuntime = true
	ctx := context.Background()
	one := conversationInput(b.RequestID, 1)
	two := conversationInput(b.RequestID, 2)
	if _, e := s.QueueMessage(ctx, two); e != nil {
		t.Fatal(e)
	}
	s.Pool.Exec(ctx, "UPDATE gateway.conversation_sessions SET input_open=true,attempt_id=$2 WHERE request_id=$1", b.RequestID, x.AttemptID)
	if v, e := s.PendingMessage(ctx, b.RequestID, x.AttemptID, "owner"); e != nil || v != nil {
		t.Fatal(e, v)
	}
	if _, e := s.QueueMessage(ctx, one); e != nil {
		t.Fatal(e)
	}
	v, e := s.PendingMessage(ctx, b.RequestID, x.AttemptID, "owner")
	if e != nil || v == nil || v.MessageID != one.MessageID {
		t.Fatal(e, v)
	}
	receipt := ConversationReceipt{MessageID: one.MessageID, InputSequence: 1, Status: "delivered"}
	if e = s.RecordMessageReceipt(ctx, x.AttemptID, "stale", receipt); e == nil {
		t.Fatal("stale owner accepted")
	}
	for i := 0; i < 2; i++ {
		if e = s.RecordMessageReceipt(ctx, x.AttemptID, "owner", receipt); e != nil {
			t.Fatal(e)
		}
	}
	v, e = s.PendingMessage(ctx, b.RequestID, x.AttemptID, "owner")
	if e != nil || v == nil || v.MessageID != two.MessageID {
		t.Fatal(e, v)
	}
}
func TestConversationPermanentRejectionSkipsSequence(t *testing.T) {
	s, id := conversationFixture(t)
	ctx := context.Background()
	in := conversationInput(id, 1)
	if _, e := s.RejectMessage(ctx, in); e != nil {
		t.Fatal(e)
	}
	var status string
	s.Pool.QueryRow(ctx, "SELECT status FROM gateway.conversation_messages WHERE message_id=$1", in.MessageID).Scan(&status)
	if status != "rejected" {
		t.Fatal(status)
	}
}
