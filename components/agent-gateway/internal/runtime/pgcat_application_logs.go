package runtime

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/netip"
	"net/url"
	"regexp"
	"sync"
	"time"
)

type pgcatAppPod struct {
	Name string `json:"name"`
	UID  string `json:"uid"`
	IP   string `json:"ip"`
}

var pgcatPodName = regexp.MustCompile(`^[a-z0-9](?:[-a-z0-9.]{0,251}[a-z0-9])?$`)

type pgcatLogState struct {
	mu         sync.Mutex
	podUID     string
	processID  string
	lastAt     time.Time
	firstSQLAt time.Time
	sequence   uint64
	success    int
	routes     map[netip.Addr]int64
	routeFrom  time.Time
	started    bool
	final      bool
	bad        bool
	ready      chan struct{}
}

func newPgcatLogState(podUID string) *pgcatLogState {
	return &pgcatLogState{podUID: podUID, ready: make(chan struct{})}
}

func (s *pgcatLogState) readLogs(stream io.Reader, since time.Time, following bool) {
	scan := bufio.NewScanner(stream)
	scan.Buffer(make([]byte, 4096), 16384)
	for scan.Scan() {
		s.mu.Lock()
		err := s.observeLog(scan.Bytes(), since)
		if err != nil {
			s.bad = true
			s.mu.Unlock()
			return
		}
		s.mu.Unlock()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if scan.Err() != nil || (following && !s.final) {
		s.bad = true
	}
}

func (s *pgcatLogState) observeLog(line []byte, since time.Time) error {
	var event struct {
		Kind      string    `json:"kind"`
		ProcessID string    `json:"processId"`
		PodUID    string    `json:"podUid"`
		At        time.Time `json:"at"`
		Sequence  uint64    `json:"sequence"`
		Outcome   string    `json:"outcome"`
		ProxyIP   string    `json:"proxyIp"`
		Snapshot  struct {
			Final     bool `json:"final"`
			Connected int  `json:"connected"`
		} `json:"snapshot"`
	}
	if json.Unmarshal(line, &event) != nil || event.PodUID != s.podUID || !transportID.MatchString(event.ProcessID) ||
		event.At.IsZero() || (s.processID != "" && event.ProcessID != s.processID) || s.final {
		return ErrRuntimeUnavailable
	}
	if event.At.Before(since) {
		return nil
	}
	if s.processID == "" {
		s.processID = event.ProcessID
	}
	if event.At.Before(s.lastAt) {
		return ErrRuntimeUnavailable
	}
	s.lastAt = event.At
	switch event.Kind {
	case "traffic_startup":
		if s.started || s.sequence != 0 || event.Sequence != 0 {
			return ErrRuntimeUnavailable
		}
		s.started = true
		return nil
	case "traffic_final":
		if s.sequence == 0 || event.Sequence != s.sequence || !event.Snapshot.Final || event.Snapshot.Connected != 0 {
			return ErrRuntimeUnavailable
		}
		s.final = true
		return nil
	case "operation":
		if event.Outcome != "success" {
			return ErrRuntimeUnavailable
		}
		if s.routes != nil {
			addr, err := netip.ParseAddr(event.ProxyIP)
			if err != nil || !addr.Is4() || !addr.IsPrivate() || len(s.routes) > 4 {
				return ErrRuntimeUnavailable
			}
			count := s.routes[addr]
			if event.At.After(s.routeFrom) {
				count++
			}
			s.routes[addr] = count
		}
		s.success++
		if s.firstSQLAt.IsZero() {
			s.firstSQLAt = event.At
		}
	case "scheduled", "connected", "disconnected":
	case "skipped", "connection_failure":
		return ErrRuntimeUnavailable
	default:
		return ErrRuntimeUnavailable
	}
	if event.Sequence == 0 || ((s.sequence > 0 || s.started) && event.Sequence != s.sequence+1) {
		return ErrRuntimeUnavailable
	}
	s.sequence = event.Sequence
	if s.success == 1 && event.Kind == "operation" {
		close(s.ready)
	}
	return nil
}

func (s *pgcatLogState) check(requireFinal, requireStartup bool, after time.Time) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return !s.bad && s.success > 0 && s.final == requireFinal && (!requireStartup || s.started) &&
		!s.lastAt.Before(after)
}

