package runtime

import (
	"context"
	"maps"
	"net/netip"
	"sync"
	"time"

	"github.com/jamesDeng/raptor-iap/components/agent-gateway/internal/execution"
)

type pgcatSocket interface {
	backendAddress(context.Context) (netip.Addr, error)
	close()
}

type pgcatConnector func(context.Context) (pgcatSocket, error)

type pgcatSocketEvidence struct {
	ID        string    `json:"id"`
	OpenedAt  time.Time `json:"openedAt"`
	LastSQLAt time.Time `json:"lastSqlAt"`
	Rounds    int       `json:"rounds"`
}

type pgcatHoldEvidence struct {
	RequestID     string                         `json:"requestId"`
	ScopeDigest   string                         `json:"scopeDigest"`
	ObservationID string                         `json:"observationId"`
	ObservedAt    time.Time                      `json:"observedAt"`
	Sockets       map[string]pgcatSocketEvidence `json:"sockets"`
}

type pgcatDrainEvidence struct {
	pgcatHoldEvidence
	DrainActionID    string         `json:"drainActionId"`
	DrainObservedAt  time.Time      `json:"drainObservedAt"`
	FreshConnections map[string]int `json:"freshConnections"`
}

type pgcatHeldSocket struct {
	pgcatSocket
	pgcatSocketEvidence
}

type pgcatHeldPair struct {
	finalTraffic *pgcatTrafficSample
	logs         *pgcatAppLogWatch
	lastDrain    *pgcatDrainEvidence
	releasedAt   time.Time
	traffic      *pgcatTrafficSample
	mu           sync.Mutex
	id           string
	request      string
	scope        string
	original     map[string]netip.Addr
	sockets      map[string]*pgcatHeldSocket
	cancel       context.CancelFunc
	stopWatching context.CancelFunc
	drain        string
	lost         bool
}

type pgcatContinuity struct {
	mu       sync.Mutex
	requests map[string]*pgcatHeldPair
}

func newPgcatContinuity() *pgcatContinuity {
	return &pgcatContinuity{requests: make(map[string]*pgcatHeldPair)}
}

func pgcatNodeAddresses(nodes map[string]netip.Addr) (map[netip.Addr]string, bool) {
	if len(nodes) != 2 {
		return nil, false
	}
	out := make(map[netip.Addr]string, len(nodes))
	for id, addr := range nodes {
		if !transportID.MatchString(id) || !addr.Is4() || !addr.IsPrivate() || out[addr] != "" {
			return nil, false
		}
		out[addr] = id
	}
	return out, true
}

func pgcatFreshTime(value, now time.Time) bool {
	return !value.IsZero() && !value.After(now.Add(2*time.Second)) && now.Sub(value) <= 30*time.Second
}

