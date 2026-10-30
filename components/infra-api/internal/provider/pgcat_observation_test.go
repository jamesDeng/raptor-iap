package provider

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"raptor-iap/infra-api/internal/domain"
)

type seriesTransport func(*http.Request) (*http.Response, error)

func (f seriesTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func seriesFixture(t *testing.T, p *reader, respond func(*http.Request) (string, error)) {
	t.Helper()
	// Real REST client and PrometheusQuery, with only HTTP transport replaced.
	c, err := kubernetes.NewForConfig(&rest.Config{Host: "https://ack.fixture", Transport: seriesTransport(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/api/v1/namespaces/raptor-test/services/http:rdev-test-metrics-metrics:http/proxy/api/v1/query" {
			t.Fatal("unregistered service path", r.URL.Path)
		}
		if _, err := time.Parse(time.RFC3339Nano, r.URL.Query().Get("time")); err != nil {
			t.Fatal("missing evaluation time")
		}
		raw, err := respond(r)
		if err != nil {
			return nil, err
		}
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(raw)), Request: r}, nil
	})})
	if err != nil {
		t.Fatal(err)
	}
	p.identity = p.proxy.identity
	p.kube = func(context.Context, domain.Environment) (kubernetes.Interface, error) { return c, nil }
}

func TestPgcatSeriesOnlyEightScopedExpressions(t *testing.T) {
	for _, tc := range []struct{ series, want string }{
		{"up", `up{job="pgcat",env="dev",db_proxy_code="proxy",target_db_code="db"}`},
		{"upTimestamp", `timestamp(up{job="pgcat",env="dev",db_proxy_code="proxy",target_db_code="db"})`},
		{"active", `pgcat_pools_cl_active{job="pgcat",env="dev",db_proxy_code="proxy",target_db_code="db"}`},
		{"activeTimestamp", `timestamp(pgcat_pools_cl_active{job="pgcat",env="dev",db_proxy_code="proxy",target_db_code="db"})`},
		{"idle", `pgcat_pools_cl_idle{job="pgcat",env="dev",db_proxy_code="proxy",target_db_code="db"}`},
		{"idleTimestamp", `timestamp(pgcat_pools_cl_idle{job="pgcat",env="dev",db_proxy_code="proxy",target_db_code="db"})`},
		{"waiting", `pgcat_pools_cl_waiting{job="pgcat",env="dev",db_proxy_code="proxy",target_db_code="db"}`},
		{"waitingTimestamp", `timestamp(pgcat_pools_cl_waiting{job="pgcat",env="dev",db_proxy_code="proxy",target_db_code="db"})`},
	} {
		t.Run(tc.series, func(t *testing.T) {
			p, env, target, _, _, _, _ := observationFixture(t)
			seriesFixture(t, p, func(r *http.Request) (string, error) {
				if r.URL.Query().Get("query") != tc.want {
					t.Fatalf("wrong query %s", r.URL.Query().Get("query"))
				}
				return `{"status":"success","data":{"resultType":"vector","result":[{"metric":{"job":"pgcat","env":"dev","db_proxy_code":"proxy","target_db_code":"db","ecs_instance_id":"i-one","pool":"test","user":"poc_app"},"value":[1793300000.125,"1.00"]}]}}`, nil
			})
			got, err := p.ObservePgcatSeries(context.Background(), env, target, tc.series)
			if err != nil {
				t.Fatal(err)
			}
			if got.Series != tc.series || got.ObservedAt.IsZero() || len(got.Rows) != 1 || string(got.Rows[0].Value[0]) != "1793300000.125" || string(got.Rows[0].Value[1]) != `"1.00"` || got.Rows[0].Metric["pool"] != "test" {
				t.Fatalf("lost raw vector: %+v", got)
			}
			raw, _ := json.Marshal(got)
			if !strings.Contains(string(raw), `"series":"`+tc.series+`"`) || !strings.Contains(string(raw), `"rows":[{"metric":`) {
				t.Fatal(string(raw))
			}
		})
	}
}

