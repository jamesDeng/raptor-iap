package metrics

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"raptor-iap/infra-api/internal/domain"
	"raptor-iap/infra-api/internal/policy"
	"strings"
	"testing"
	"time"
)

func metricFixture(t *testing.T) (*Client, policy.ProxySnapshot, *[]sample, *[]sample) {
	t.Helper()
	stamp := float64(time.Now().Unix() - 5)
	scopes := map[string]domain.Environment{"dev": {Code: "dev", AccountID: "123", Region: "ap-southeast-1", ClusterID: "cluster", Proxies: []domain.ProxyMapping{{Code: "proxy", GroupID: "asg", TargetDBCode: "db"}}}}
	rows, times := []sample{}, []sample{}
	for _, id := range []string{"i-one", "i-two"} {
		base := map[string]string{"env": "dev", "db_proxy_code": "proxy", "target_db_code": "db", "ecs_instance_id": id}
		add := func(name string, value float64, pool bool) {
			m := map[string]string{}
			for k, v := range base {
				m[k] = v
			}
			m["__name__"] = name
			if pool {
				m["pool"] = "test"
				m["user"] = "poc_app"
				m["job"] = "pgcat"
				m["instance"] = id + ":9930"
			}
			rows = append(rows, sample{m, []json.RawMessage{json.RawMessage(fmt.Sprint(stamp)), json.RawMessage(fmt.Sprintf("%q", fmt.Sprint(value)))}})
		}
		add("pgcat_pools_cl_active", 0, true)
		add("pgcat_pools_cl_idle", 0, true)
		add("pgcat_pools_cl_waiting", 0, true)
		add("infra_pgcat_connected_clients", 0, false)
		add("infra_pgcat_client_observation_timestamp_seconds", stamp, false)
		add("up", 1, false)
		rows[len(rows)-1].Metric["job"] = "pgcat"
		rows[len(rows)-1].Metric["instance"] = id + ":9930"
		for _, kind := range []string{"active", "idle", "waiting"} {
			m := map[string]string{}
			for k, v := range base {
				m[k] = v
			}
			m["source_metric"] = kind
			m["pool"] = "test"
			m["user"] = "poc_app"
			m["job"] = "pgcat"
			m["instance"] = id + ":9930"
			times = append(times, sample{m, []json.RawMessage{json.RawMessage(fmt.Sprint(stamp)), json.RawMessage(fmt.Sprintf("%q", fmt.Sprint(stamp)))}})
		}
	}
	query := func(_ context.Context, env domain.Environment, q, evaluation string) ([]byte, error) {
		if env.ClusterID != "cluster" || !strings.Contains(q, `env="dev"`) || !strings.Contains(q, `db_proxy_code="proxy"`) || !strings.Contains(q, `target_db_code="db"`) {
			t.Fatal("unscoped query")
		}
		result := rows
		if strings.Contains(q, "timestamp(") {
			result = times
		}
		return json.Marshal(map[string]any{"status": "success", "data": map[string]any{"resultType": "vector", "result": result}})
	}
	c, e := New(scopes, query)
	if e != nil {
		t.Fatal(e)
	}
	s := policy.ProxySnapshot{EnvCode: "dev", ProxyCode: "proxy", GroupID: "asg", Nodes: []policy.Node{{ID: "i-one", Protected: true}, {ID: "i-two"}}}
	return c, s, &rows, &times
}
func TestMetricsRequiresEveryNodeIncludingProtected(t *testing.T) {
	c, s, _, _ := metricFixture(t)
	rows, e := c.ConnectedClients(context.Background(), s)
	if e != nil {
		t.Fatal(e)
	}
	if len(rows) != 2 || rows[0].InstanceID != "i-one" || rows[1].Clients != 0 || time.Since(rows[1].ObservedAt) > 10*time.Second {
		t.Fatalf("wrong observation: %+v", rows)
	}
}
func TestMetricsAcceptsAgentAndSelectsItBeforeCentralScrape(t *testing.T) {
	c, s, rows, times := metricFixture(t)
	previous := c.query
	queries := []string{}
	c.query = func(ctx context.Context, env domain.Environment, q, evaluation string) ([]byte, error) {
		queries = append(queries, q)
		return previous(ctx, env, q, evaluation)
	}
	for i := 0; i < 6; i++ {
		if (*rows)[i].Metric["job"] == "pgcat" {
			(*rows)[i].Metric["job"] = "pgcat-agent"
		}
	}
	for i := 0; i < 3; i++ {
		(*times)[i].Metric["job"] = "pgcat-agent"
	}
	if _, err := c.ConnectedClients(context.Background(), s); err != nil {
		t.Fatalf("agent observations refused: %v", err)
	}
	if len(queries) != 2 || !strings.Contains(queries[0], `up{job="pgcat-agent"`) || !strings.Contains(queries[0], `unless on (env,db_proxy_code,target_db_code,ecs_instance_id)`) || !strings.Contains(queries[1], `job="pgcat-agent"`) {
		t.Fatal("agent precedence missing from query")
	}
}
func TestMetricsRefusesMissingStaleOrAmbiguousCoverage(t *testing.T) {
	cases := []string{"missing raw", "duplicate raw", "missing protected", "missing pool", "missing user", "down", "stale raw", "stale recorded", "negative", "fraction", "nan", "infinite", "record mismatch", "foreign node", "foreign db", "duplicate up", "missing source", "duplicate source", "foreign group", "empty nodes"}
	for _, kind := range cases {
		t.Run(kind, func(t *testing.T) {
			c, s, rows, times := metricFixture(t)
			set := func(i int, v float64) { (*rows)[i].Value[1] = json.RawMessage(fmt.Sprintf("%q", fmt.Sprint(v))) }
			switch kind {
			case "missing raw":
				*rows = append((*rows)[:1], (*rows)[2:]...)
			case "duplicate raw":
				*rows = append(*rows, (*rows)[0])
			case "missing protected":
				*rows = (*rows)[6:]
			case "missing pool":
				(*rows)[0].Metric["pool"] = "other"
			case "missing user":
				(*rows)[0].Metric["user"] = "other"
			case "down":
				set(5, 0)
			case "stale raw":
				(*times)[0].Value[1] = json.RawMessage(fmt.Sprintf("%q", fmt.Sprint(time.Now().Unix()-90)))
			case "stale recorded":
				set(4, float64(time.Now().Unix()-90))
			case "negative":
				set(0, -1)
			case "fraction":
				set(0, 0.5)
			case "nan":
				set(0, math.NaN())
			case "infinite":
				set(0, math.Inf(1))
			case "record mismatch":
				set(3, 1)
			case "foreign node":
				(*rows)[0].Metric["ecs_instance_id"] = "foreign"
			case "foreign db":
				(*rows)[0].Metric["target_db_code"] = "other"
			case "duplicate up":
				*rows = append(*rows, (*rows)[5])
			case "missing source":
				*times = (*times)[1:]
			case "duplicate source":
				*times = append(*times, (*times)[0])
			case "foreign group":
				s.GroupID = "foreign"
			case "empty nodes":
				s.Nodes = nil
			}
			if _, e := c.ConnectedClients(context.Background(), s); e == nil {
				t.Fatal("accepted invalid metrics")
			}
		})
	}
}

