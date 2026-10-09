package execution

import (
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

var conversationUUID = regexp.MustCompile(`^[a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12}$`)

type ConversationInput struct {
	RequestID     string    `json:"requestId"`
	MessageID     string    `json:"messageId"`
	ActorID       string    `json:"actorId"`
	InputSequence int64     `json:"inputSequence"`
	Text          string    `json:"text"`
	AcceptedAt    time.Time `json:"acceptedAt"`
}
type ConversationReceipt struct {
	MessageID     string    `json:"messageId"`
	InputSequence int64     `json:"inputSequence"`
	Status        string    `json:"status"`
	AttemptID     string    `json:"attemptId,omitempty"`
	Reason        string    `json:"reason,omitempty"`
	AcceptedAt    time.Time `json:"acceptedAt"`
}
type ConversationCapability struct {
	Version     int    `json:"version"`
	Enabled     bool   `json:"enabled"`
	CanContinue bool   `json:"canContinue"`
	Reason      string `json:"reason"`
}
type ConversationSession struct {
	RequestID   string             `json:"requestId"`
	SessionID   string             `json:"sessionId"`
	SessionFile string             `json:"sessionFile"`
	Checkpoint  VerifiedCheckpoint `json:"checkpoint"`
}
type ConversationTurn struct {
	TurnID     string     `json:"turnId"`
	MessageIDs []string   `json:"messageIds"`
	Result     LiveResult `json:"result"`
}

func (v ConversationInput) valid() bool {
	return conversationUUID.MatchString(v.RequestID) && conversationUUID.MatchString(v.MessageID) && conversationUUID.MatchString(v.ActorID) && v.InputSequence > 0 && !v.AcceptedAt.IsZero() && v.AcceptedAt.Before(time.Now().Add(time.Minute)) && utf8.ValidString(v.Text) && strings.TrimSpace(v.Text) != "" && !strings.ContainsRune(v.Text, 0) && utf8.RuneCountInString(v.Text) <= 2000 && len(v.Text) <= 8192
}