func TestPgcatSeriesRejectsMalformedAndUnboundedResponses(t *testing.T) {
	const labels = `{"job":"pgcat","env":"dev","db_proxy_code":"proxy","target_db_code":"db"}`
	for _, tc := range []struct{ name, raw string }{
		{"warnings", `{"status":"success","warnings":["partial"],"data":{"resultType":"vector","result":[]}}`},
		{"provider error", `{"status":"error","error":"private token"}`},
		{"matrix", `{"status":"success","data":{"resultType":"matrix","result":[]}}`},
		{"missing rows", `{"status":"success","data":{"resultType":"vector"}}`},
		{"foreign scope", `{"status":"success","data":{"resultType":"vector","result":[{"metric":{"job":"pgcat","env":"foreign","db_proxy_code":"proxy","target_db_code":"db"},"value":[1,"1"]}]}}`},
		{"missing labels", `{"status":"success","data":{"resultType":"vector","result":[{"metric":{},"value":[1,"1"]}]}}`},
		{"missing metric", `{"status":"success","data":{"resultType":"vector","result":[{"value":[1,"1"]}]}}`},
		{"missing value", `{"status":"success","data":{"resultType":"vector","result":[{"metric":` + labels + `}]}}`},
		{"numeric value", `{"status":"success","data":{"resultType":"vector","result":[{"metric":` + labels + `,"value":[1,1]}]}}`},
		{"bad timestamp", `{"status":"success","data":{"resultType":"vector","result":[{"metric":` + labels + `,"value":["bad","1"]}]}}`},
		{"invalid value", `{"status":"success","data":{"resultType":"vector","result":[{"metric":` + labels + `,"value":[1,"NaN"]}]}}`},
		{"malformed", `{"status":`},
		{"too large", strings.Repeat("x", 1048577)},
		{"too many rows", `{"status":"success","data":{"resultType":"vector","result":[` + strings.TrimSuffix(strings.Repeat(`{"metric":`+labels+`,"value":[1,"1"]},`, 101), ",") + `]}}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p, env, target, _, _, _, _ := observationFixture(t)
			seriesFixture(t, p, func(*http.Request) (string, error) { return tc.raw, nil })
			_, err := p.ObservePgcatSeries(context.Background(), env, target, "up")
			if err == nil || strings.Contains(err.Error(), "private") {
				t.Fatal("accepted/leaked invalid metrics", err)
			}
		})
	}
}

func TestPgcatSeriesScopeAndUnknownSeriesFailBeforeQuery(t *testing.T) {
	for _, kind := range []string{"unknown", "promql", "url", "account", "mapping", "target database", "group pin", "backend pin", "cluster missing"} {
		t.Run(kind, func(t *testing.T) {
			p, env, target, g, _, _, _ := observationFixture(t)
			series := "up"
			seriesFixture(t, p, func(*http.Request) (string, error) { t.Fatal("query reached despite invalid scope"); return "", nil })
			switch kind {
			case "unknown":
				series = "UP"
			case "promql":
				series = "up{job=\"other\"}"
			case "url":
				series = "https://other"
			case "account":
				env.AccountID = "foreign"
			case "mapping":
				env.Proxies = nil
			case "target database":
				g.Tags[2].TagValue = nil
			case "group pin":
				target.GroupID = "foreign"
			case "backend pin":
				target.ServerGroupID = "foreign"
			case "cluster missing":
				env.ClusterID = ""
			}
			if _, err := p.ObservePgcatSeries(context.Background(), env, target, series); err == nil {
				t.Fatal("accepted invalid read")
			}
		})
	}
}

func TestPgcatSeriesSanitizesTransportAndPreservesEmptyVector(t *testing.T) {
	for _, kind := range []string{"empty", "failure"} {
		p, env, target, _, _, _, _ := observationFixture(t)
		seriesFixture(t, p, func(*http.Request) (string, error) {
			if kind == "failure" {
				return "", errors.New("private credential")
			}
			return `{"status":"success","data":{"resultType":"vector","result":[]}}`, nil
		})
		got, err := p.ObservePgcatSeries(context.Background(), env, target, "up")
		if kind == "failure" {
			if err == nil || strings.Contains(err.Error(), "private") {
				t.Fatal(err)
			}
			continue
		}
		if err != nil || got.Rows == nil || len(got.Rows) != 0 {
			t.Fatal(got, err)
		}
	}
}

func TestPgcatSeriesPreservesBoundedRawRows(t *testing.T) {
	p, env, target, _, _, _, _ := observationFixture(t)
	row := `{"metric":{"job":"pgcat","env":"dev","db_proxy_code":"proxy","target_db_code":"db"},"value":[1793300000,"0"]}`
	seriesFixture(t, p, func(*http.Request) (string, error) {
		return `{"status":"success","data":{"resultType":"vector","result":[` + strings.TrimSuffix(strings.Repeat(row+",", 100), ",") + `]}}`, nil
	})
	got, err := p.ObservePgcatSeries(context.Background(), env, target, "active")
	if err != nil || len(got.Rows) != 100 {
		t.Fatalf("lost bounded raw rows: %d %v", len(got.Rows), err)
	}
}
