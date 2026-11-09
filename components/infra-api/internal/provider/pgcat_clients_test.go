package provider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"raptor-iap/infra-api/internal/api"
	"raptor-iap/infra-api/internal/domain"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	ess "github.com/alibabacloud-go/ess-20220222/v2/client"
	"github.com/alibabacloud-go/tea/dara"
	"github.com/alibabacloud-go/tea/tea"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

const clientTestLabels = `env="dev",db_proxy_code="proxy",target_db_code="db",ecs_instance_id=~"i-one|i-two"`

var clientsQueries = []string{
	clientTestQuery("up", false),
	clientTestQuery("up", true),
	clientTestQuery("pgcat_pools_cl_active", false),
	clientTestQuery("pgcat_pools_cl_active", true),
	clientTestQuery("pgcat_pools_cl_idle", false),
	clientTestQuery("pgcat_pools_cl_idle", true),
	clientTestQuery("pgcat_pools_cl_waiting", false),
	clientTestQuery("pgcat_pools_cl_waiting", true),
}

func clientTestQuery(metric string, timestamp bool) string {
	agent := fmt.Sprintf(`%s{job="pgcat-agent",%s}`, metric, clientTestLabels)
	central := fmt.Sprintf(`%s{job="pgcat",%s}`, metric, clientTestLabels)
	if timestamp {
		agent, central = "timestamp("+agent+")", "timestamp("+central+")"
	}
	return fmt.Sprintf(`(%s or (%s unless on (env,db_proxy_code,target_db_code,ecs_instance_id) up{job="pgcat-agent",%s}))`, agent, central, clientTestLabels)
}

const clientsVector = `{"status":"success","data":{"resultType":"vector","result":[{"metric":{"job":"pgcat","env":"dev","db_proxy_code":"proxy","target_db_code":"db","ecs_instance_id":"i-one","pool":"test","user":"poc_app"},"value":[1793300000.125,"1.00"]}]}}`

func requireClientsObserver(t *testing.T, p *reader) api.PgcatClientsObserver {
	t.Helper()
	observer, ok := any(p).(api.PgcatClientsObserver)
	if !ok {
		t.Fatal("private provider does not implement the complete clients observation")
	}
	return observer
}

