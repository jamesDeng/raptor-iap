package metrics

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"raptor-iap/infra-api/internal/policy"
	"strings"
	"testing"
	"time"
)

type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// Missing/duplicate series and cached timestamps must not become a zero count.
func TestPrometheusRequiresCompleteFreshObservation(t *testing.T) {
	for _, mode := range []string{"ok", "missing", "duplicate", "stale", "wrong-label", "nan"} {
		t.Run(mode, func(t *testing.T) {
			rt := roundTrip(func(r *http.Request) (*http.Response, error) {
				if r.URL.Scheme != "https" || r.URL.Host != "metrics.internal" || r.URL.Path != "/api/v1/query" {
					t.Fatal(r.URL)
				}
				query := r.URL.Query().Get("query")
				if !strings.Contains(query, `ecs_instance_id`) {
					t.Fatal(query)
				}
				value := "0"
				if strings.Contains(query, "observed_at_seconds") {
					value = "1000"
					if mode == "stale" {
						value = "900"
					}
				}
				if mode == "nan" && !strings.Contains(query, "observed_at_seconds") {
					value = "NaN"
				}
				env := "dev"
				if mode == "wrong-label" {
					env = "other"
				}
				row := fmt.Sprintf(`{"metric":{"env_code":%q,"proxy_code":"p","ess_group_id":"g","ecs_instance_id":"i"},"value":[1000,%q]}`, env, value)
				result := row
				if mode == "missing" {
					result = ""
				}
				if mode == "duplicate" {
					result = row + "," + row
				}
				body := `{"status":"success","data":{"resultType":"vector","result":[` + result + `]}}`
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: http.Header{}}, nil
			})
			c, e := New("https://metrics.internal", &http.Client{Transport: rt})
			if e != nil {
				t.Fatal(e)
			}
			snapshot := policy.ProxySnapshot{EnvCode: "dev", ProxyCode: "p", GroupID: "g", Nodes: []policy.Node{{ID: "i"}}}
			rows, e := c.ConnectedClients(context.Background(), snapshot)
			if e == nil {
				e = policy.ScaleInClientsAllowed(snapshot, rows, time.Unix(1000, 0))
			}
			if mode == "ok" {
				if e != nil || len(rows) != 1 {
					t.Fatal(rows, e)
				}
			} else if e == nil {
				t.Fatal("invalid metrics accepted")
			}
		})
	}
}
func TestPrometheusRejectsUnsafeOrigin(t *testing.T) {
	for _, origin := range []string{"http://metrics.internal", "https://u:p@metrics.internal", "https://metrics.internal/?token=secret", "https://metrics.internal/path"} {
		if _, e := New(origin, nil); e == nil {
			t.Fatal(origin)
		}
	}
}

func TestMetricsQueriesUseOneFixedEvaluationTime(t *testing.T) {
	evaluation := ""
	calls := 0
	client, _ := New("https://metrics.internal", &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		calls++
		at := r.URL.Query().Get("time")
		if at == "" {
			t.Error("missing fixed evaluation time")
		}
		if calls == 1 {
			evaluation = at
		} else if at != evaluation {
			t.Error("queries used different snapshots")
		}
		value := "0"
		if strings.Contains(r.URL.Query().Get("query"), "observed_at_seconds") {
			value = "1000"
		}
		body := fmt.Sprintf(`{"status":"success","data":{"resultType":"vector","result":[{"metric":{"env_code":"dev","proxy_code":"p","ess_group_id":"g","ecs_instance_id":"i"},"value":[1000,%q]}]}}`, value)
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: http.Header{}}, nil
	})})
	_, e := client.ConnectedClients(context.Background(), policy.ProxySnapshot{EnvCode: "dev", ProxyCode: "p", GroupID: "g", Nodes: []policy.Node{{ID: "i"}}})
	if e != nil || calls != 2 {
		t.Fatal(e, calls)
	}
}
