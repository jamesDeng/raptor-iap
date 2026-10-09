package runtime

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/jamesDeng/raptor-iap/components/agent-gateway/internal/execution"
	"io"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

func conversationEnvelope(in execution.ConversationInput, b execution.AttemptBinding) ([]byte, error) {
	if in.RequestID != b.RequestID || in.InputSequence < 1 || in.MessageID == "" || !utf8.ValidString(in.Text) || strings.TrimSpace(in.Text) == "" || utf8.RuneCountInString(in.Text) > 2000 || len(in.Text) > 8192 {
		return nil, ErrConfiguration
	}
	raw, e := json.Marshal(struct {
		execution.ConversationInput
		AttemptID string `json:"attemptId"`
	}{in, b.AttemptID})
	if e != nil || len(raw) > 16384 {
		return nil, ErrConfiguration
	}
	return raw, nil
}
func (n *NativeLive) DeliverMessage(ctx context.Context, h execution.RuntimeHandle, in execution.ConversationInput) error {
	r := n.runs[h.RequestID]
	if r == nil || r.sandbox.ID != h.ID {
		return ErrRuntimeUnavailable
	}
	if e := n.fence(ctx, r.start.Binding.AttemptID); e != nil {
		return e
	}
	raw, e := conversationEnvelope(in, r.start.Binding)
	if e != nil {
		return e
	}
	return r.transport.WriteFile(ctx, r.sandbox, "/tmp/raptor-private/conversation-inbox.json", raw)
}
func (a *AXLive) DeliverMessage(ctx context.Context, h execution.RuntimeHandle, in execution.ConversationInput) error {
	r := a.axRuns[h.RequestID]
	if r == nil || h.ID != "raptor-"+r.start.Binding.AttemptID {
		return ErrRuntimeUnavailable
	}
	raw, e := conversationEnvelope(in, r.start.Binding)
	if e != nil {
		return e
	}
	return a.write(ctx, r.start, "/tmp/raptor-private/conversation-inbox.json", raw)
}
func (r *Providers) DeliverMessage(ctx context.Context, h execution.RuntimeHandle, in execution.ConversationInput) error {
	b, e := r.backend(ctx, h)
	if e != nil {
		return e
	}
	c, ok := b.(execution.ConversationRuntime)
	if !ok {
		return ErrConfiguration
	}
	return c.DeliverMessage(ctx, h, in)
}
func observeConversation(ctx context.Context, events []execution.RuntimeEvent, b execution.AttemptBinding, read func(context.Context, string, int64) ([]byte, error), secrets []string) ([]execution.ConversationTurn, error) {
	out := []execution.ConversationTurn{}
	for _, event := range events {
		if event.Kind != "turn" {
			continue
		}
		if len(event.TurnID) != 36 || filepath.Base(event.TurnID) != event.TurnID {
			return nil, ErrConfiguration
		}
		raw, e := read(ctx, "/tmp/raptor-private/conversation-turn-"+event.TurnID+".json", 65536)
		if e != nil || len(raw) > 65536 {
			return nil, ErrRuntimeUnavailable
		}
		var turn execution.ConversationTurn
		d := json.NewDecoder(bytes.NewReader(raw))
		d.DisallowUnknownFields()
		var extra any
		if d.Decode(&turn) != nil || d.Decode(&extra) != io.EOF || turn.TurnID != event.TurnID || execution.ValidateLiveResult(turn.Result, b, secrets...) != nil {
			return nil, ErrRuntimeUnavailable
		}
		out = append(out, turn)
	}
	return out, nil
}
