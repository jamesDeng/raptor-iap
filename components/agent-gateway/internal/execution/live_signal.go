package execution

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"unicode/utf8"
)

// Receipt must be fetched from Raptor after wakeup. Signal payloads are never
// passed to this decision boundary as approval evidence.
type LiveApprovalDecision struct {
	RequestID  string          `json:"requestId"`
	ApprovalID string          `json:"approvalId"`
	ActionID   string          `json:"actionId"`
	Binding    json.RawMessage `json:"binding"`
	State      string          `json:"state"`
	Guidance   json.RawMessage `json:"guidance"`
}
type LiveContinuation struct {
	Next     string
	Guidance string
}

func DecisionForLiveWait(requestID, control string, w LiveWait, a LiveApprovalDecision) (LiveContinuation, error) {
	if control == "cancelled" {
		return LiveContinuation{Next: "cancel"}, nil
	}
	if control == "blocked" {
		return LiveContinuation{Next: "block"}, nil
	}
	if control != "" {
		return LiveContinuation{}, ErrInvalid
	}
	if w.Kind != "approval" || a.RequestID != requestID || a.ApprovalID != w.ApprovalID || a.ActionID != w.ActionID || len(a.Binding) > 8192 {
		return LiveContinuation{}, ErrInvalid
	}
	var binding map[string]any
	if json.Unmarshal(a.Binding, &binding) != nil || binding["requestId"] != requestID || binding["actionId"] != w.ActionID {
		return LiveContinuation{}, ErrInvalid
	}
	raw, e := json.Marshal(binding)
	if e != nil {
		return LiveContinuation{}, ErrInvalid
	}
	digest := sha256.Sum256(raw)
	if hex.EncodeToString(digest[:]) != w.BindingDigest {
		return LiveContinuation{}, ErrInvalid
	}
	switch a.State {
	case "pending":
		return LiveContinuation{Next: "waiting"}, nil
	case "approved":
		return LiveContinuation{Next: "resume"}, nil
	case "denied":
		var guidance struct {
			Decision string `json:"decision"`
			Next     string `json:"next"`
			Guidance string `json:"guidance"`
		}
		if len(a.Guidance) > 4096 || json.Unmarshal(a.Guidance, &guidance) != nil || guidance.Decision != "deny" || !utf8.ValidString(guidance.Guidance) {
			return LiveContinuation{}, ErrInvalid
		}
		switch guidance.Next {
		case "block", "cancel":
			return LiveContinuation{Next: guidance.Next, Guidance: guidance.Guidance}, nil
		case "continue":
			if strings.TrimSpace(guidance.Guidance) != "" {
				return LiveContinuation{Next: "continue", Guidance: guidance.Guidance}, nil
			}
		}
	}
	return LiveContinuation{}, ErrInvalid
}

// ResumeLive treats an event as a durable wakeup, then resolves the decision via
// a trusted Raptor reader. Missing readers fail closed; no signal grants access.
func (s *Store) ResumeLive(ctx context.Context, requestID, signalID string) (bool, error) {
	tx, e := s.beginLive(ctx)
	if e != nil {
		return false, e
	}
	defer tx.Rollback(ctx)
	var slot *string
	if e = tx.QueryRow(ctx, "SELECT request_id::text FROM gateway.runtime_slot WHERE id=1 FOR UPDATE").Scan(&slot); e != nil {
		return false, e
	}
	var mode string
	if e = tx.QueryRow(ctx, "SELECT runtime_mode FROM gateway.executions WHERE request_id=$1 FOR UPDATE", requestID).Scan(&mode); e != nil || mode != "live" {
		return false, ErrInvalid
	}
	var signalRequest, kind string
	var payload []byte
	var consumed bool
	if e = tx.QueryRow(ctx, "SELECT request_id::text,kind,payload,consumed FROM gateway.signals WHERE id=$1 FOR UPDATE", signalID).Scan(&signalRequest, &kind, &payload, &consumed); e != nil || signalRequest != requestID {
		return false, ErrInvalid
	}
	if consumed {
		return false, nil
	}
	if kind != "approval" && kind != "continue" && kind != "cancel" && kind != "block" {
		return false, ErrInvalid
	}
	var attempt, phase string
	var raw []byte
	if e = tx.QueryRow(ctx, `SELECT w.attempt_id::text,w.phase,w.wait FROM gateway.live_waits w JOIN gateway.attempts a ON a.id=w.attempt_id WHERE w.request_id=$1 ORDER BY a.started_at DESC LIMIT 1 FOR UPDATE OF w`, requestID).Scan(&attempt, &phase, &raw); e != nil {
		return false, e
	}
	var wait LiveWait
	if json.Unmarshal(raw, &wait) != nil {
		return false, ErrInvalid
	}
	var hint struct {
		ApprovalID string `json:"approvalId"`
	}
	if json.Unmarshal(payload, &hint) != nil || (kind != "cancel" && kind != "block" && hint.ApprovalID != wait.ApprovalID) {
		return false, ErrInvalid
	}
	if phase == "resumed" {
		if _, e = tx.Exec(ctx, "UPDATE gateway.signals SET consumed=true WHERE id=$1", signalID); e != nil {
			return false, e
		}
		return false, tx.Commit(ctx)
	}
	if phase == "checkpointed" {
		return false, nil
	}
	if phase != "released" {
		return false, ErrInvalid
	}
	if s.ResolveLiveDecision == nil {
		return false, ErrUnavailable
	}
	decision, e := s.ResolveLiveDecision(ctx, requestID, wait)
	if e != nil {
		return false, e
	}
	if decision.Next == "waiting" {
		return false, nil
	}
	status := ""
	switch decision.Next {
	case "resume", "continue":
		status = "queued"
	case "cancel":
		status = "cancelled"
	case "block":
		status = "blocked"
	default:
		return false, ErrInvalid
	}
	contextRaw, _ := json.Marshal(map[string]string{"approvalId": wait.ApprovalID, "actionId": wait.ActionID, "bindingDigest": wait.BindingDigest, "decision": decision.Next, "guidance": decision.Guidance})
	if containsSensitive(string(contextRaw), s.KnownSecrets) {
		return false, ErrInvalid
	}
	if _, e = tx.Exec(ctx, "UPDATE gateway.executions SET status=$2,stage='waiting_resume',resume_context=$3,updated_at=now() WHERE request_id=$1", requestID, status, contextRaw); e != nil {
		return false, e
	}
	if _, e = tx.Exec(ctx, "UPDATE gateway.live_waits SET phase='resumed' WHERE attempt_id=$1", attempt); e != nil {
		return false, e
	}
	if _, e = tx.Exec(ctx, "UPDATE gateway.signals SET consumed=true WHERE id=$1", signalID); e != nil {
		return false, e
	}
	if e = tx.Commit(ctx); e != nil {
		return false, e
	}
	return status == "queued", nil
}
