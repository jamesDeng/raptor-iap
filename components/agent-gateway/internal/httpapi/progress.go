package httpapi

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"github.com/gorilla/websocket"
	"github.com/jamesDeng/raptor-iap/components/agent-gateway/internal/execution"
	"net/http"
	"net/url"
	"time"
)

type ProgressConfig struct {
	URL     string
	Origins []string
}
type progressServer struct {
	store  *execution.Store
	config ProgressConfig
	slots  chan struct{}
}
type progressTicket struct {
	Token     string    `json:"token"`
	URL       string    `json:"url"`
	ExpiresAt time.Time `json:"expiresAt"`
}

func newProgress(s *execution.Store, c ProgressConfig) *progressServer {
	return &progressServer{store: s, config: c, slots: make(chan struct{}, 128)}
}
func (g *progressServer) allowed(origin string) bool {
	for _, v := range g.config.Origins {
		if v == origin && v != "" {
			return true
		}
	}
	return false
}
func validProgressURL(raw string) bool {
	u, e := url.Parse(raw)
	if e != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return false
	}
	return u.Scheme == "wss" || (u.Scheme == "ws" && (u.Hostname() == "127.0.0.1" || u.Hostname() == "localhost" || u.Hostname() == "::1"))
}
func ticketHash(token string) string {
	h := sha256.Sum256([]byte(token))
	return hex.EncodeToString(h[:])
}
func (g *progressServer) issue(ctx context.Context, id, subject, origin string) (progressTicket, error) {
	if !validProgressURL(g.config.URL) || !g.allowed(origin) || subject == "" || len(subject) > 200 {
		return progressTicket{}, execution.ErrInvalid
	}
	if _, e := g.store.Get(ctx, id); e != nil {
		return progressTicket{}, e
	}
	if _, e := g.store.Pool.Exec(ctx, "DELETE FROM gateway.progress_tickets WHERE expires_at<=now()"); e != nil {
		return progressTicket{}, e
	}
	var b [32]byte
	if _, e := rand.Read(b[:]); e != nil {
		return progressTicket{}, e
	}
	v := progressTicket{base64.RawURLEncoding.EncodeToString(b[:]), g.config.URL, time.Now().UTC().Add(time.Minute)}
	_, e := g.store.Pool.Exec(ctx, "INSERT INTO gateway.progress_tickets(token_hash,request_id,subject,origin,expires_at) VALUES($1,$2,$3,$4,$5)", ticketHash(v.Token), id, subject, origin, v.ExpiresAt)
	return v, e
}
func (g *progressServer) consume(ctx context.Context, token, id, origin string) error {
	if len(token) != 43 || !g.allowed(origin) {
		return execution.ErrInvalid
	}
	tag, e := g.store.Pool.Exec(ctx, "DELETE FROM gateway.progress_tickets WHERE token_hash=$1 AND request_id=$2 AND origin=$3 AND expires_at>now()", ticketHash(token), id, origin)
	if e != nil {
		return e
	}
	if tag.RowsAffected() != 1 {
		return execution.ErrInvalid
	}
	return nil
}
func (g *progressServer) mint(w http.ResponseWriter, r *http.Request) {
	if g.config.URL == "" {
		write(w, 501, map[string]string{"error": "ProgressNotConfigured"})
		return
	}
	var in struct {
		Subject string `json:"subject"`
		Origin  string `json:"origin"`
	}
	d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 2048))
	d.DisallowUnknownFields()
	if d.Decode(&in) != nil {
		write(w, 400, map[string]string{"error": "InvalidInput"})
		return
	}
	v, e := g.issue(r.Context(), r.PathValue("id"), in.Subject, in.Origin)
	result(w, v, e)
}
func (g *progressServer) connect(w http.ResponseWriter, r *http.Request) {
	if !validProgressURL(g.config.URL) || !g.allowed(r.Header.Get("Origin")) || r.URL.RawQuery != "" {
		write(w, 403, map[string]string{"error": "Forbidden"})
		return
	}
	select {
	case g.slots <- struct{}{}:
		defer func() { <-g.slots }()
	default:
		write(w, 503, map[string]string{"error": "Unavailable"})
		return
	}
	up := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return g.allowed(r.Header.Get("Origin")) }}
	c, e := up.Upgrade(w, r, nil)
	if e != nil {
		return
	}
	defer c.Close()
	c.SetReadLimit(2048)
	c.SetReadDeadline(time.Now().Add(5 * time.Second))
	var auth struct {
		Type      string `json:"type"`
		Token     string `json:"token"`
		RequestID string `json:"requestId"`
		After     int64  `json:"afterSequence"`
	}
	if c.ReadJSON(&auth) != nil || auth.Type != "subscribe" || auth.After < 0 || g.consume(r.Context(), auth.Token, auth.RequestID, r.Header.Get("Origin")) != nil {
		return
	}
	auth.Token = ""
	ctx, cancel := context.WithTimeout(r.Context(), time.Minute)
	defer cancel()
	c.SetReadDeadline(time.Now().Add(time.Minute))
	// A dedicated reader detects navigation/disconnection and forbids subsequent commands.
	go func() {
		defer cancel()
		for {
			_, _, e := c.ReadMessage()
			if e != nil {
				return
			}
			return
		}
	}()
	send := func(v any) error { c.SetWriteDeadline(time.Now().Add(5 * time.Second)); return c.WriteJSON(v) }
	cursor := auth.After
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	lastSnapshot := ""
	for {
		for {
			events, e := g.store.Events(ctx, auth.RequestID, cursor)
			if e != nil {
				return
			}
			for _, v := range events {
				if e = send(map[string]any{"type": "event", "event": v}); e != nil {
					return
				}
				cursor = v.Sequence
			}
			if len(events) < 101 {
				break
			}
		}
		snapshot, e := g.store.Get(ctx, auth.RequestID)
		if e != nil {
			return
		}
		b, _ := json.Marshal(snapshot)
		if string(b) != lastSnapshot {
			if send(map[string]any{"type": "snapshot", "execution": publicExecution(snapshot)}) != nil {
				return
			}
			lastSnapshot = string(b)
		}
		// Final state can race event persistence; only signal completion once the durable cursor catches up.
		if terminalAndClean(snapshot) {
			events, e := g.store.Events(ctx, auth.RequestID, cursor)
			if e != nil {
				return
			}
			if len(events) == 0 {
				send(map[string]any{"type": "complete"})
				return
			}
			continue
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
func terminalAndClean(x execution.Execution) bool {
	if x.Status != "completed" && x.Status != "failed" && x.Status != "cancelled" {
		return false
	}
	var cleanup struct {
		SandboxAbsent bool `json:"sandboxAbsent"`
		KeyAbsent     bool `json:"keyAbsent"`
		AccessRevoked bool `json:"accessRevoked"`
	}
	return !x.RecoveryNeeded && json.Unmarshal(x.CleanupStatus, &cleanup) == nil && cleanup.SandboxAbsent && cleanup.KeyAbsent && cleanup.AccessRevoked
}

// Only browser-facing execution fields belong in the direct progress stream.
func publicExecution(x execution.Execution) map[string]any {
	return map[string]any{"requestId": x.RequestID, "attemptId": x.AttemptID, "status": x.Status, "stage": x.Stage, "runtimeMode": x.RuntimeMode, "result": x.Result, "checkpointStatus": x.CheckpointStatus, "cleanupStatus": x.CleanupStatus, "failureCode": x.FailureCode, "appliedSkills": x.AppliedSkills, "recoveryNeeded": x.RecoveryNeeded, "updatedAt": x.UpdatedAt}
}
