package runtime

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/netip"
	"strconv"
	"strings"
	"time"
)

type pgcatQueryCount struct {
	Queries      int64 `json:"queries"`
	Transactions int64 `json:"transactions"`
}

type pgcatTrafficSample struct {
	action string
	at     time.Time
	counts map[string]pgcatQueryCount
}

type pgcatTrafficEvidence struct {
	RestartActionID string                     `json:"restartActionId"`
	ApplicationUID  string                     `json:"applicationUid"`
	Generation      int64                      `json:"generation"`
	ObservedAt      time.Time                  `json:"observedAt"`
	NoErrors        bool                       `json:"noErrors"`
	Nodes           map[string]pgcatQueryCount `json:"nodes"`
}

func readPgcatApplicationCounter(ctx context.Context, addr netip.Addr) (pgcatQueryCount, error) {
	if !addr.Is4() || !addr.IsPrivate() {
		return pgcatQueryCount{}, ErrConfiguration
	}
	transport := &http.Transport{DialContext: (&net.Dialer{Timeout: 3 * time.Second}).DialContext, ResponseHeaderTimeout: 3 * time.Second}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 5 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+net.JoinHostPort(addr.String(), "9930")+"/metrics", nil)
	if err != nil {
		return pgcatQueryCount{}, ErrConfiguration
	}
	resp, err := client.Do(req)
	if err != nil {
		return pgcatQueryCount{}, ErrRuntimeUnavailable
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return pgcatQueryCount{}, ErrRuntimeUnavailable
	}
	return parsePgcatApplicationCounter(resp.Body)
}

func parsePgcatApplicationCounter(r io.Reader) (pgcatQueryCount, error) {
	input := &io.LimitedReader{R: r, N: 262145}
	scan := bufio.NewScanner(input)
	values := map[string]int64{}
	for scan.Scan() {
		line := scan.Text()
		metric, tail, ok := strings.Cut(line, "{")
		if !ok || (metric != "pgcat_stats_total_query_count" && metric != "pgcat_stats_total_xact_count") {
			continue
		}
		labels, value, ok := strings.Cut(tail, "}")
		if !ok {
			return pgcatQueryCount{}, ErrRuntimeUnavailable
		}
		pool, user := false, false
		for _, label := range strings.Split(labels, ",") {
			switch strings.TrimSpace(label) {
			case `pool="test"`:
				pool = true
			case `user="poc_app"`:
				user = true
			}
		}
		if !pool || !user {
			continue
		}
		number, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
		if _, exists := values[metric]; exists || err != nil || number < 0 {
			return pgcatQueryCount{}, ErrRuntimeUnavailable
		}
		values[metric] = number
	}
	if scan.Err() != nil || input.N == 0 || len(values) != 2 {
		return pgcatQueryCount{}, ErrRuntimeUnavailable
	}
	return pgcatQueryCount{Queries: values["pgcat_stats_total_query_count"], Transactions: values["pgcat_stats_total_xact_count"]}, nil
}

func (p *ObservationProxy) pgcatTraffic(ctx context.Context, entry observationEntry, args pgcatContinuityArgs, routed map[string]netip.Addr) (json.RawMessage, error) {
	b, scope := entry.start.Binding, entry.start.Access.ReplacementScope
	pair := p.pgcat.get(b.RequestID)
	if pair == nil || p.ReadPgcatRestartAction == nil || len(routed) != 2 {
		return nil, ErrRuntimeUnavailable
	}
	pair.mu.Lock()
	defer pair.mu.Unlock()
	if pair.id != args.ObservationID || pair.scope != scope.ResolutionDigest || pair.releasedAt.IsZero() || pair.logs == nil || pair.lastDrain == nil {
		return nil, ErrRuntimeUnavailable
	}
	for id := range routed {
		if pair.lastDrain.FreshConnections[id] <= 0 {
			return nil, ErrRuntimeUnavailable
		}
	}
	restart, err := p.ReadPgcatRestartAction(ctx, b.RequestID, b.EnvCode, scope.Application.AppCode, scope.Application.UID, scope.ResolutionDigest, args.RestartActionID)
	if err != nil {
		return nil, err
	}
	after := restart.RecordedAt
	if pair.releasedAt.After(after) {
		after = pair.releasedAt
	}
	body, err := p.execute(ctx, entry.start, entry.material, "deployment_read", json.RawMessage(`{}`))
	var deployment struct {
		UID                                                                                         string
		ObservedAt                                                                                  time.Time
		Generation, ObservedGeneration, Replicas, ReadyReplicas, UpdatedReplicas, AvailableReplicas int64
	}
	if err != nil || json.Unmarshal(body, &deployment) != nil || deployment.UID != scope.Application.UID ||
		deployment.Generation != restart.Generation || deployment.ObservedGeneration != restart.Generation ||
		deployment.Replicas != 2 || deployment.ReadyReplicas != 2 || deployment.UpdatedReplicas != 2 || deployment.AvailableReplicas != 2 ||
		deployment.ObservedAt.Before(after) || !pgcatFreshTime(deployment.ObservedAt, time.Now()) {
		return json.RawMessage(`{"readiness":"traffic-warming"}`), nil
	}
	for _, old := range pair.logs.streams {
		if !old.check(true, false, restart.ClaimedAt) {
			return nil, ErrRuntimeUnavailable
		}
	}
	pods, err := p.pgcatPods(ctx, entry)
	if err != nil {
		return nil, err
	}
	for _, pod := range pods {
		if pair.logs.streams[pod.UID] != nil {
			return nil, ErrRuntimeUnavailable
		}
		stream, err := pgcatPodLog(ctx, entry.material, pod, pair.logs.started, false)
		if err != nil {
			return nil, err
		}
		state := newPgcatLogState(pod.UID)
		bounded := &io.LimitedReader{R: stream, N: 16 * 1024 * 1024}
		state.readLogs(bounded, pair.logs.started, false)
		_ = stream.Close()
		if bounded.N == 0 || !state.check(false, true, after) {
			return nil, ErrRuntimeUnavailable
		}
	}
	counts := map[string]pgcatQueryCount{}
	for id, ip := range routed {
		current, err := readPgcatApplicationCounter(ctx, ip)
		if err != nil {
			return nil, err
		}
		counts[id] = current
	}
	now := time.Now().UTC()
	slot := &pair.traffic
	if args.Phase == "verify-final-traffic" {
		slot = &pair.finalTraffic
	}
	if *slot == nil {
		*slot = &pgcatTrafficSample{action: args.RestartActionID, at: now, counts: counts}
		return json.RawMessage(`{"readiness":"traffic-warming"}`), nil
	}
	base := *slot
	if base.action != args.RestartActionID || !base.at.After(after) {
		return nil, ErrRuntimeUnavailable
	}
	out := pgcatTrafficEvidence{RestartActionID: args.RestartActionID, ApplicationUID: scope.Application.UID,
		Generation: restart.Generation, ObservedAt: now, NoErrors: true, Nodes: map[string]pgcatQueryCount{}}
	for id, current := range counts {
		before, ok := base.counts[id]
		if !ok || current.Queries < before.Queries || current.Transactions < before.Transactions {
			return nil, ErrRuntimeUnavailable
		}
		if current.Queries == before.Queries || current.Transactions == before.Transactions {
			return json.RawMessage(`{"readiness":"traffic-warming"}`), nil
		}
		out.Nodes[id] = pgcatQueryCount{Queries: current.Queries - before.Queries, Transactions: current.Transactions - before.Transactions}
	}
	return json.Marshal(out)
}