// Reconstructing ACK clients per series, re-reading inventory or executing
// grouped pairs instead of refilling either available slot breaks this test.
func TestPgcatClientsSharesCheckedInventoryAndClientWithTwoRefillableQueries(t *testing.T) {
	p, env, target, _, _, _, _ := observationFixture(t)
	observer := requireClientsObserver(t, p)
	var mu sync.Mutex
	seen := map[string]int{}
	evaluations := map[string]bool{}
	active, maxActive := 0, 0
	upStarted, activeStarted, releaseUp := make(chan struct{}), make(chan struct{}), make(chan struct{})
	defer close(releaseUp)
	seriesFixture(t, p, func(r *http.Request) (string, error) {
		query := r.URL.Query().Get("query")
		known := false
		for _, allowed := range clientsQueries {
			if query == allowed {
				known = true
			}
		}
		if !known {
			t.Errorf("arbitrary query escaped: %q", query)
			return "", errors.New("unexpected query")
		}
		mu.Lock()
		seen[query]++
		evaluations[r.URL.Query().Get("time")] = true
		active++
		if active > maxActive {
			maxActive = active
		}
		mu.Unlock()
		defer func() { mu.Lock(); active--; mu.Unlock() }()
		if query == clientsQueries[0] {
			close(upStarted)
			select {
			case <-releaseUp:
			case <-r.Context().Done():
				return "", r.Context().Err()
			}
		}
		if query == clientsQueries[2] {
			close(activeStarted)
		}
		return clientsVector, nil
	})
	kube := p.kube
	var clients atomic.Int32
	p.kube = func(ctx context.Context, env domain.Environment) (kubernetes.Interface, error) {
		clients.Add(1)
		return kube(ctx, env)
	}
	groups, groupCalls := p.proxy.sdk.groups, 0
	p.proxy.sdk.groups = func(ctx context.Context, r *ess.DescribeScalingGroupsRequest, o *dara.RuntimeOptions) (*ess.DescribeScalingGroupsResponse, error) {
		groupCalls++
		return groups(ctx, r, o)
	}
	members, memberCalls := p.proxy.sdk.instances, 0
	p.proxy.sdk.instances = func(ctx context.Context, r *ess.DescribeScalingInstancesRequest, o *dara.RuntimeOptions) (*ess.DescribeScalingInstancesResponse, error) {
		memberCalls++
		return members(ctx, r, o)
	}
	identity, mappingChecks := p.proxy.identity, 0
	p.proxy.identity = func(ctx context.Context, env domain.Environment) (string, error) {
		mappingChecks++
		return identity(ctx, env)
	}
	type outcome struct {
		data domain.PgcatClientsObservation
		err  error
	}
	done := make(chan outcome, 1)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	go func() { data, err := observer.ObservePgcatClients(ctx, env, target); done <- outcome{data, err} }()
	select {
	case <-upStarted:
	case <-ctx.Done():
		t.Fatal("first query never started")
	}
	select {
	case <-activeStarted:
	case <-ctx.Done():
		t.Fatal("free query slot was not refilled while up remained in flight")
	}
	// Release without relying on wall-clock query timing.
	releaseUp <- struct{}{}
	got := <-done
	if got.err != nil {
		t.Fatal(got.err)
	}
	if clients.Load() != 1 || groupCalls != 2 || memberCalls != 1 || mappingChecks != 1 {
		t.Fatalf("repeated setup: clients=%d group list/detail=%d members=%d mapping=%d", clients.Load(), groupCalls, memberCalls, mappingChecks)
	}
	mu.Lock()
	defer mu.Unlock()
	if maxActive != 2 || active != 0 || len(seen) != 8 || len(evaluations) != 1 {
		t.Fatalf("invalid bounded batch: active=%d max=%d queries=%v evaluations=%v", active, maxActive, seen, evaluations)
	}
	for _, count := range seen {
		if count != 1 {
			t.Fatal("query retried", seen)
		}
	}
	if got.data.Series != "clients" || got.data.Fleet.GroupID != "asg-test" || got.data.Backend.ServerGroupID != "sgp-test" || len(got.data.Fleet.Instances) != 2 || len(got.data.Backend.Nodes) != 2 || !got.data.Backend.Nodes[0].Healthy {
		t.Fatalf("bad inventory: %+v", got.data)
	}
	if got.data.Fleet.ObservedAt.IsZero() || got.data.Backend.ObservedAt.Before(got.data.Fleet.ObservedAt) || got.data.ObservedAt.Before(got.data.Backend.ObservedAt) {
		t.Fatal("source timestamps replaced", got.data)
	}
	raw, _ := json.Marshal(got.data)
	var decoded map[string]json.RawMessage
	_ = json.Unmarshal(raw, &decoded)
	var samples map[string][]domain.PrometheusSample
	_ = json.Unmarshal(decoded["samples"], &samples)
	if len(decoded) != 5 || len(samples) != 8 {
		t.Fatal(string(raw))
	}
	for _, key := range []string{"up", "upTimestamp", "active", "activeTimestamp", "idle", "idleTimestamp", "waiting", "waitingTimestamp"} {
		rows := samples[key]
		if len(rows) != 1 || string(rows[0].Value[0]) != "1793300000.125" || string(rows[0].Value[1]) != `"1.00"` || rows[0].Metric["pool"] != "test" {
			t.Fatal("raw vector lost", key, rows)
		}
	}
}

// A failed vector cancels the other active query and never starts queued work.
func TestPgcatClientsFailureCancelsPeerAndQueuedQueries(t *testing.T) {
	for _, failure := range []string{"transport", "invalid", "cancelled"} {
		t.Run(failure, func(t *testing.T) {
			p, env, target, _, _, _, _ := observationFixture(t)
			observer := requireClientsObserver(t, p)
			var started, finished atomic.Int32
			both := make(chan struct{})
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			seriesFixture(t, p, func(r *http.Request) (string, error) {
				n := started.Add(1)
				if n == 2 {
					close(both)
				}
				defer finished.Add(1)
				select {
				case <-both:
				case <-r.Context().Done():
					return "", r.Context().Err()
				}
				if failure == "cancelled" {
					cancel()
				}
				if r.URL.Query().Get("query") == clientsQueries[0] {
					if failure == "transport" {
						return "", errors.New("private credential")
					}
					return `{"status":"error","error":"private credential"}`, nil
				}
				<-r.Context().Done()
				return "", r.Context().Err()
			})
			got, err := observer.ObservePgcatClients(ctx, env, target)
			if err == nil || strings.Contains(err.Error(), "private") || !reflect.DeepEqual(got, domain.PgcatClientsObservation{}) || started.Load() != 2 || finished.Load() != 2 {
				t.Fatalf("partial result/late query: %+v err=%v started=%d finished=%d", got, err, started.Load(), finished.Load())
			}
		})
	}
}