func (r *pgcatContinuity) hold(ctx context.Context, request, scope string, original map[string]netip.Addr, seenAt time.Time, connect pgcatConnector, lifecycle ...func(context.Context) bool) (pgcatHoldEvidence, error) {
	want, valid := pgcatNodeAddresses(original)
	if request == "" || scope == "" || !valid || connect == nil || !pgcatFreshTime(seenAt, time.Now()) {
		return pgcatHoldEvidence{}, ErrConfiguration
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if prior := r.requests[request]; prior != nil {
		prior.mu.Lock()
		defer prior.mu.Unlock()
		if prior.lost || scope != prior.scope || !maps.Equal(prior.original, original) {
			return pgcatHoldEvidence{}, ErrRuntimeUnavailable
		}
		return prior.evidence(), nil
	}
	hold := &pgcatHeldPair{id: execution.NewID(), request: request, scope: scope, original: maps.Clone(original), sockets: make(map[string]*pgcatHeldSocket)}
	for range 24 {
		opened := time.Now().UTC()
		conn, err := connect(ctx)
		if err != nil {
			break
		}
		backend, err := conn.backendAddress(ctx)
		node := want[backend]
		if err != nil || node == "" || hold.sockets[node] != nil {
			conn.close()
			if err != nil || node == "" {
				break
			}
			continue
		}
		hold.sockets[node] = &pgcatHeldSocket{pgcatSocket: conn,
			pgcatSocketEvidence: pgcatSocketEvidence{ID: execution.NewID(), OpenedAt: opened, LastSQLAt: time.Now().UTC(), Rounds: 1}}
		if len(hold.sockets) == 2 {
			background, cancel := context.WithCancel(context.Background())
			hold.cancel = cancel
			r.requests[request] = hold
			evidence := hold.evidence()
			go hold.keepAlive(background)
			if len(lifecycle) == 1 && lifecycle[0] != nil {
				watch, stop := context.WithCancel(context.Background())
				hold.stopWatching = stop
				go r.watchRequest(watch, request, hold, lifecycle[0])
			}
			return evidence, nil
		}
	}
	hold.close()
	return pgcatHoldEvidence{}, ErrRuntimeUnavailable
}

func (h *pgcatHeldPair) evidence() pgcatHoldEvidence {
	evidence := pgcatHoldEvidence{RequestID: h.request, ScopeDigest: h.scope, ObservationID: h.id, ObservedAt: time.Now().UTC(), Sockets: make(map[string]pgcatSocketEvidence, len(h.sockets))}
	for id, socket := range h.sockets {
		evidence.Sockets[id] = socket.pgcatSocketEvidence
	}
	return evidence
}

func (h *pgcatHeldPair) keepAlive(ctx context.Context) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			h.mu.Lock()
			call, cancel := context.WithTimeout(ctx, 5*time.Second)
			err := h.probe(call)
			cancel()
			h.mu.Unlock()
			if err != nil {
				return
			}
		}
	}
}

func (h *pgcatHeldPair) probe(ctx context.Context) error {
	if h.lost {
		return ErrRuntimeUnavailable
	}
	for id, socket := range h.sockets {
		actual, err := socket.backendAddress(ctx)
		if err != nil || actual != h.original[id] {
			h.close()
			return ErrRuntimeUnavailable
		}
		socket.LastSQLAt = time.Now().UTC()
		socket.Rounds++
	}
	return nil
}

func (h *pgcatHeldPair) close() {
	if h.cancel != nil {
		h.cancel()
	}
	h.lost = true
	for _, socket := range h.sockets {
		socket.close()
	}
}

func (r *pgcatContinuity) verifyDrain(ctx context.Context, request, scope, observation, action string, seenAt time.Time, replacements map[string]netip.Addr, connect pgcatConnector) (pgcatDrainEvidence, error) {
	want, valid := pgcatNodeAddresses(replacements)
	if !valid || !transportID.MatchString(action) || connect == nil || !pgcatFreshTime(seenAt, time.Now()) {
		return pgcatDrainEvidence{}, ErrConfiguration
	}
	r.mu.Lock()
	hold := r.requests[request]
	r.mu.Unlock()
	if hold == nil {
		return pgcatDrainEvidence{}, ErrRuntimeUnavailable
	}
	hold.mu.Lock()
	if hold.lost || hold.id != observation || hold.scope != scope || (hold.drain != "" && hold.drain != action) {
		hold.mu.Unlock()
		return pgcatDrainEvidence{}, ErrRuntimeUnavailable
	}
	for oldID, addr := range hold.original {
		socket := hold.sockets[oldID]
		if want[addr] != "" || replacements[oldID].IsValid() || !socket.OpenedAt.Before(seenAt) {
			hold.mu.Unlock()
			return pgcatDrainEvidence{}, ErrRuntimeUnavailable
		}
	}
	hold.drain = action
	err := hold.probe(ctx)
	hold.mu.Unlock()
	if err != nil {
		return pgcatDrainEvidence{}, err
	}
	counts := make(map[string]int, 2)
	for range 24 {
		conn, err := connect(ctx)
		if err != nil {
			return pgcatDrainEvidence{}, ErrRuntimeUnavailable
		}
		backend, err := conn.backendAddress(ctx)
		conn.close()
		if err != nil || want[backend] == "" {
			return pgcatDrainEvidence{}, ErrRuntimeUnavailable
		}
		counts[want[backend]]++
	}
	if len(counts) != 2 || !pgcatFreshTime(seenAt, time.Now()) {
		return pgcatDrainEvidence{}, ErrRuntimeUnavailable
	}
	hold.mu.Lock()
	defer hold.mu.Unlock()
	if err := hold.probe(ctx); err != nil {
		return pgcatDrainEvidence{}, err
	}
	evidence := pgcatDrainEvidence{pgcatHoldEvidence: hold.evidence(), DrainActionID: action,
		DrainObservedAt: seenAt, FreshConnections: counts}
	hold.lastDrain = &evidence
	return evidence, nil
}

