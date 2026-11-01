package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"sync"
	"time"

	"raptor-iap/infra-api/internal/domain"
)

// Only these eight named selections can reach the existing private ACK Service.
func pgcatExpression(series string, env domain.Environment, m domain.ProxyMapping) (string, error) {
	metric, timestamp := "", false
	switch series {
	case "up":
		metric = "up"
	case "upTimestamp":
		metric, timestamp = "up", true
	case "active":
		metric = "pgcat_pools_cl_active"
	case "activeTimestamp":
		metric, timestamp = "pgcat_pools_cl_active", true
	case "idle":
		metric = "pgcat_pools_cl_idle"
	case "idleTimestamp":
		metric, timestamp = "pgcat_pools_cl_idle", true
	case "waiting":
		metric = "pgcat_pools_cl_waiting"
	case "waitingTimestamp":
		metric, timestamp = "pgcat_pools_cl_waiting", true
	default:
		return "", domain.ErrScope
	}
	query := fmt.Sprintf(`%s{job="pgcat",env=%s,db_proxy_code=%s,target_db_code=%s}`, metric, strconv.Quote(env.Code), strconv.Quote(m.Code), strconv.Quote(m.TargetDBCode))
	if timestamp {
		query = "timestamp(" + query + ")"
	}
	return query, nil
}

func (p *reader) ObservePgcatSeries(ctx context.Context, env domain.Environment, target domain.ProxyReadTarget, series string) (domain.PgcatSeriesObservation, error) {
	if env.ClusterID == "" {
		return domain.PgcatSeriesObservation{}, domain.ErrScope
	}
	// Reject the finite selector before any provider or query work.
	if _, err := pgcatExpression(series, env, domain.ProxyMapping{}); err != nil {
		return domain.PgcatSeriesObservation{}, err
	}
	m, _, err := p.observationGroup(ctx, env, target)
	if err != nil {
		return domain.PgcatSeriesObservation{}, err
	}
	query, _ := pgcatExpression(series, env, m)
	raw, err := p.PrometheusQuery(ctx, env, query, time.Now().UTC().Format(time.RFC3339Nano))
	if err != nil {
		return domain.PgcatSeriesObservation{}, &domain.CommandError{Code: "MetricsUnavailable"}
	}
	rows, err := pgcatVector(raw, env, m)
	if err != nil {
		return domain.PgcatSeriesObservation{}, err
	}
	// Source freshness and all-pool/node coverage remain the consumer's checks.
	return domain.PgcatSeriesObservation{Series: series, ObservedAt: time.Now().UTC(), Rows: rows}, nil
}

func pgcatVector(raw []byte, env domain.Environment, m domain.ProxyMapping) ([]domain.PrometheusSample, error) {
	invalid := func() ([]domain.PrometheusSample, error) {
		return nil, &domain.CommandError{Code: "MetricsInvalid"}
	}
	if len(raw) > 1048576 {
		return invalid()
	}
	var response struct {
		Status   string   `json:"status"`
		Warnings []string `json:"warnings"`
		Data     struct {
			ResultType string                    `json:"resultType"`
			Result     []domain.PrometheusSample `json:"result"`
		} `json:"data"`
	}
	if json.Unmarshal(raw, &response) != nil || response.Status != "success" || len(response.Warnings) > 0 || response.Data.ResultType != "vector" || response.Data.Result == nil || len(response.Data.Result) > 100 {
		return invalid()
	}
	for _, row := range response.Data.Result {
		if row.Metric == nil || row.Metric["job"] != "pgcat" || row.Metric["env"] != env.Code || row.Metric["db_proxy_code"] != m.Code || row.Metric["target_db_code"] != m.TargetDBCode || len(row.Value) != 2 {
			return invalid()
		}
		var stamp float64
		var value string
		if json.Unmarshal(row.Value[0], &stamp) != nil || stamp <= 0 || json.Unmarshal(row.Value[1], &value) != nil {
			return invalid()
		}
		v, err := strconv.ParseFloat(value, 64)
		if err != nil || math.IsInf(v, 0) || math.IsNaN(v) || v < 0 {
			return invalid()
		}
	}
	return response.Data.Result, nil
}

func (p *reader) ObservePgcatClients(ctx context.Context, env domain.Environment, target domain.ProxyReadTarget) (domain.PgcatClientsObservation, error) {
	if env.ClusterID == "" {
		return domain.PgcatClientsObservation{}, domain.ErrScope
	}
	m, _, err := p.observationGroup(ctx, env, target)
	if err != nil {
		return domain.PgcatClientsObservation{}, err
	}
	members, err := p.proxy.observationMembers(ctx, env, m)
	if err != nil {
		return domain.PgcatClientsObservation{}, err
	}
	fleet := domain.FleetObservation{GroupID: m.GroupID, Instances: members, ObservedAt: time.Now().UTC()}
	backend, err := p.observeBackend(ctx, env, m, members)
	if err != nil {
		return domain.PgcatClientsObservation{}, err
	}
	// Initialize the checked private ACK transport once, under the existing
	// setup bound. All fixed queries retain their individual five-second bound.
	setup, cancelSetup := context.WithTimeout(ctx, 5*time.Second)
	client, err := p.prometheusClient(setup, env)
	cancelSetup()
	if err != nil {
		return domain.PgcatClientsObservation{}, err
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	evaluation := time.Now().UTC().Format(time.RFC3339Nano)
	selections := [...]string{"up", "upTimestamp", "active", "activeTimestamp", "idle", "idleTimestamp", "waiting", "waitingTimestamp"}
	var rows [8][]domain.PrometheusSample
	var mu sync.Mutex
	next := 0
	var failure error
	var workers sync.WaitGroup
	for worker := 0; worker < 2; worker++ {
		workers.Go(func() {
			for {
				mu.Lock()
				if failure != nil || ctx.Err() != nil || next == len(selections) {
					mu.Unlock()
					return
				}
				i := next
				next++
				mu.Unlock()
				query, _ := pgcatExpression(selections[i], env, m)
				raw, err := queryPrivatePrometheus(ctx, client, query, evaluation, true)
				var vector []domain.PrometheusSample
				if err == nil {
					vector, err = pgcatVector(raw, env, m)
				}
				if err == nil {
					seen := map[string]bool{}
					for _, row := range vector {
						labels, _ := json.Marshal(row.Metric)
						if seen[string(labels)] {
							err = &domain.CommandError{Code: "MetricsInvalid"}
							break
						}
						seen[string(labels)] = true
					}
				}
				if err != nil {
					mu.Lock()
					if failure == nil {
						failure = err
						cancel()
					}
					mu.Unlock()
					return
				}
				rows[i] = vector
			}
		})
	}
	workers.Wait()
	if failure != nil {
		return domain.PgcatClientsObservation{}, failure
	}
	if ctx.Err() != nil {
		return domain.PgcatClientsObservation{}, &domain.CommandError{Code: "MetricsUnavailable"}
	}
	return domain.PgcatClientsObservation{Series: "clients", ObservedAt: time.Now().UTC(), Fleet: fleet, Backend: backend, Samples: domain.PgcatClientsSamples{Up: rows[0], UpTimestamp: rows[1], Active: rows[2], ActiveTimestamp: rows[3], Idle: rows[4], IdleTimestamp: rows[5], Waiting: rows[6], WaitingTimestamp: rows[7]}}, nil
}
