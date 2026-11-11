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
	"reflect"
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
	active   func(context.Context) bool
}
type ObservationProxy struct {
	ReadPgcatRequestJournal func(context.Context, execution.AttemptBinding, execution.ReplacementScope) (execution.PgcatRequestJournal, error)
	startedAt               time.Time
	ReadPgcatPreDrainHold   func(context.Context, execution.AttemptBinding, execution.ReplacementScope) (json.RawMessage, error)
	ReadPgcatRestartAction  func(context.Context, string, string, string, string, string, string) (execution.PgcatRestartAction, error)
	RecordPgcatObservation  func(context.Context, execution.PgcatObservationRecord) error
	ReadPgcatDrainAction    func(context.Context, string, string, string, string, string) (execution.PgcatDrainAction, error)
	origin                  string
	execute                 ObservationExecutor
	mu                      sync.Mutex
	entries                 map[[32]byte]observationEntry
	busy                    chan struct{}
	pgcat                   *pgcatContinuity
}

func NewObservationProxy(origin string, execute ObservationExecutor) (*ObservationProxy, error) {
	if origin != "https://api.rdev.raptor-iap.top"+observationRoute || execute == nil {
		return nil, ErrConfiguration
	}
	return &ObservationProxy{origin: origin, startedAt: time.Now(), execute: execute, entries: map[[32]byte]observationEntry{}, busy: make(chan struct{}, 4), pgcat: newPgcatContinuity()}, nil
}
func (p *ObservationProxy) Register(in execution.LiveStart, m ObservationMaterial, check func(context.Context) error, lifecycle ...func(context.Context) bool) (ObservationMaterial, error) {
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
	// Snapshot the issued scope; later mutation cannot change registered authority.
	scope := *in.Access.ReplacementScope
	in.Access.ReplacementScope = &scope
	entry := observationEntry{start: in, material: m, expires: expires, check: check}
	if len(lifecycle) == 1 {
		entry.active = lifecycle[0]
	}
	p.entries[sha256.Sum256([]byte(token))] = entry
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
	b := entry.start.Binding
	if !ok || input.RequestID != b.RequestID || input.AttemptID != b.AttemptID || input.Hash != b.DefinitionSHA256 {
		proxyReply(w, 401, map[string]string{"error": "Unauthorized"})
		return
	}
	raw, status := p.guardedExecute(r.Context(), hash, entry, input.Tool, input.Arguments)
	if status != 200 {
		category := "InvalidObservation"
		if status == 401 {
			category = "Unauthorized"
		}
		if status == 429 {
			category = "Busy"
		}
		proxyReply(w, status, map[string]string{"error": category})
		return
	}
	proxyReply(w, 200, map[string]any{"data": json.RawMessage(raw)})
}

// qualificationPreflight uses only the registered private entry. Its output is
// deliberately discarded; it is feasibility evidence, never the model baseline.
func (p *ObservationProxy) registeredEntry(in execution.LiveStart) ([32]byte, observationEntry, bool) {
	p.mu.Lock()
	var hash [32]byte
	var entry observationEntry
	matches := 0
	for k, candidate := range p.entries {
		if candidate.start.Binding == in.Binding && reflect.DeepEqual(candidate.start.Access, in.Access) {
			hash, entry = k, candidate
			matches++
		}
	}
	p.mu.Unlock()
	return hash, entry, matches == 1
}

func (p *ObservationProxy) qualificationPreflight(ctx context.Context, in execution.LiveStart) error {
	if p == nil {
		return ErrConfiguration
	}
	hash, entry, ok := p.registeredEntry(in)
	if !ok {
		return ErrConfiguration
	}
	_, status := p.guardedExecute(ctx, hash, entry, "metrics_read", json.RawMessage(`{"kind":"pgcatClients"}`))
	if status != 200 {
		return ErrRuntimeUnavailable
	}
	return nil
}

// Both entry points share the timeout, busy gate, lease/cancellation and result
// guards. The HTTP caller retains its own authentication and input validation.
func (p *ObservationProxy) guardedExecute(parent context.Context, hash [32]byte, entry observationEntry, tool string, args json.RawMessage) (json.RawMessage, int) {
	deadline := time.Now().Add(20 * time.Second)
	if entry.expires.Before(deadline) {
		deadline = entry.expires
	}
	ctx, cancel := context.WithDeadline(parent, deadline)
	defer cancel()
	active := func() bool { p.mu.Lock(); defer p.mu.Unlock(); _, ok := p.entries[hash]; return ok }
	if ctx.Err() != nil || !active() || !entry.expires.After(time.Now()) || entry.check(ctx) != nil || ctx.Err() != nil || !active() {
		return nil, 401
	}
	select {
	case p.busy <- struct{}{}:
		defer func() { <-p.busy }()
	default:
		return nil, 429
	}
	var raw json.RawMessage
	var err error
	if tool == pgcatContinuityTool {
		raw, err = p.pgcatObservation(ctx, entry, args)
	} else if tool == "db_connection_probe" && entry.start.Binding.ApprovalProtocol == "request-grants-v1" &&
		p.pgcat.probesReleased(entry.start.Binding.RequestID) {
		return nil, 503
	} else {
		raw, err = p.execute(ctx, entry.start, entry.material, tool, args)
	}
	unsafe := false
	for _, secret := range entry.material.Secrets {
		if secret != "" && bytes.Contains(raw, []byte(secret)) {
			unsafe = true
		}
	}
	if err != nil || ctx.Err() != nil || !active() || !entry.expires.After(time.Now()) || entry.check(ctx) != nil || ctx.Err() != nil || !active() || len(raw) > 262144 || !json.Valid(raw) || unsafe {
		return nil, 503
	}
	if entry.start.Binding.ApprovalProtocol == "request-grants-v1" &&
		p.recordPgcatObservation(ctx, entry, tool, args, raw) != nil {
		return nil, 503
	}
	return raw, 200
}

func validObservationInput(tool string, raw json.RawMessage) bool {
	var args map[string]json.RawMessage
	if json.Unmarshal(raw, &args) != nil || args == nil {
		return false
	}
	if tool == pgcatContinuityTool {
		return validPgcatContinuityArgs(raw)
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
			return value == "fleet" || value == "backend" || value == "capacity" || value == "expansion" || value == "retirement"
		}
		return value == "trafficFailures" || value == "connections" || value == "pgcatClients"
	case "db_connection_probe":
		return json.Unmarshal(args["instanceId"], &value) == nil && transportID.MatchString(value)
	default:
		return false
	}
}
