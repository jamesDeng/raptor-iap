package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
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
	invalid := func() (domain.PgcatSeriesObservation, error) {
		return domain.PgcatSeriesObservation{}, &domain.CommandError{Code: "MetricsInvalid"}
	}
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
	// Source freshness and all-pool/node coverage remain the consumer's checks.
	return domain.PgcatSeriesObservation{Series: series, ObservedAt: time.Now().UTC(), Rows: response.Data.Result}, nil
}
