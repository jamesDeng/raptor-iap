// Package metrics turns private Prometheus observations into complete gate input.
// It has no model-supplied URL, query or credentials.
package metrics

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"raptor-iap/infra-api/internal/domain"
	"raptor-iap/infra-api/internal/policy"
	"strconv"
	"strings"
	"time"
)

type Query func(context.Context, domain.Environment, string, string) ([]byte, error)
type Client struct {
	scopes map[string]domain.Environment
	query  Query
}
type sample struct {
	Metric map[string]string `json:"metric"`
	Value  []json.RawMessage `json:"value"`
}

func fail() error { return &domain.CommandError{Code: "MetricsInvalid"} }
func New(scopes map[string]domain.Environment, query Query) (*Client, error) {
	if query == nil || len(scopes) == 0 {
		return nil, fail()
	}
	copied := map[string]domain.Environment{}
	for key, env := range scopes {
		if key != env.Code || env.Region != "ap-southeast-1" || env.ClusterID == "" || env.AccountID == "" {
			return nil, fail()
		}
		env.Proxies = append([]domain.ProxyMapping(nil), env.Proxies...)
		copied[key] = env
	}
	return &Client{copied, query}, nil
}
func fresh(stamp float64, now time.Time) bool {
	n := float64(now.UnixNano()) / 1e9
	return !math.IsNaN(stamp) && !math.IsInf(stamp, 0) && stamp > 0 && n-stamp <= 45 && stamp-n <= 5
}
func value(row sample, now time.Time) (float64, error) {
	if len(row.Value) != 2 {
		return 0, fail()
	}
	var stamp float64
	var text string
	if json.Unmarshal(row.Value[0], &stamp) != nil || !fresh(stamp, now) || json.Unmarshal(row.Value[1], &text) != nil {
		return 0, fail()
	}
	v, e := strconv.ParseFloat(text, 64)
	if e != nil || math.IsNaN(v) || math.IsInf(v, 0) || v < 0 {
		return 0, fail()
	}
	return v, nil
}
func (c *Client) vector(ctx context.Context, env domain.Environment, q, evaluation string) ([]sample, error) {
	raw, e := c.query(ctx, env, q, evaluation)
	if e != nil {
		return nil, &domain.CommandError{Code: "MetricsUnavailable"}
	}
	if len(raw) > 1048576 {
		return nil, fail()
	}
	var response struct {
		Status   string   `json:"status"`
		Warnings []string `json:"warnings"`
		Data     struct {
			ResultType string   `json:"resultType"`
			Result     []sample `json:"result"`
		} `json:"data"`
	}
	if json.Unmarshal(raw, &response) != nil || response.Status != "success" || response.Data.ResultType != "vector" || len(response.Warnings) > 0 || response.Data.Result == nil {
		return nil, fail()
	}
	return response.Data.Result, nil
}

type poolKey struct{ id, pool, user, kind string }

