package runtime

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/jamesDeng/raptor-iap/components/agent-gateway/internal/execution"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

const observationRoute = "/v1/gateway-observations"

type ObservationExecutor func(context.Context, execution.LiveStart, ObservationMaterial, string, json.RawMessage) (json.RawMessage, error)
type observationEntry struct {
	start    execution.LiveStart
	material ObservationMaterial
	expires  time.Time
	check    func(context.Context) error
}
type ObservationProxy struct {
	origin  string
	execute ObservationExecutor
	mu      sync.Mutex
	entries map[[32]byte]observationEntry
	busy    chan struct{}
}

func NewObservationProxy(origin string, execute ObservationExecutor) (*ObservationProxy, error) {
	if origin != "https://api.rdev.raptor-iap.top"+observationRoute || execute == nil {
		return nil, ErrConfiguration
	}
	return &ObservationProxy{origin: origin, execute: execute, entries: map[[32]byte]observationEntry{}, busy: make(chan struct{}, 1)}, nil
}
func (p *ObservationProxy) Register(in execution.LiveStart, m ObservationMaterial, check func(context.Context) error) (ObservationMaterial, error) {
	if in.Binding.Operation != "db-proxy.replace-nodes" || in.Access.Binding != in.Binding || in.Access.ReplacementScope == nil || !in.Access.ReplacementScope.ResourceValidFor(in.Binding) || check == nil {
		return ObservationMaterial{}, ErrConfiguration
	}
	expires := in.Deadline
	if in.Access.ExpiresAt.Before(expires) {
		expires = in.Access.ExpiresAt
	}
	if !expires.After(time.Now()) || expires.After(time.Now().Add(900*time.Second)) {
		return ObservationMaterial{}, ErrConfiguration
	}
	// Private source credentials can expire before the attempt deadline.
	var source struct {
		ExpiresAt time.Time `json:"expiresAt"`
	}
	if json.Unmarshal(m.Config, &source) == nil && !source.ExpiresAt.IsZero() && source.ExpiresAt.Before(expires) {
		expires = source.ExpiresAt
	}
	if !expires.After(time.Now()) {
		return ObservationMaterial{}, ErrConfiguration
	}
	tokenBytes := make([]byte, 32)
	if _, e := rand.Read(tokenBytes); e != nil {
		return ObservationMaterial{}, ErrRuntimeUnavailable
	}
	token := hex.EncodeToString(tokenBytes)
	raw, e := json.Marshal(map[string]any{"version": 1, "transport": "gateway", "url": p.origin, "token": token, "binding": in.Binding, "scope": in.Access.ReplacementScope, "expiresAt": expires})
	if e != nil {
		return ObservationMaterial{}, ErrConfiguration
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	for k, entry := range p.entries {
		if !entry.expires.After(time.Now()) {
			delete(p.entries, k)
		}
	}
	if len(p.entries) > 0 {
		return ObservationMaterial{}, ErrConfiguration
	}
	m.Secrets = append(append([]string{}, m.Secrets...), gatewayObservationSecrets(in.Access.Credential)...)
	p.entries[sha256.Sum256([]byte(token))] = observationEntry{start: in, material: m, expires: expires, check: check}
	return ObservationMaterial{Config: raw, Secrets: append(append([]string{}, m.Secrets...), token)}, nil
}
func (p *ObservationProxy) Remove(attempt string) {
	if p == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	for k, e := range p.entries {
		if e.start.Binding.AttemptID == attempt {
			delete(p.entries, k)
		}
	}
}
func proxyReply(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}
func (p *ObservationProxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" || r.URL.Path != observationRoute || r.URL.RawQuery != "" {
		proxyReply(w, 404, map[string]string{"error": "Unavailable"})
		return
	}
	if !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") {
		proxyReply(w, 401, map[string]string{"error": "Unauthorized"})
		return
	}
	token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	if len(token) != 64 {
		proxyReply(w, 401, map[string]string{"error": "Unauthorized"})
		return
	}
	var input struct {
		RequestID string          `json:"requestId"`
		AttemptID string          `json:"attemptId"`
		Hash      string          `json:"definitionSha256"`
		Tool      string          `json:"tool"`
		Arguments json.RawMessage `json:"arguments"`
	}
	d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8192))
	d.DisallowUnknownFields()
	var extra any
	if d.Decode(&input) != nil || d.Decode(&extra) != io.EOF || !validObservationInput(input.Tool, input.Arguments) {
		proxyReply(w, 400, map[string]string{"error": "InvalidInput"})
		return
	}
	hash := sha256.Sum256([]byte(token))
	p.mu.Lock()
	entry, ok := p.entries[hash]
	p.mu.Unlock()
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	b := entry.start.Binding
	if !ok || !entry.expires.After(time.Now()) || input.RequestID != b.RequestID || input.AttemptID != b.AttemptID || input.Hash != b.DefinitionSHA256 || entry.check(ctx) != nil {
		proxyReply(w, 401, map[string]string{"error": "Unauthorized"})
		return
	}
	select {
	case p.busy <- struct{}{}:
		defer func() { <-p.busy }()
	default:
		proxyReply(w, 429, map[string]string{"error": "Busy"})
		return
	}
	raw, e := p.execute(ctx, entry.start, entry.material, input.Tool, input.Arguments)
	// Recheck after the read: cancellation/cleanup cannot export a late result.
	p.mu.Lock()
	_, active := p.entries[hash]
	p.mu.Unlock()
	unsafe := false
	for _, secret := range entry.material.Secrets {
		if secret != "" && bytes.Contains(raw, []byte(secret)) {
			unsafe = true
		}
	}
	if e != nil || ctx.Err() != nil || !active || !entry.expires.After(time.Now()) || entry.check(ctx) != nil || len(raw) > 262144 || !json.Valid(raw) || unsafe {
		proxyReply(w, 503, map[string]string{"error": "InvalidObservation"})
		return
	}
	proxyReply(w, 200, map[string]any{"data": json.RawMessage(raw)})
}
func validObservationInput(tool string, raw json.RawMessage) bool {
	var args map[string]json.RawMessage
	if json.Unmarshal(raw, &args) != nil || args == nil {
		return false
	}
	if tool == "deployment_read" {
		return len(args) == 0
	}
	if len(args) != 1 {
		return false
	}
	var value string
	switch tool {
	case "cloud_read", "metrics_read":
		if json.Unmarshal(args["kind"], &value) != nil {
			return false
		}
		if tool == "cloud_read" {
			return value == "fleet" || value == "backend" || value == "capacity"
		}
		return value == "trafficFailures" || value == "connections" || value == "pgcatClients"
	case "db_connection_probe":
		return json.Unmarshal(args["instanceId"], &value) == nil && transportID.MatchString(value)
	default:
		return false
	}
}