func TestPgcatClientsRejectsMalformedRawVectors(t *testing.T) {
	row := `{"metric":{"job":"pgcat","env":"dev","db_proxy_code":"proxy","target_db_code":"db","ecs_instance_id":"i-one"},"value":[1793300000.125,"1.00"]}`
	for _, raw := range []string{
		`{"status":"success","data":{"resultType":"vector"}}`,
		`{"status":"success","data":{"resultType":"vector","result":[` + row + `,` + row + `]}}`,
		strings.Replace(clientsVector, `"env":"dev"`, `"env":"foreign"`, 1),
		strings.Replace(clientsVector, `"i-one"`, `"i-retired"`, 1),
		strings.Replace(clientsVector, `"1.00"`, `"NaN"`, 1),
		strings.Replace(clientsVector, `1793300000.125`, `"bad"`, 1),
		strings.Replace(clientsVector, `"1.00"`, `1`, 1),
		`{"status":"success","warnings":["partial"],"data":{"resultType":"vector","result":[]}}`,
	} {
		t.Run(fmt.Sprint(len(raw), strings.Contains(raw, "NaN")), func(t *testing.T) {
			p, env, target, _, _, _, _ := observationFixture(t)
			observer := requireClientsObserver(t, p)
			seriesFixture(t, p, func(r *http.Request) (string, error) {
				if r.URL.Query().Get("query") == clientsQueries[3] {
					return raw, nil
				}
				return clientsVector, nil
			})
			got, err := observer.ObservePgcatClients(context.Background(), env, target)
			if err == nil || !reflect.DeepEqual(got, domain.PgcatClientsObservation{}) {
				t.Fatal("accepted partial/bad vector", got, err)
			}
		})
	}
}

func TestPgcatClientsReusesFleetAndBackendValidationBeforeMetrics(t *testing.T) {
	for _, change := range []string{"mapping", "group", "members", "backend", "health", "cluster"} {
		t.Run(change, func(t *testing.T) {
			p, env, target, g, m, s, h := observationFixture(t)
			observer := requireClientsObserver(t, p)
			switch change {
			case "mapping":
				env.Proxies = nil
			case "group":
				g.DesiredCapacity = nil
			case "members":
				m.ScalingInstances[1] = m.ScalingInstances[0]
			case "backend":
				s.Servers[1] = s.Servers[0]
			case "health":
				h.NextToken = tea.String("more")
			case "cluster":
				env.ClusterID = ""
			}
			seriesFixture(t, p, func(*http.Request) (string, error) {
				t.Error("invalid inventory reached metrics")
				return clientsVector, nil
			})
			if _, err := observer.ObservePgcatClients(context.Background(), env, target); err == nil {
				t.Fatal("accepted invalid bundle")
			}
		})
	}
}

func TestPgcatClientsDoesNotRetryPrivateRateLimit(t *testing.T) {
	p, env, target, _, _, _, _ := observationFixture(t)
	observer := requireClientsObserver(t, p)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	var calls atomic.Int32
	client, err := kubernetes.NewForConfig(&rest.Config{Host: "https://ack.fixture", Transport: seriesTransport(func(r *http.Request) (*http.Response, error) {
		if calls.Add(1) > 2 {
			cancel()
		}
		return &http.Response{StatusCode: 429, Header: http.Header{"Retry-After": []string{"0"}}, Body: io.NopCloser(strings.NewReader(`{"error":"private rate limit"}`)), Request: r}, nil
	})})
	if err != nil {
		t.Fatal(err)
	}
	p.identity = p.proxy.identity
	p.kube = func(context.Context, domain.Environment) (kubernetes.Interface, error) { return client, nil }
	_, err = observer.ObservePgcatClients(ctx, env, target)
	if err == nil || strings.Contains(err.Error(), "private") || calls.Load() > 2 {
		t.Fatalf("private query retried after rate limit: requests=%d err=%v", calls.Load(), err)
	}
}

func TestPgcatClientsSelectsAutomaticSamplesForOnlyProviderMembers(t *testing.T) {
	p, env, target, _, _, _, _ := observationFixture(t)
	seriesFixture(t, p, func(r *http.Request) (string, error) {
		query := r.URL.Query().Get("query")
		if !strings.Contains(query, "job=\"pgcat-agent\"") || !strings.Contains(query, "ecs_instance_id=~\"i-one|i-two\"") {
			t.Errorf("query does not select automatically reported samples for the verified fleet: %s", query)
		}
		return strings.Replace(clientsVector, "job\":\"pgcat\"", "job\":\"pgcat-agent\"", 1), nil
	})
	if _, err := requireClientsObserver(t, p).ObservePgcatClients(context.Background(), env, target); err != nil {
		t.Fatal(err)
	}
}
