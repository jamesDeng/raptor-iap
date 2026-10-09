package execution

import (
	"context"
	"encoding/json"
	"github.com/jackc/pgx/v5"
	"path/filepath"
	"strings"
)

func (s *Store) BeginConversationAttempt(ctx context.Context, attempt, owner string) error {
	tx, id, e := s.liveTx(ctx, attempt, owner)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	_, e = tx.Exec(ctx, "INSERT INTO gateway.conversation_sessions(request_id,attempt_id,input_open) VALUES($1,$2,true) ON CONFLICT(request_id) DO UPDATE SET attempt_id=$2,input_open=true", id, attempt)
	if e != nil {
		return e
	}
	return tx.Commit(ctx)
}
func (s *Store) CloseConversationInput(ctx context.Context, attempt, owner string, last int64) error {
	tx, id, e := s.liveTx(ctx, attempt, owner)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	if e = closeConversationInput(ctx, tx, id, attempt, last); e != nil {
		return e
	}
	return tx.Commit(ctx)
}
func closeConversationInput(ctx context.Context, tx pgx.Tx, id, attempt string, last int64) error {
	var highest int64
	if e := tx.QueryRow(ctx, "SELECT COALESCE(max(input_sequence),0) FROM gateway.conversation_messages WHERE request_id=$1 AND status IN ('delivered','answered','interrupted')", id).Scan(&highest); e != nil {
		return e
	}
	if highest != last {
		return ErrInvalid
	}
	_, e := tx.Exec(ctx, "UPDATE gateway.conversation_sessions SET input_open=false WHERE request_id=$1 AND attempt_id=$2", id, attempt)
	return e
}
func (s *Store) RequestConversationSession(ctx context.Context, id string) (ConversationSession, error) {
	v := ConversationSession{RequestID: id}
	var raw []byte
	e := s.Pool.QueryRow(ctx, "SELECT session_id,session_file,checkpoint FROM gateway.conversation_sessions WHERE request_id=$1 AND session_id<>'' AND checkpoint IS NOT NULL", id).Scan(&v.SessionID, &v.SessionFile, &raw)
	if e != nil {
		return v, e
	}
	if json.Unmarshal(raw, &v.Checkpoint) != nil || !v.Checkpoint.valid() || !conversationUUID.MatchString(v.SessionID) || !validSessionFile(v.SessionFile) {
		return v, ErrInvalid
	}
	return v, nil
}
func validSessionFile(file string) bool {
	return file != "" && len(file) <= 256 && filepath.Base(file) == file && !strings.ContainsAny(file, "/\\") && strings.HasSuffix(file, ".jsonl")
}
func recordConversationSession(ctx context.Context, tx pgx.Tx, id, attempt string, v RuntimeEvent) error {
	if !conversationUUID.MatchString(v.SessionID) || !validSessionFile(v.SessionFile) {
		return ErrInvalid
	}
	tag, e := tx.Exec(ctx, `INSERT INTO gateway.conversation_sessions(request_id,session_id,session_file,attempt_id,input_open) VALUES($1,$2,$3,$4,true) ON CONFLICT(request_id) DO UPDATE SET session_id=$2,session_file=$3,attempt_id=$4 WHERE gateway.conversation_sessions.session_id='' OR (gateway.conversation_sessions.session_id=$2 AND gateway.conversation_sessions.session_file=$3)`, id, v.SessionID, v.SessionFile, attempt)
	if e != nil {
		return e
	}
	if tag.RowsAffected() != 1 {
		return ErrInvalid
	}
	return nil
}
func recordConversationTurn(ctx context.Context, tx pgx.Tx, id, attempt string, b AttemptBinding, turn ConversationTurn, secrets []string) error {
	if !conversationUUID.MatchString(turn.TurnID) || len(turn.MessageIDs) > 20 || ValidateLiveResult(turn.Result, b, secrets...) != nil {
		return ErrInvalid
	}
	raw, _ := json.Marshal(turn)
	var previous []byte
	e := tx.QueryRow(ctx, "SELECT payload FROM gateway.conversation_turns WHERE request_id=$1 AND turn_id=$2", id, turn.TurnID).Scan(&previous)
	if e == nil {
		var old ConversationTurn
		if json.Unmarshal(previous, &old) != nil {
			return ErrInvalid
		}
		canonical, _ := json.Marshal(old)
		if string(canonical) != string(raw) {
			return ErrInvalid
		}
		return nil
	}
	if e != pgx.ErrNoRows {
		return e
	}
	seen := map[string]bool{}
	for _, msg := range turn.MessageIDs {
		if !conversationUUID.MatchString(msg) || seen[msg] {
			return ErrInvalid
		}
		seen[msg] = true
		var seq int64
		var status, bound string
		e = tx.QueryRow(ctx, "SELECT input_sequence,status,COALESCE(attempt_id::text,'') FROM gateway.conversation_messages WHERE request_id=$1 AND message_id=$2", id, msg).Scan(&seq, &status, &bound)
		if e != nil || status != "delivered" || bound != attempt {
			return ErrInvalid
		}
		if e = recordConversationReceipt(ctx, tx, id, attempt, ConversationReceipt{MessageID: msg, InputSequence: seq, Status: "answered"}); e != nil {
			return e
		}
	}
	if _, e = tx.Exec(ctx, "INSERT INTO gateway.conversation_turns(request_id,turn_id,attempt_id,payload) VALUES($1,$2,$3,$4)", id, turn.TurnID, attempt, raw); e != nil {
		return e
	}
	var n int64
	if e = tx.QueryRow(ctx, "UPDATE gateway.executions SET next_sequence=next_sequence+1 WHERE request_id=$1 RETURNING next_sequence", id).Scan(&n); e != nil {
		return e
	}
	details, _ := json.Marshal(map[string]any{"role": "assistant", "status": "answered", "messageId": turn.TurnID, "turnId": turn.TurnID, "messageIds": turn.MessageIDs})
	event := ProgressEvent{EventID: "turn:" + turn.TurnID, RequestID: id, AttemptID: attempt, Sequence: n, Kind: "message", Summary: turn.Result.Answer, Details: details, EvidenceMode: "live", OccurredAt: turn.Result.GeneratedAt}
	payload, _ := json.Marshal(event)
	_, e = tx.Exec(ctx, "INSERT INTO gateway.events(request_id,sequence,event_id,payload) VALUES($1,$2,$3,$4)", id, n, event.EventID, payload)
	return e
}
func (s *Store) QueueContinuation(ctx context.Context, id string) error {
	_, e := s.Pool.Exec(ctx, `UPDATE gateway.executions SET status='queued',queued_at=now(),updated_at=now() WHERE request_id=$1 AND status='completed' AND cleanup='confirmed' AND checkpoint_status='verified' AND NOT recovery_needed AND EXISTS(SELECT 1 FROM gateway.conversation_messages WHERE request_id=$1 AND status='queued') AND EXISTS(SELECT 1 FROM gateway.conversation_sessions WHERE request_id=$1 AND session_id<>'' AND checkpoint IS NOT NULL)`, id)
	return e
}
func (s *Store) InterruptConversation(ctx context.Context, attempt, owner string) error {
	tx, id, e := s.liveTx(ctx, attempt, owner)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	rows, e := tx.Query(ctx, "SELECT message_id::text,input_sequence FROM gateway.conversation_messages WHERE request_id=$1 AND attempt_id=$2 AND status IN ('queued','delivered')", id, attempt)
	if e != nil {
		return e
	}
	receipts := []ConversationReceipt{}
	for rows.Next() {
		var r ConversationReceipt
		r.Status = "interrupted"
		if e = rows.Scan(&r.MessageID, &r.InputSequence); e != nil {
			rows.Close()
			return e
		}
		receipts = append(receipts, r)
	}
	rows.Close()
	for _, r := range receipts {
		if e = recordConversationReceipt(ctx, tx, id, attempt, r); e != nil {
			return e
		}
	}
	if _, e = tx.Exec(ctx, "UPDATE gateway.conversation_sessions SET input_open=false WHERE request_id=$1 AND attempt_id=$2", id, attempt); e != nil {
		return e
	}
	return tx.Commit(ctx)
}
func (w *LiveWorker) deliverConversation(ctx context.Context, h RuntimeHandle, b AttemptBinding) error {
	if !w.Store.ConversationEnabled || !w.Store.ConversationRuntime {
		return nil
	}
	r, ok := w.Runtime.(ConversationRuntime)
	if !ok {
		return ErrUnavailable
	}
	in, e := w.Store.PendingMessage(ctx, b.RequestID, b.AttemptID, w.Owner)
	if e != nil || in == nil {
		return e
	}
	if e = w.authorized(ctx); e != nil {
		return e
	}
	return r.DeliverMessage(ctx, h, *in)
}
