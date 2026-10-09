package requests

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/domain"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

var ErrMessageCapacity = errors.New("MessageCapacity")
var messageUUID = regexp.MustCompile(`^[a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12}$`)

type ConversationCapability struct {
	Version     int    `json:"version"`
	Enabled     bool   `json:"enabled"`
	CanContinue bool   `json:"canContinue"`
	Reason      string `json:"reason"`
}
type conversationReader interface {
	Conversation(context.Context, string) (ConversationCapability, error)
}
type MessageReceipt struct {
	MessageID     string    `json:"messageId"`
	InputSequence int64     `json:"inputSequence"`
	Status        string    `json:"status"`
	AcceptedAt    time.Time `json:"acceptedAt"`
	Reason        string    `json:"reason,omitempty"`
}
type PublicMessage struct {
	MessageReceipt
	Text    string `json:"text"`
	ActorID string `json:"actorId"`
}
type MessageInput struct {
	MessageID     string    `json:"messageId"`
	RequestID     string    `json:"requestId"`
	ActorID       string    `json:"actorId"`
	InputSequence int64     `json:"inputSequence"`
	Text          string    `json:"text"`
	AcceptedAt    time.Time `json:"acceptedAt"`
}
type MessageClient interface {
	PutMessage(context.Context, string, json.RawMessage) (MessageReceipt, error)
}
type ConversationView struct {
	CanSend  bool            `json:"canSend"`
	Reason   string          `json:"reason"`
	Messages []PublicMessage `json:"messages"`
}

