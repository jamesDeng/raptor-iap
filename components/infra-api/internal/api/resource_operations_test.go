package api

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"net/url"
	"raptor-iap/infra-api/internal/domain"
	"raptor-iap/infra-api/internal/resources"
	"strings"
	"testing"
	"time"
)

type resourceObserver struct {
	observedReader
	at     time.Time
	target domain.ProxyReadTarget
}

func (f *resourceObserver) ObserveProxy(_ context.Context, _ domain.Environment, t domain.ProxyReadTarget, kind string) (domain.ProxyObservation, error) {
	f.target = t
	switch kind {
	case "fleet":
		return domain.ProxyObservation{Fleet: &domain.FleetObservation{GroupID: "g", ObservedAt: f.at, Instances: []domain.FleetNode{}}}, nil
	case "backend":
		return domain.ProxyObservation{Backend: &domain.BackendObservation{ServerGroupID: "sg", ListenerID: "l", ObservedAt: f.at, Nodes: []domain.BackendNode{}}}, nil
	}
	return domain.ProxyObservation{Capacity: &domain.CapacityObservation{GroupID: "g", ObservedAt: f.at}}, nil
}
func (f *resourceObserver) ObservePgcatSeries(_ context.Context, _ domain.Environment, t domain.ProxyReadTarget, series string) (domain.PgcatSeriesObservation, error) {
	f.target = t
	return domain.PgcatSeriesObservation{Series: series, ObservedAt: f.at, Rows: []domain.PrometheusSample{{Metric: map[string]string{"instance_id": "i"}, Value: []json.RawMessage{json.RawMessage(`123`), json.RawMessage(`"4"`)}}}}, nil
}
func TestResourceObservationsPreserveSourceTimestampsAndSamples(t *testing.T) {
	reader := &resourceObserver{at: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)}
	f := &resourceFixture{rows: resourceRows()}
	op := resourceOperations{reader: reader, resolver: f, principal: "user"}
	for _, tc := range []struct{ tool, field, value, nested string }{{"proxy_observation_get", "kind", "fleet", "fleet"}, {"proxy_observation_get", "kind", "backend", "backend"}, {"proxy_observation_get", "kind", "capacity", "capacity"}, {"proxy_pgcat_metrics_get", "series", "active", "metrics"}} {
		data, e := op.proxyRead(context.Background(), tc.tool, map[string]string{"requestId": "r", "envCode": "dev", "proxyCode": "p", tc.field: tc.value})
		if e != nil {
			t.Fatal(e)
		}
		raw, _ := json.Marshal(data)
		s := string(raw)
		if !strings.Contains(s, `"observedAt":"2026-10-01T00:00:00Z"`) || !strings.Contains(s, `"type":"ess"`) || !strings.Contains(s, `"`+tc.nested+`"`) {
			t.Fatal(s)
		}
		if tc.nested == "metrics" && !strings.Contains(s, `"value":[123,"4"]`) {
			t.Fatal(s)
		}
		if reader.target.GroupID != "g" || reader.target.ServerGroupID != "sg" {
			t.Fatal(reader.target)
		}
	}
}
func TestResourceProofRequiresExplicitServiceAuthorizer(t *testing.T) {
	f := &resourceFixture{}
	h, e := NewResourceServer(&observedReader{}, f, "user", "secret", "X-Infra-Authorization", nil, nil, nil)
	if e != nil {
		t.Fatal(e)
	}
	w := resourceHTTP(h, "GET", "/v1/resource-resolution-proof?envCode=dev&proxyCode=p", "", valid())
	if w.Code != 403 || f.proofs != 0 {
		t.Fatal(w.Code, f.proofs)
	}
}
func TestResourceDiscoveryChecksLifecycle(t *testing.T) {
	checks := 0
	f := &resourceFixture{}
	h := resourceHandler(t, f, func(_ context.Context, _ string, tool string, s map[string]string) (AgentIdentity, error) {
		checks++
		if tool != "" || len(s) != 0 {
			t.Error(tool, s)
		}
		return AgentIdentity{}, nil
	})
	for _, method := range []string{"initialize", "tools/list"} {
		r := httptest.NewRequest("POST", "/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"`+method+`"}`))
		r.Header.Set("X-Infra-Authorization", agentCredential())
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != 401 {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	if checks != 2 || f.calls != 0 {
		t.Fatal(checks, f.calls)
	}
}
func TestResourceApplicationQuestionRejectsForeignCluster(t *testing.T) {
	i := resourceIdentity()
	f := &resourceFixture{rows: resourceRows()}
	f.rows.Environment.ClusterID = "foreign"
	f.rows.ApplicationWorkloads[0].Target.ClusterID = "foreign"
	h := resourceHandler(t, f, func(context.Context, string, string, map[string]string) (AgentIdentity, error) { return i, nil })
	w := resourceHTTP(h, "GET", "/v1/deployment-status?envCode=dev&appCode=app&workloadKey=wk", "", agentCredential())
	if w.Code != 409 {
		t.Fatal(w.Code, w.Body.String())
	}
}

type resourceStatusReader struct {
	observedReader
	at time.Time
}

func (f *resourceStatusReader) Status(_ context.Context, _ domain.Environment, t domain.Target) (domain.Status, error) {
	return domain.Status{UID: t.UID, ObservedAt: f.at}, nil
}
func TestResourceStatusTimestampsAndEmptyWorkloadLists(t *testing.T) {
	f := &resourceFixture{rows: resourceRows()}
	for _, source := range []time.Time{{}, time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)} {
		h, e := NewResourceServer(&resourceStatusReader{at: source}, f, "user", "secret", "X-Infra-Authorization", nil, nil, nil)
		if e != nil {
			t.Fatal(e)
		}
		w := resourceHTTP(h, "GET", "/v1/deployment-status?envCode=dev&appCode=app", "", valid())
		var got struct {
			Data struct {
				Status domain.Status `json:"status"`
			} `json:"data"`
		}
		if json.Unmarshal(w.Body.Bytes(), &got) != nil || w.Code != 200 {
			t.Fatal(w.Code, w.Body.String())
		}
		if source.IsZero() {
			if got.Data.Status.ObservedAt.IsZero() || time.Since(got.Data.Status.ObservedAt) > time.Second {
				t.Fatal("fresh provider status missing observation time", w.Body.String())
			}
		} else if !got.Data.Status.ObservedAt.Equal(source) {
			t.Fatal("source time replaced", w.Body.String())
		}
	}
	f.rows.ApplicationWorkloads = nil
	w := resourceHTTP(resourceHandler(t, f, nil), "GET", "/v1/deployments?envCode=dev&kind=application&code=app", "", valid())
	if !strings.Contains(w.Body.String(), `"applicationWorkloads":[]`) {
		t.Fatal(w.Body.String())
	}
}

type resourceDeadlineResolver struct{ deadline time.Time }

func (f *resourceDeadlineResolver) Resolve(ctx context.Context, _, _, _ string) (domain.ResolvedResource, error) {
	f.deadline, _ = ctx.Deadline()
	return domain.ResolvedResource{}, context.DeadlineExceeded
}
func (f *resourceDeadlineResolver) ResolveProof(ctx context.Context, _, _ string) (resources.Proof, error) {
	f.deadline, _ = ctx.Deadline()
	return resources.Proof{}, context.DeadlineExceeded
}
func TestResourcePreservesShorterCallerDeadlineAndSanitizesErrors(t *testing.T) {
	resolver := &resourceDeadlineResolver{}
	op := resourceOperations{reader: &observedReader{}, resolver: resolver, principal: "user"}
	deadline := time.Now().Add(20 * time.Millisecond)
	ctx, cancel := context.WithDeadline(context.Background(), deadline)
	defer cancel()
	_, e := op.execute(ctx, "/v1/deployments", url.Values{"envCode": {"dev"}, "kind": {"application"}, "code": {"app"}})
	if e == nil || e.Error() != "ProviderUnavailable" || resolver.deadline.After(deadline) {
		t.Fatal(e, resolver.deadline, deadline)
	}
}
