package execution

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/jackc/pgx/v5"
	"time"
)

func (s *Store) Conversation(ctx context.Context, id string) (ConversationCapability, error) {
	v := ConversationCapability{Version: 1, Enabled: s.ConversationEnabled && s.ConversationRuntime, Reason: "ConversationDisabled"}
	x, e := s.Get(ctx, id)
	if e != nil {
		return v, e
	}
	if !v.Enabled || x.RuntimeMode != "live" {
		return v, nil
	}
	v.CanContinue = !x.RecoveryNeeded
	switch x.Status {
	case "queued", "preparing", "running", "waiting_for_permission", "waiting_for_pr_review":
	case "completed":
		var ready bool
		e = s.Pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM gateway.conversation_sessions WHERE request_id=$1 AND session_id<>'' AND checkpoint IS NOT NULL)`, id).Scan(&ready)
		if e != nil {
			return v, e
		}
		v.CanContinue = v.CanContinue && ready && x.CheckpointStatus == "verified" && x.Cleanup == "confirmed"
	default:
		v.CanContinue = false
	}
	v.Reason = ""
	if !v.CanContinue {
		v.Reason = "ConversationNotReady"
	}
	return v, nil
}
func messagePublicEvent(ctx context.Context, tx pgx.Tx, id, attempt, text string, r ConversationReceipt) error {
	var n int64
	if e := tx.QueryRow(ctx, "UPDATE gateway.executions SET next_sequence=next_sequence+1,updated_at=now() WHERE request_id=$1 RETURNING next_sequence", id).Scan(&n); e != nil {
		return e
	}
	detail, _ := json.Marshal(map[string]any{"messageId": r.MessageID, "inputSequence": r.InputSequence, "status": r.Status, "role": "user", "reason": r.Reason})
	v := ProgressEvent{EventID: fmt.Sprintf("message:%s:%s", r.MessageID, r.Status), RequestID: id, AttemptID: attempt, Sequence: n, Kind: "message", Summary: text, Details: detail, EvidenceMode: "live", OccurredAt: time.Now().UTC()}
	raw, _ := json.Marshal(v)
	_, e := tx.Exec(ctx, "INSERT INTO gateway.events(request_id,sequence,event_id,payload) VALUES($1,$2,$3,$4)", id, n, v.EventID, raw)
	return e
}
func (s *Store) QueueMessage(ctx context.Context, in ConversationInput) (ConversationReceipt, error) {
	var out ConversationReceipt
	if !in.valid() {
		return out, ErrInvalid
	}
	if !s.ConversationEnabled || !s.ConversationRuntime {
		return out, ErrUnavailable
	}
	tx, e := s.Pool.Begin(ctx)
	if e != nil {
		return out, e
	}
	defer tx.Rollback(ctx)
	var status, mode, attempt, cleanup, checkpoint string
	var recovery bool
	e = tx.QueryRow(ctx, "SELECT status,runtime_mode,COALESCE(attempt_id::text,''),recovery_needed,cleanup,checkpoint_status FROM gateway.executions WHERE request_id=$1 FOR UPDATE", in.RequestID).Scan(&status, &mode, &attempt, &recovery, &cleanup, &checkpoint)
	if e != nil {
		return out, ErrUnavailable
	}
	digest := sha256.Sum256([]byte(in.Text))
	hash := hex.EncodeToString(digest[:])
	var actor, oldHash string
	e = tx.QueryRow(ctx, "SELECT message_id::text,input_sequence,status,accepted_at,reason,COALESCE(attempt_id::text,''),actor_id::text,text_hash FROM gateway.conversation_messages WHERE request_id=$1 AND message_id=$2", in.RequestID, in.MessageID).Scan(&out.MessageID, &out.InputSequence, &out.Status, &out.AcceptedAt, &out.Reason, &out.AttemptID, &actor, &oldHash)
	if e == nil {
		if out.InputSequence != in.InputSequence || actor != in.ActorID || oldHash != hash {
			return out, ErrInvalid
		}
		return out, nil
	}
	if e != pgx.ErrNoRows {
		return out, e
	}
	var clash bool
	e = tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM gateway.conversation_messages WHERE request_id=$1 AND input_sequence=$2)", in.RequestID, in.InputSequence).Scan(&clash)
	if e != nil {
		return out, e
	}
	if clash {
		return out, ErrInvalid
	}
	out = ConversationReceipt{MessageID: in.MessageID, InputSequence: in.InputSequence, Status: "queued", AcceptedAt: in.AcceptedAt}
	text := in.Text
	if mode != "live" || recovery {
		out.Status = "rejected"
		out.Reason = "ConversationNotReady"
	}
	switch status {
	case "queued", "preparing", "running", "waiting_for_permission", "waiting_for_pr_review":
	case "completed":
		var ready bool
		tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM gateway.conversation_sessions WHERE request_id=$1 AND session_id<>'' AND checkpoint IS NOT NULL)", in.RequestID).Scan(&ready)
		if !ready || cleanup != "confirmed" || checkpoint != "verified" {
			out.Status = "rejected"
			out.Reason = "MissingConversationSession"
		}
	default:
		out.Status = "rejected"
		out.Reason = "ConversationNotReady"
	}
	if containsSensitive(text, s.KnownSecrets) {
		text = "[redacted sensitive content]"
		out.Status = "rejected"
		out.Reason = "SensitiveInput"
	}
	var pending int
	if e = tx.QueryRow(ctx, "SELECT count(*) FROM gateway.conversation_messages WHERE request_id=$1 AND status IN ('queued','delivered')", in.RequestID).Scan(&pending); e != nil {
		return out, e
	}
	if pending >= 20 {
		out.Status = "rejected"
		out.Reason = "MessageCapacity"
	}
	if _, e = tx.Exec(ctx, "INSERT INTO gateway.conversation_sessions(request_id) VALUES($1) ON CONFLICT DO NOTHING", in.RequestID); e != nil {
		return out, e
	}
	_, e = tx.Exec(ctx, "INSERT INTO gateway.conversation_messages(request_id,message_id,actor_id,input_sequence,text,text_hash,accepted_at,status,reason) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)", in.RequestID, in.MessageID, in.ActorID, in.InputSequence, text, hash, in.AcceptedAt, out.Status, out.Reason)
	if e != nil {
		return out, e
	}
	if e = messagePublicEvent(ctx, tx, in.RequestID, "", text, out); e != nil {
		return out, e
	}
	if status == "completed" && out.Status == "queued" {
		if _, e = tx.Exec(ctx, "UPDATE gateway.executions SET status='queued',queued_at=now() WHERE request_id=$1", in.RequestID); e != nil {
			return out, e
		}
	}
	if e = tx.Commit(ctx); e != nil {
		return out, e
	}
	return out, nil
}
func (s *Store) PendingMessage(ctx context.Context, id, attempt, owner string) (*ConversationInput, error) {
	tx, request, e := s.liveTx(ctx, attempt, owner)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback(ctx)
	if request != id {
		return nil, ErrInvalid
	}
	var last int64
	var open bool
	var sessionAttempt string
	e = tx.QueryRow(ctx, "SELECT last_input_sequence,input_open,COALESCE(attempt_id::text,'') FROM gateway.conversation_sessions WHERE request_id=$1 FOR UPDATE", id).Scan(&last, &open, &sessionAttempt)
	if e == pgx.ErrNoRows {
		return nil, nil
	}
	if e != nil {
		return nil, e
	}
	if !open || sessionAttempt != attempt {
		return nil, nil
	}
	for {
		var in ConversationInput
		var status string
		e = tx.QueryRow(ctx, "SELECT request_id::text,message_id::text,actor_id::text,input_sequence,text,accepted_at,status FROM gateway.conversation_messages WHERE request_id=$1 AND input_sequence=$2", id, last+1).Scan(&in.RequestID, &in.MessageID, &in.ActorID, &in.InputSequence, &in.Text, &in.AcceptedAt, &status)
		if e == pgx.ErrNoRows {
			return nil, tx.Commit(ctx)
		}
		if e != nil {
			return nil, e
		}
		if status == "rejected" || status == "interrupted" {
			last++
			if _, e = tx.Exec(ctx, "UPDATE gateway.conversation_sessions SET last_input_sequence=$2 WHERE request_id=$1", id, last); e != nil {
				return nil, e
			}
			continue
		}
		if status != "queued" {
			return nil, tx.Commit(ctx)
		}
		tag, err := tx.Exec(ctx, "UPDATE gateway.conversation_messages SET attempt_id=$3 WHERE request_id=$1 AND message_id=$2 AND (attempt_id IS NULL OR attempt_id=$3)", id, in.MessageID, attempt)
		if err != nil {
			return nil, err
		}
		if tag.RowsAffected() != 1 {
			return nil, ErrInvalid
		}
		return &in, tx.Commit(ctx)
	}
}
func (s *Store) RecordMessageReceipt(ctx context.Context, attempt, owner string, r ConversationReceipt) error {
	tx, id, e := s.liveTx(ctx, attempt, owner)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	if e = recordConversationReceipt(ctx, tx, id, attempt, r); e != nil {
		return e
	}
	return tx.Commit(ctx)
}
func recordConversationReceipt(ctx context.Context, tx pgx.Tx, id, attempt string, r ConversationReceipt) error {
	var old, text, bound string
	var seq int64
	e := tx.QueryRow(ctx, "SELECT status,text,input_sequence,COALESCE(attempt_id::text,'') FROM gateway.conversation_messages WHERE request_id=$1 AND message_id=$2 FOR UPDATE", id, r.MessageID).Scan(&old, &text, &seq, &bound)
	if e != nil || seq != r.InputSequence || bound != attempt {
		return ErrInvalid
	}
	if old == r.Status {
		return nil
	}
	if !(old == "queued" && (r.Status == "delivered" || r.Status == "interrupted") || old == "delivered" && (r.Status == "answered" || r.Status == "interrupted")) {
		return ErrInvalid
	}
	if _, e = tx.Exec(ctx, "UPDATE gateway.conversation_messages SET status=$3 WHERE request_id=$1 AND message_id=$2", id, r.MessageID, r.Status); e != nil {
		return e
	}
	if r.Status == "delivered" {
		tag, e := tx.Exec(ctx, "UPDATE gateway.conversation_sessions SET last_input_sequence=$2 WHERE request_id=$1 AND last_input_sequence=$2-1", id, seq)
		if e != nil {
			return e
		}
		if tag.RowsAffected() != 1 {
			return ErrInvalid
		}
	}
	return messagePublicEvent(ctx, tx, id, attempt, text, r)
}
