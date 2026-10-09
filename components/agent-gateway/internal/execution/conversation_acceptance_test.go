package execution

import (
	"bytes"
	"context"
	"encoding/json"
	"os/exec"
	"path/filepath"
	"testing"
)

// Uses the production worker/journal and the real pinned Pi SDK. Checkpoint/sandbox
// I/O remains a local fixture; deployed ingress/restore acceptance is separate.
type piConversationFixture struct {
	t           *testing.T
	root        string
	starts      []LiveStart
	observation LiveObservation
}

func (r *piConversationFixture) Start(ctx context.Context, in LiveStart) (RuntimeHandle, error) {
	r.starts = append(r.starts, in)
	conversation := map[string]any{}
	if in.Conversation != nil {
		conversation["requestId"] = in.Binding.RequestID
		conversation["sessionId"] = in.Conversation.SessionID
		conversation["sessionFile"] = in.Conversation.SessionFile
		conversation["initialMessage"] = in.InitialMessage
	}
	payload, _ := json.Marshal(map[string]any{"binding": in.Binding, "question": in.Question, "stateRoot": filepath.Join(r.root, "state"), "privateRoot": filepath.Join(r.root, in.Binding.AttemptID), "conversation": conversation})
	cmd := exec.CommandContext(ctx, "node", "../../../agent-harness/conversation-acceptance-fixture.mjs")
	cmd.Stdin = bytes.NewReader(payload)
	raw, e := cmd.Output()
	if e != nil {
		if x, ok := e.(*exec.ExitError); ok {
			r.t.Fatalf("Pi fixture failed: %s", x.Stderr)
		}
		return RuntimeHandle{}, e
	}
	var response struct {
		Out struct {
			Passed bool       `json:"passed"`
			Error  string     `json:"error"`
			Result LiveResult `json:"result"`
		}
		Events []RuntimeEvent     `json:"events"`
		Turns  []ConversationTurn `json:"turns"`
	}
	if e = json.Unmarshal(raw, &response); e != nil {
		return RuntimeHandle{}, e
	}
	if !response.Out.Passed {
		r.t.Fatal(response.Out.Error)
	}
	r.observation = LiveObservation{Terminal: true, Result: &response.Out.Result, Events: response.Events, Turns: response.Turns}
	return RuntimeHandle{ID: "local-fixture", RequestID: in.Binding.RequestID}, nil
}
func (r *piConversationFixture) Poll(context.Context, RuntimeHandle, int64) (LiveObservation, error) {
	return r.observation, nil
}
func (r *piConversationFixture) Checkpoint(context.Context, RuntimeHandle) (VerifiedCheckpoint, error) {
	return verifiedCheckpoint(), nil
}
func (r *piConversationFixture) Cancel(context.Context, RuntimeHandle) error { return nil }
func (r *piConversationFixture) Stop(context.Context, RuntimeHandle) (LiveCleanup, error) {
	return LiveCleanup{SandboxAbsent: true, KeyAbsent: true}, nil
}
func (r *piConversationFixture) Reconcile(context.Context, RecoveryRecord) (RecoveredRun, error) {
	return RecoveredRun{}, ErrUnavailable
}
func (r *piConversationFixture) DeliverMessage(context.Context, RuntimeHandle, ConversationInput) error {
	return ErrUnavailable
}
func TestConversationContinuationRestoresRequestOnly(t *testing.T) {
	if _, e := exec.LookPath("node"); e != nil {
		t.Skip("node unavailable")
	}
	w, x, _, _ := workerFixture(t)
	s := w.Store
	s.RuntimeMode = "live"
	s.ConversationEnabled = true
	s.ConversationRuntime = true
	driver := &piConversationFixture{t: t, root: t.TempDir()}
	w.Runtime = driver
	ctx := context.Background()
	if e := w.RunNext(ctx); e != nil {
		t.Fatal(e)
	}
	if e := w.RunActive(ctx); e != nil {
		t.Fatal(e)
	}
	first, e := s.RequestConversationSession(ctx, x.RequestID)
	if e != nil {
		t.Fatal(e)
	}
	msg := conversationInput(x.RequestID, 1)
	msg.Text = "/skill:literal {{unchanged}}"
	if _, e = s.QueueMessage(ctx, msg); e != nil {
		t.Fatal(e)
	}
	if e = w.RunNext(ctx); e != nil {
		t.Fatal(e)
	}
	if e = w.RunActive(ctx); e != nil {
		t.Fatal(e)
	}
	second, e := s.RequestConversationSession(ctx, x.RequestID)
	if e != nil || first.SessionID != second.SessionID {
		t.Fatal("wrong session", e)
	}
	if len(driver.starts) != 2 || driver.starts[0].Binding.AttemptID == driver.starts[1].Binding.AttemptID || !driver.starts[1].Deadline.After(driver.starts[0].Deadline) || driver.starts[1].InitialMessage.MessageID != msg.MessageID {
		t.Fatal("not a fresh continuation")
	}
	var status string
	if e = s.Pool.QueryRow(ctx, "SELECT status FROM gateway.conversation_messages WHERE message_id=$1", msg.MessageID).Scan(&status); e != nil || status != "answered" {
		t.Fatal(status, e)
	}
	events, e := s.Events(ctx, x.RequestID, 0)
	if e != nil {
		t.Fatal(e)
	}
	answers := 0
	for _, event := range events {
		if event.Kind == "message" {
			var d struct{ Role string }
			json.Unmarshal(event.Details, &d)
			if d.Role == "assistant" {
				answers++
			}
		}
	}
	if answers != 2 {
		t.Fatalf("answers lost: %d", answers)
	}
	final, e := s.Get(ctx, x.RequestID)
	if e != nil || final.Status != "completed" || final.Cleanup != "confirmed" || final.Result == nil {
		t.Fatal(final, e)
	}
}