func conversationRequest(r domain.Request) bool {
	return r.Definition.Type == "agent" && len(r.Definition.Operations) == 1 && r.Definition.Operations[0].Name == "application.question"
}
func messageAllowedStatus(status string) bool {
	switch status {
	case "queued", "preparing", "running", "waiting_for_permission", "waiting_for_pr_review", "completed":
		return true
	}
	return false
}
func (s *Service) capability(ctx context.Context, id string) (ConversationCapability, error) {
	g, ok := s.Gateway.(conversationReader)
	if !ok {
		return ConversationCapability{}, domain.ErrUnavailable
	}
	v, e := g.Conversation(ctx, id)
	if e != nil || v.Version != 1 || !v.Enabled {
		return v, domain.ErrUnavailable
	}
	return v, nil
}
func (s *Service) SubmitMessage(ctx context.Context, actor domain.User, requestID, messageID, text string) (MessageReceipt, error) {
	var out MessageReceipt
	if !messageUUID.MatchString(requestID) || !messageUUID.MatchString(messageID) || actor.ID == "" || !utf8.ValidString(text) || strings.TrimSpace(text) == "" || utf8.RuneCountInString(text) > 2000 || len(text) > 8192 || strings.ContainsRune(text, 0) {
		return out, domain.ErrInvalid
	}
	tx, e := s.Pool.Begin(ctx)
	if e != nil {
		return out, domain.ErrUnavailable
	}
	defer tx.Rollback(ctx)
	r, e := scanRequest(tx.QueryRow(ctx, "SELECT "+requestColumns+" FROM raptor.requests WHERE id=$1 FOR UPDATE", requestID))
	if e != nil {
		return out, e
	}
	if actor.ID != r.CreatorID && actor.Role != "admin" {
		return out, domain.ErrForbidden
	}
	var oldActor, oldText string
	e = tx.QueryRow(ctx, "SELECT message_id::text,input_sequence,status,accepted_at,reason,actor_id::text,text FROM raptor.request_messages WHERE request_id=$1 AND message_id=$2", requestID, messageID).Scan(&out.MessageID, &out.InputSequence, &out.Status, &out.AcceptedAt, &out.Reason, &oldActor, &oldText)
	if e == nil {
		if oldActor != actor.ID || oldText != text {
			return MessageReceipt{}, domain.ErrConflict
		}
		return out, nil
	}
	if e != pgx.ErrNoRows {
		return out, domain.ErrUnavailable
	}
	if !s.ConversationEnabled || !conversationRequest(r) {
		return out, domain.ErrForbidden
	}
	cap, e := s.capability(ctx, requestID)
	if e != nil {
		return out, e
	}
	if !cap.CanContinue || !messageAllowedStatus(r.Status) {
		return out, domain.ErrConflict
	}
	var pending int
	var sequence int64
	e = tx.QueryRow(ctx, "SELECT count(*) FILTER(WHERE status IN ('accepted','queued','delivered')),COALESCE(max(input_sequence),0)+1 FROM raptor.request_messages WHERE request_id=$1", requestID).Scan(&pending, &sequence)
	if e != nil {
		return out, domain.ErrUnavailable
	}
	if pending >= 20 {
		return out, ErrMessageCapacity
	}
	out = MessageReceipt{MessageID: messageID, InputSequence: sequence, Status: "accepted", AcceptedAt: time.Now().UTC()}
	digest := sha256.Sum256([]byte(text))
	_, e = tx.Exec(ctx, "INSERT INTO raptor.request_messages(request_id,message_id,actor_id,input_sequence,text,text_hash,accepted_at) VALUES($1,$2,$3,$4,$5,$6,$7)", requestID, messageID, actor.ID, sequence, text, hex.EncodeToString(digest[:]), out.AcceptedAt)
	if e != nil {
		return out, domain.ErrUnavailable
	}
	payload, _ := json.Marshal(MessageInput{MessageID: messageID, RequestID: requestID, ActorID: actor.ID, InputSequence: sequence, Text: text, AcceptedAt: out.AcceptedAt})
	_, e = tx.Exec(ctx, "INSERT INTO raptor.outbox(id,topic,entity_id,payload) VALUES($1,'message',$2,$3)", domain.NewID(), requestID, payload)
	if e != nil {
		return out, domain.ErrUnavailable
	}
	if e = tx.Commit(ctx); e != nil {
		return out, domain.ErrUnavailable
	}
	return out, nil
}
func (s *Service) ListMessages(ctx context.Context, id string, after int64) ([]PublicMessage, error) {
	out := []PublicMessage{}
	if after < 0 || !messageUUID.MatchString(id) {
		return out, domain.ErrInvalid
	}
	if _, e := s.Get(ctx, id); e != nil {
		return out, e
	}
	rows, e := s.Pool.Query(ctx, "SELECT message_id::text,input_sequence,status,accepted_at,reason,text,actor_id::text FROM raptor.request_messages WHERE request_id=$1 AND input_sequence>$2 ORDER BY input_sequence LIMIT 100", id, after)
	if e != nil {
		return out, domain.ErrUnavailable
	}
	defer rows.Close()
	for rows.Next() {
		var v PublicMessage
		if e = rows.Scan(&v.MessageID, &v.InputSequence, &v.Status, &v.AcceptedAt, &v.Reason, &v.Text, &v.ActorID); e != nil {
			return out, domain.ErrUnavailable
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
func (s *Service) conversationView(ctx context.Context, actor domain.User, r domain.Request) ConversationView {
	v := ConversationView{Reason: "ConversationDisabled", Messages: []PublicMessage{}}
	if !conversationRequest(r) {
		return v
	}
	v.Messages, _ = s.ListMessages(ctx, r.ID, 0)
	if !s.ConversationEnabled {
		return v
	}
	if actor.ID != r.CreatorID && actor.Role != "admin" {
		v.Reason = "Forbidden"
		return v
	}
	cap, e := s.capability(ctx, r.ID)
	if e != nil {
		v.Reason = "ConversationUnavailable"
		return v
	}
	if !cap.CanContinue || !messageAllowedStatus(r.Status) {
		v.Reason = cap.Reason
		if v.Reason == "" {
			v.Reason = "ConversationNotReady"
		}
		return v
	}
	v.CanSend = true
	v.Reason = ""
	return v
}
func applyMessageReceipt(ctx context.Context, tx pgx.Tx, id string, receipt MessageReceipt) error {
	switch receipt.Status {
	case "queued", "delivered", "answered", "interrupted", "rejected":
	default:
		return domain.ErrInvalid
	}
	if !messageUUID.MatchString(receipt.MessageID) || receipt.InputSequence < 1 || len(receipt.Reason) > 256 {
		return domain.ErrInvalid
	}
	_, e := tx.Exec(ctx, `UPDATE raptor.request_messages SET status=$3,reason=$4 WHERE request_id=$1 AND message_id=$2 AND input_sequence=$5 AND (status='accepted' OR (status='queued' AND $3 IN ('delivered','answered','interrupted','rejected')) OR (status='delivered' AND $3 IN ('answered','interrupted')) OR status=$3)`, id, receipt.MessageID, receipt.Status, receipt.Reason, receipt.InputSequence)
	return e
}
