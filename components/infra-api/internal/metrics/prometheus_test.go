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