func (r *pgcatContinuity) release(request string) {
	r.remove(request, nil)
}

func (r *pgcatContinuity) remove(request string, expected *pgcatHeldPair) {
	r.mu.Lock()
	hold := r.requests[request]
	if hold == nil || (expected != nil && hold != expected) {
		r.mu.Unlock()
		return
	}
	delete(r.requests, request)
	r.mu.Unlock()

	hold.mu.Lock()
	defer hold.mu.Unlock()
	if hold.stopWatching != nil {
		hold.stopWatching()
	}
	if hold.logs != nil {
		hold.logs.cancel()
	}
	hold.close()
}

func (r *pgcatContinuity) watchRequest(ctx context.Context, request string, hold *pgcatHeldPair, active func(context.Context) bool) {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			call, cancel := context.WithTimeout(ctx, 5*time.Second)
			retain := active(call)
			cancel()
			if !retain && ctx.Err() == nil {
				r.remove(request, hold)
				return
			}
		}
	}
}

func (r *pgcatContinuity) matchesOriginalBackends(request string, backends map[string]netip.Addr) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	held := r.requests[request]
	if held == nil {
		return false
	}
	for id, address := range held.original {
		if backends[id] != address {
			return false
		}
	}
	return true
}

func (r *pgcatContinuity) matchesOriginalNodes(request string, ids []string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	held := r.requests[request]
	return held != nil && len(ids) == 2 && ids[0] != ids[1] && held.original[ids[0]].IsValid() && held.original[ids[1]].IsValid()
}

type pgcatProbeRelease struct {
	pgcatHoldEvidence
	DrainActionID string    `json:"drainActionId"`
	ReleasedAt    time.Time `json:"releasedAt"`
}

func (r *pgcatContinuity) get(request string) *pgcatHeldPair {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.requests[request]
}

func (r *pgcatContinuity) releaseProbes(ctx context.Context, request, scope, observation, action string) (pgcatProbeRelease, error) {
	pair := r.get(request)
	if pair == nil {
		return pgcatProbeRelease{}, ErrRuntimeUnavailable
	}
	pair.mu.Lock()
	defer pair.mu.Unlock()
	if pair.id != observation || pair.scope != scope || pair.lastDrain == nil || pair.lastDrain.DrainActionID != action ||
		pair.logs == nil {
		return pgcatProbeRelease{}, ErrRuntimeUnavailable
	}
	if pair.releasedAt.IsZero() {
		if !pgcatFreshTime(pair.lastDrain.ObservedAt, time.Now()) || pair.probe(ctx) != nil {
			return pgcatProbeRelease{}, ErrRuntimeUnavailable
		}
		pair.releasedAt = time.Now().UTC()
		pair.close()
	}
	return pgcatProbeRelease{pgcatHoldEvidence: pair.evidence(), DrainActionID: action, ReleasedAt: pair.releasedAt}, nil
}

func (r *pgcatContinuity) probesReleased(request string) bool {
	pair := r.get(request)
	if pair == nil {
		return false
	}
	pair.mu.Lock()
	defer pair.mu.Unlock()
	return !pair.releasedAt.IsZero()
}