func TestResourceMetricsResolveCurrentMappingBeforeQuery(t *testing.T) {
	calls := 0
	queries := 0
	c, e := NewResource(func(_ context.Context, env, kind, code string) (domain.ResolvedResource, error) {
		calls++
		if env != "dev" || kind != "db-proxy" || code != "proxy" {
			t.Fatal("logical identity")
		}
		return domain.ResolvedResource{Environment: domain.Environment{Code: "dev", AccountID: "123", Region: "ap-southeast-1", ClusterID: "c"}, Proxy: &domain.ProxyMapping{Code: "proxy", GroupID: "changed", TargetDBCode: "db"}}, nil
	}, func(context.Context, domain.Environment, string, string) ([]byte, error) { queries++; return nil, nil })
	if e != nil {
		t.Fatal(e)
	}
	for i := 0; i < 2; i++ {
		if _, e = c.ConnectedClients(context.Background(), policy.ProxySnapshot{EnvCode: "dev", ProxyCode: "proxy", GroupID: "original", Nodes: []policy.Node{{ID: "one"}}}); e == nil {
			t.Fatal("changed group accepted")
		}
	}
	if calls != 2 || queries != 0 {
		t.Fatal("cached or premature query", calls, queries)
	}
}

func TestResourceMetricsRetainsCompletePoolGate(t *testing.T) {
	legacy, s, _, _ := metricFixture(t)
	calls := 0
	c, e := NewResource(func(_ context.Context, env, kind, code string) (domain.ResolvedResource, error) {
		calls++
		current := legacy.scopes[env]
		mapping := current.Proxies[0]
		return domain.ResolvedResource{Environment: current, Proxy: &mapping}, nil
	}, legacy.query)
	if e != nil {
		t.Fatal(e)
	}
	rows, e := c.ConnectedClients(context.Background(), s)
	if e != nil || len(rows) != len(s.Nodes) || calls != 1 {
		t.Fatal(rows, e, calls)
	}
	for _, row := range rows {
		if row.Clients != 0 || row.ObservedAt.IsZero() {
			t.Fatal("dynamic resolver changed source observations", row)
		}
	}
}

func TestMetricsQueriesOnlyCurrentProviderMembers(t *testing.T) {
	c, s, _, _ := metricFixture(t)
	previous := c.query
	c.query = func(ctx context.Context, env domain.Environment, q, evaluation string) ([]byte, error) {
		// Both value and source-time queries exclude series for already retired
		// nodes, even when Prometheus still holds their latest samples.
		if !strings.Contains(q, "ecs_instance_id=~\"i-one|i-two\"") ||
			strings.Count(q, "target_db_code=") != strings.Count(q, "ecs_instance_id=~") {
			t.Fatalf("query includes selectors without an exact fleet match: %s", q)
		}
		return previous(ctx, env, q, evaluation)
	}
	if _, err := c.ConnectedClients(context.Background(), s); err != nil {
		t.Fatal(err)
	}
}