func kind(name string) string {
	switch name {
	case "pgcat_pools_cl_active":
		return "active"
	case "pgcat_pools_cl_idle":
		return "idle"
	case "pgcat_pools_cl_waiting":
		return "waiting"
	}
	return ""
}
func (c *Client) ConnectedClients(ctx context.Context, s policy.ProxySnapshot) ([]policy.ClientObservation, error) {
	env, ok := c.scopes[s.EnvCode]
	if !ok || len(s.Nodes) == 0 {
		return nil, fail()
	}
	mappings := 0
	var db string
	for _, m := range env.Proxies {
		if m.Code == s.ProxyCode && m.GroupID == s.GroupID {
			mappings++
			db = m.TargetDBCode
		}
	}
	if mappings != 1 || db == "" {
		return nil, fail()
	}
	members := map[string]bool{}
	for _, n := range s.Nodes {
		if n.ID == "" || members[n.ID] {
			return nil, fail()
		}
		members[n.ID] = true
	}
	selector := fmt.Sprintf(`env=%s,db_proxy_code=%s,target_db_code=%s`, strconv.Quote(s.EnvCode), strconv.Quote(s.ProxyCode), strconv.Quote(db))
	rawSelector := `job="pgcat",` + selector
	q := `{__name__=~"pgcat_pools_cl_(active|idle|waiting)",` + rawSelector + `} or {__name__=~"infra_pgcat_connected_clients|infra_pgcat_client_observation_timestamp_seconds",` + selector + `} or up{` + rawSelector + `}`
	timestampQueries := []string{}
	for _, k := range []string{"active", "idle", "waiting"} {
		timestampQueries = append(timestampQueries, fmt.Sprintf(`label_replace(timestamp(pgcat_pools_cl_%s{%s}),"source_metric",%s,"","")`, k, rawSelector, strconv.Quote(k)))
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	now := time.Now().UTC()
	evaluation := now.Format(time.RFC3339Nano)
	values, e := c.vector(ctx, env, q, evaluation)
	if e != nil {
		return nil, e
	}
	sources, e := c.vector(ctx, env, strings.Join(timestampQueries, " or "), evaluation)
	if e != nil {
		return nil, e
	}
	validLabels := func(m map[string]string) bool {
		return members[m["ecs_instance_id"]] && m["env"] == s.EnvCode && m["db_proxy_code"] == s.ProxyCode && m["target_db_code"] == db
	}
	counts, origins := map[poolKey]float64{}, map[poolKey]string{}
	recorded, observed, ups := map[string]float64{}, map[string]float64{}, map[string]float64{}
	put := func(dst map[string]float64, id string, v float64) error {
		if _, exists := dst[id]; exists {
			return fail()
		}
		dst[id] = v
		return nil
	}
	for _, r := range values {
		if !validLabels(r.Metric) {
			return nil, fail()
		}
		id := r.Metric["ecs_instance_id"]
		v, e := value(r, now)
		if e != nil {
			return nil, e
		}
		k := kind(r.Metric["__name__"])
		if k != "" {
			key := poolKey{id, r.Metric["pool"], r.Metric["user"], k}
			if key.pool == "" || key.user == "" || r.Metric["job"] != "pgcat" || r.Metric["instance"] == "" || math.Trunc(v) != v || v > 9007199254740991 {
				return nil, fail()
			}
			if _, exists := counts[key]; exists {
				return nil, fail()
			}
			counts[key] = v
			origins[key] = r.Metric["instance"]
		} else {
			switch r.Metric["__name__"] {
			case "infra_pgcat_connected_clients":
				e = put(recorded, id, v)
			case "infra_pgcat_client_observation_timestamp_seconds":
				if !fresh(v, now) {
					return nil, fail()
				}
				e = put(observed, id, v)
			case "up":
				if r.Metric["job"] != "pgcat" || r.Metric["instance"] == "" || v != 1 {
					return nil, fail()
				}
				e = put(ups, id, v)
			default:
				return nil, fail()
			}
			if e != nil {
				return nil, e
			}
		}
	}
	sourceTimes := map[poolKey]float64{}
	for _, r := range sources {
		if !validLabels(r.Metric) || r.Metric["job"] != "pgcat" {
			return nil, fail()
		}
		key := poolKey{r.Metric["ecs_instance_id"], r.Metric["pool"], r.Metric["user"], r.Metric["source_metric"]}
		if _, exists := counts[key]; !exists || origins[key] != r.Metric["instance"] {
			return nil, fail()
		}
		if _, exists := sourceTimes[key]; exists {
			return nil, fail()
		}
		stamp, e := value(r, now)
		if e != nil || !fresh(stamp, now) {
			return nil, fail()
		}
		sourceTimes[key] = stamp
	}
	if len(sourceTimes) != len(counts) {
		return nil, fail()
	}
	totals, minSource := map[string]float64{}, map[string]float64{}
	for key, v := range counts {
		for _, k := range []string{"active", "idle", "waiting"} {
			if _, exists := counts[poolKey{key.id, key.pool, key.user, k}]; !exists {
				return nil, fail()
			}
		}
		totals[key.id] += v
		stamp := sourceTimes[key]
		old, present := minSource[key.id]
		if !present || stamp < old {
			minSource[key.id] = stamp
		}
	}
	rows := make([]policy.ClientObservation, 0, len(s.Nodes))
	for _, n := range s.Nodes {
		for _, k := range []string{"active", "idle", "waiting"} {
			if _, exists := counts[poolKey{n.ID, "test", "poc_app", k}]; !exists {
				return nil, fail()
			}
		}
		count, present := recorded[n.ID]
		stamp, hasTime := observed[n.ID]
		_, hasUp := ups[n.ID]
		if !present || !hasTime || !hasUp || count != totals[n.ID] || count > 9007199254740991 || math.Trunc(count) != count || stamp > minSource[n.ID]+1 {
			return nil, fail()
		}
		seconds, fraction := math.Modf(stamp)
		rows = append(rows, policy.ClientObservation{InstanceID: n.ID, Clients: count, ObservedAt: time.Unix(int64(seconds), int64(fraction*1e9))})
	}
	return rows, nil
}