func (s *pgcatLogState) recoveryCoverage(before, through time.Time) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.bad || s.final || s.started || s.firstSQLAt.IsZero() || !s.firstSQLAt.Before(before) {
		return false, ErrRuntimeUnavailable
	}
	return !s.lastAt.Before(through), nil
}

func (s *pgcatLogState) shutdownComplete(after time.Time) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.bad || s.success == 0 || (s.final && s.lastAt.Before(after)) {
		return false, ErrRuntimeUnavailable
	}
	return s.final, nil
}

func pgcatPodLog(ctx context.Context, material ObservationMaterial, pod pgcatAppPod, since time.Time, follow bool) (io.ReadCloser, error) {
	if !pgcatPodName.MatchString(pod.Name) || !transportID.MatchString(pod.UID) || since.IsZero() {
		return nil, ErrConfiguration
	}
	session, err := observerKubeConnection(material.Kubeconfig, 0)
	if err != nil {
		return nil, err
	}
	q := url.Values{"container": {"client"}, "sinceTime": {since.UTC().Format(time.RFC3339Nano)}}
	if follow {
		q.Set("follow", "true")
	}
	r, err := http.NewRequestWithContext(ctx, http.MethodGet, session.origin+"/api/v1/namespaces/raptor-test/pods/"+
		url.PathEscape(pod.Name)+"/log?"+q.Encode(), nil)
	if err != nil {
		return nil, ErrConfiguration
	}
	r.Header.Set("Authorization", "Bearer "+session.token)
	resp, err := session.client.Do(r)
	if err != nil {
		return nil, ErrRuntimeUnavailable
	}
	if resp.StatusCode != http.StatusOK {
		_ = resp.Body.Close()
		return nil, ErrRuntimeUnavailable
	}
	return resp.Body, nil
}

type pgcatAppLogWatch struct {
	started time.Time
	streams map[string]*pgcatLogState
	cancel  context.CancelFunc
}

func (p *ObservationProxy) pgcatPods(ctx context.Context, entry observationEntry, retiringPodUIDs []string) ([]pgcatAppPod, error) {
	args, err := json.Marshal(struct {
		RetiringPodUIDs []string `json:"retiringPodUids,omitempty"`
	}{retiringPodUIDs})
	if err != nil {
		return nil, ErrConfiguration
	}
	raw, err := p.execute(ctx, entry.start, entry.material, "pgcat_client_pods", args)
	var out struct {
		Pods []pgcatAppPod `json:"pods"`
	}
	if err != nil || json.Unmarshal(raw, &out) != nil || len(out.Pods) != 2 || out.Pods[0].UID == out.Pods[1].UID ||
		out.Pods[0].Name == out.Pods[1].Name {
		return nil, ErrRuntimeUnavailable
	}
	for _, pod := range out.Pods {
		if !pgcatPodName.MatchString(pod.Name) || !transportID.MatchString(pod.UID) {
			return nil, ErrRuntimeUnavailable
		}
	}
	return out.Pods, nil
}

func (p *ObservationProxy) startPgcatLogWatch(ctx context.Context, entry observationEntry, hold pgcatHoldEvidence) error {
	p.pgcat.mu.Lock()
	pair := p.pgcat.requests[entry.start.Binding.RequestID]
	p.pgcat.mu.Unlock()
	if pair == nil {
		return ErrRuntimeUnavailable
	}
	pair.mu.Lock()
	defer pair.mu.Unlock()
	if pair.logs != nil {
		return nil
	}
	pods, err := p.pgcatPods(ctx, entry, nil)
	if err != nil {
		return err
	}
	monitor, cancel := context.WithCancel(context.Background())
	watch := &pgcatAppLogWatch{started: hold.ObservedAt, streams: map[string]*pgcatLogState{}, cancel: cancel}
	for _, pod := range pods {
		state := newPgcatLogState(pod.UID)
		watch.streams[pod.UID] = state
		go func() {
			stream, err := pgcatPodLog(monitor, entry.material, pod, watch.started, true)
			if err != nil {
				state.mu.Lock()
				state.bad = true
				close(state.ready)
				state.mu.Unlock()
				return
			}
			defer stream.Close()
			state.readLogs(stream, watch.started, true)
		}()
	}
	for _, stream := range watch.streams {
		select {
		case <-ctx.Done():
			cancel()
			return ErrRuntimeUnavailable
		case <-stream.ready:
			if !stream.check(false, false, watch.started) {
				cancel()
				return ErrRuntimeUnavailable
			}
		}
	}
	pair.logs = watch
	return nil
}
