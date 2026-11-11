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
	finalTraffic     *pgcatTrafficSample
	logs             *pgcatAppLogWatch
	lastDrain        *pgcatDrainEvidence
	releasedAt       time.Time
	recoveredRelease *pgcatProbeRelease
	restoredDrain    bool
	retiredRestart   *pgcatRetiredRestart
	traffic          *pgcatTrafficSample
	mu               sync.Mutex
	id               string
	request          string
	scope            string
	original         map[string]netip.Addr
	sockets          map[string]*pgcatHeldSocket
	cancel           context.CancelFunc
	stopWatching     context.CancelFunc
	drain            string
	lost             bool
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

func (r *pgcatContinuity) hold(ctx context.Context, request, scope string, original map[string]netip.Addr, seenAt time.Time, connect pgcatConnector, lifecycle ...func(context.Context) bool) (pgcatHoldEvidence, error) {
	return r.capture(ctx, request, scope, execution.NewID(), original, nil, seenAt, connect, lifecycle...)
}

// recaptureBeforeDrain keeps the logical observation and its original cohort,
// but records new physical sockets. Only the caller that verified no drain was
// claimed may use this path.
func (r *pgcatContinuity) recaptureBeforeDrain(ctx context.Context, saved pgcatHoldEvidence, routed map[string]netip.Addr, seenAt time.Time, connect pgcatConnector, lifecycle ...func(context.Context) bool) (pgcatHoldEvidence, error) {
	if saved.ObservationID == "" || len(saved.Sockets) != 2 || (len(routed) != 2 && len(routed) != 4) {
		return pgcatHoldEvidence{}, ErrConfiguration
	}
	original, added := make(map[string]netip.Addr, 2), make(map[string]netip.Addr, 2)
	for id, addr := range routed {
		if _, old := saved.Sockets[id]; old {
			original[id] = addr
		} else {
			added[id] = addr
		}
	}
	if len(original) != 2 {
		return pgcatHoldEvidence{}, ErrConfiguration
	}
	skip := make(map[netip.Addr]string, 2)
	if len(added) != 0 {
		var valid bool
		skip, valid = pgcatNodeAddresses(added)
		if !valid {
			return pgcatHoldEvidence{}, ErrConfiguration
		}
	}
	for _, addr := range original {
		if skip[addr] != "" {
			return pgcatHoldEvidence{}, ErrConfiguration
		}
	}
	return r.capture(ctx, saved.RequestID, saved.ScopeDigest, saved.ObservationID, original, skip, seenAt, connect, lifecycle...)
}

func (r *pgcatContinuity) capture(ctx context.Context, request, scope, observationID string, original map[string]netip.Addr, skip map[netip.Addr]string, seenAt time.Time, connect pgcatConnector, lifecycle ...func(context.Context) bool) (pgcatHoldEvidence, error) {
	want, valid := pgcatNodeAddresses(original)
	if request == "" || scope == "" || !valid || connect == nil || seenAt.IsZero() {
		return pgcatHoldEvidence{}, ErrConfiguration
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if prior := r.requests[request]; prior != nil {
		prior.mu.Lock()
		defer prior.mu.Unlock()
		if prior.lost || scope != prior.scope || !maps.Equal(prior.original, original) || (skip != nil && prior.id != observationID) {
			return pgcatHoldEvidence{}, ErrRuntimeUnavailable
		}
		return prior.evidence(), nil
	}
	hold := &pgcatHeldPair{id: observationID, request: request, scope: scope, original: maps.Clone(original), sockets: make(map[string]*pgcatHeldSocket)}
	for range 24 {
		conn, err := connect(ctx)
		if err != nil {
			break
		}
		backend, err := conn.backendAddress(ctx)
		node := want[backend]
		if err != nil || node == "" || hold.sockets[node] != nil {
			conn.close()
			if err != nil || (node == "" && skip[backend] == "") {
				break
			}
			continue
		}
		firstSQL := time.Now().UTC()
		hold.sockets[node] = &pgcatHeldSocket{pgcatSocket: conn,
			pgcatSocketEvidence: pgcatSocketEvidence{ID: execution.NewID(), OpenedAt: firstSQL, LastSQLAt: firstSQL, Rounds: 1}}
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

func (r *pgcatContinuity) verifyDrain(ctx context.Context, request, scope, observation, action string, claimedAt, seenAt time.Time, replacements map[string]netip.Addr, connect pgcatConnector) (pgcatDrainEvidence, error) {
	want, valid := pgcatNodeAddresses(replacements)
	if !valid || !transportID.MatchString(action) || connect == nil || claimedAt.IsZero() || seenAt.Before(claimedAt) {
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
		if want[addr] != "" || replacements[oldID].IsValid() || !socket.OpenedAt.Before(claimedAt) {
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
	if len(counts) != 2 {
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
	if held == nil || len(ids) != 2 || ids[0] == ids[1] {
		return false
	}
	_, first := held.original[ids[0]]
	_, second := held.original[ids[1]]
	return first && second
}

type pgcatProbeRelease struct {
	pgcatHoldEvidence
	DrainActionID string    `json:"drainActionId"`
	ReleasedAt    time.Time `json:"releasedAt"`
	Recovery      string    `json:"recovery,omitempty"`
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
	if pair.restoredDrain {
		for _, stream := range pair.logs.streams {
			if !stream.check(false, false, pair.logs.started) {
				return pgcatProbeRelease{}, ErrRuntimeUnavailable
			}
		}
		if pair.recoveredRelease == nil {
			pair.releasedAt = time.Now().UTC()
			proof := pair.lastDrain.pgcatHoldEvidence
			proof.ObservedAt = pair.releasedAt
			pair.recoveredRelease = &pgcatProbeRelease{pgcatHoldEvidence: proof, DrainActionID: action,
				ReleasedAt: pair.releasedAt, Recovery: "previous-controller-cleanup"}
		}
		return *pair.recoveredRelease, nil
	}
	if pair.releasedAt.IsZero() {
		if pair.probe(ctx) != nil {
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

// installRecoveredDrain restores only immutable evidence. There are no
// historical socket handles in this process and no probes are re-opened.
func (r *pgcatContinuity) installRecoveredDrain(ctx context.Context, request, scope string, proof pgcatDrainEvidence, original map[string]netip.Addr, active func(context.Context) bool) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if ctx.Err() != nil || r.requests[request] != nil || active == nil {
		return ErrRuntimeUnavailable
	}
	pair := &pgcatHeldPair{id: proof.ObservationID, request: request, scope: scope, original: maps.Clone(original),
		drain: proof.DrainActionID, lastDrain: &proof, lost: true, restoredDrain: true}
	r.requests[request] = pair
	watch, stop := context.WithCancel(context.Background())
	pair.stopWatching = stop
	go r.watchRequest(watch, request, pair, active)
	return nil
}
