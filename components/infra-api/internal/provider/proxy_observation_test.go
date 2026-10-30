package provider

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"testing"

	ess "github.com/alibabacloud-go/ess-20220222/v2/client"
	nlb "github.com/alibabacloud-go/nlb-20220430/v4/client"
	"github.com/alibabacloud-go/tea/dara"
	"github.com/alibabacloud-go/tea/tea"
	"raptor-iap/infra-api/internal/api"
	"raptor-iap/infra-api/internal/domain"
)

var _ api.ProxyObserver = (*reader)(nil)

func observationFixture(t *testing.T) (*reader, domain.Environment, domain.ProxyReadTarget, *ess.DescribeScalingGroupsResponseBodyScalingGroups, *ess.DescribeScalingInstancesResponseBody, *nlb.ListServerGroupServersResponseBody, *nlb.GetListenerHealthStatusResponseBody) {
	t.Helper()
	backend, env, g, m, s, h := proxyFixture(t)
	s.Servers[0].ServerIp = tea.String("10.70.1.30")
	s.Servers[1].ServerIp = tea.String("10.70.1.31")
	env.ClusterID = "cluster"
	return &reader{proxy: backend}, env, domain.ProxyReadTarget{RequestID: "req", EnvCode: "dev", ProxyCode: "proxy", GroupID: "asg-test", ServerGroupID: "sgp-test"}, g, m, s, h
}

func TestProxyObservationShapesAndPendingCounts(t *testing.T) {
	p, env, target, g, m, _, _ := observationFixture(t)
	g.DesiredCapacity = tea.Int32(4)
	m.ScalingInstances[1].LifecycleState = tea.String("Pending:Wait")
	for _, kind := range []string{"fleet", "capacity", "backend"} {
		got, err := p.ObserveProxy(context.Background(), env, target, kind)
		if err != nil {
			t.Fatal(err)
		}
		branches := 0
		if got.Fleet != nil {
			branches++
			if kind != "fleet" || got.Fleet.GroupID != "asg-test" || len(got.Fleet.Instances) != 2 || !got.Fleet.Instances[0].ProtectedFromScaleIn || got.Fleet.Instances[1].LifecycleState != "Pending:Wait" || got.Fleet.ObservedAt.IsZero() {
				t.Fatalf("bad fleet %+v", got.Fleet)
			}
		}
		if got.Capacity != nil {
			branches++
			if kind != "capacity" || got.Capacity.DesiredCapacity != 4 || got.Capacity.MinSize != 1 || got.Capacity.MaxSize != 4 || got.Capacity.ObservedAt.IsZero() {
				t.Fatalf("bad capacity %+v", got.Capacity)
			}
		}
		if got.Backend != nil {
			branches++
			if kind != "backend" || got.Backend.ListenerID != "lsn-test@6432" || got.Backend.ServerGroupID != "sgp-test" || len(got.Backend.Nodes) != 2 || got.Backend.Nodes[1].Healthy || !got.Backend.Nodes[1].Registered || got.Backend.Nodes[1].PrivateIP != "10.70.1.31" || got.Backend.ObservedAt.IsZero() {
				t.Fatalf("bad backend %+v", got.Backend)
			}
		}
		if branches != 1 {
			t.Fatalf("expected exactly one branch: %+v", got)
		}
	}
	if _, err := p.proxy.Snapshot(context.Background(), env, "proxy"); err == nil {
		t.Fatal("observation weakened strict snapshot")
	}
	raw, _ := json.Marshal(target)
	if string(raw) != `{"requestId":"req","envCode":"dev","proxyCode":"proxy","groupId":"asg-test","serverGroupId":"sgp-test"}` {
		t.Fatal(string(raw))
	}
}

func TestProxyObservationLifecycleAndBackendStates(t *testing.T) {
	for _, life := range []string{"InService", "Protected", "Pending", "Pending:Wait", "Standby", "Stopped", "Removing", "Removing:Wait"} {
		for _, status := range []string{"Available", "Adding", "Configuring", "Removing"} {
			t.Run(life+"/"+status, func(t *testing.T) {
				p, env, target, _, m, s, _ := observationFixture(t)
				m.ScalingInstances[1].LifecycleState = tea.String(life)
				s.Servers[1].Status = tea.String(status)
				fleet, err := p.ObserveProxy(context.Background(), env, target, "fleet")
				if err != nil {
					t.Fatal(err)
				}
				if fleet.Fleet.Instances[1].LifecycleState != life || fleet.Fleet.Instances[1].ProtectedFromScaleIn != (life == "Protected") {
					t.Fatal(fleet)
				}
				got, err := p.ObserveProxy(context.Background(), env, target, "backend")
				if err != nil {
					t.Fatal(err)
				}
				want := (life == "InService" || life == "Protected") && status == "Available"
				if got.Backend.Nodes[1].Healthy != want || got.Backend.Nodes[1].Status != status {
					t.Fatal(got.Backend.Nodes[1])
				}
			})
		}
	}
	for _, kind := range []string{"zero weight", "ess unhealthy", "nlb unhealthy"} {
		p, env, target, _, m, s, h := observationFixture(t)
		switch kind {
		case "zero weight":
			s.Servers[1].Weight = tea.Int32(0)
		case "ess unhealthy":
			m.ScalingInstances[1].HealthStatus = tea.String("Unhealthy")
		case "nlb unhealthy":
			h.ListenerHealthStatus[0].ServerGroupInfos[0].NonNormalServers = []*nlb.GetListenerHealthStatusResponseBodyListenerHealthStatusServerGroupInfosNonNormalServers{{ServerId: tea.String("i-two"), Port: tea.Int32(6432), Status: tea.String("Initial")}}
		}
		got, err := p.ObserveProxy(context.Background(), env, target, "backend")
		if err != nil {
			t.Fatal(err)
		}
		if got.Backend.Nodes[1].Healthy || got.Backend.Nodes[1].Registered != (kind != "zero weight") {
			t.Fatal(kind, got.Backend.Nodes[1])
		}
	}
}

func TestProxyObservationRefusesInvalidState(t *testing.T) {
	for _, name := range []string{"account", "zero mapping", "duplicate mapping", "foreign env", "foreign group", "foreign backend target", "tag", "database tag", "capacity", "desired disabled", "group association", "member duplicate", "member foreign", "missing lifecycle", "unknown lifecycle", "missing health", "unknown health", "member truncated", "server duplicate", "server foreign", "server group", "server type", "server port", "server status", "missing weight", "weight range", "missing ip", "public ip", "invalid ip", "mapped ipv6", "server truncated", "health truncated", "health disabled", "health missing", "health foreign", "unknown kind"} {
		t.Run(name, func(t *testing.T) {
			p, env, target, g, m, s, h := observationFixture(t)
			kind := "backend"
			switch name {
			case "account":
				env.AccountID = "foreign"
			case "zero mapping":
				env.Proxies = nil
			case "duplicate mapping":
				env.Proxies = append(env.Proxies, env.Proxies[0])
			case "foreign env":
				target.EnvCode = "foreign"
			case "foreign group":
				target.GroupID = "foreign"
			case "foreign backend target":
				target.ServerGroupID = "foreign"
			case "tag":
				g.Tags[0].TagValue = tea.String("foreign")
			case "database tag":
				g.Tags[2].TagValue = tea.String("foreign")
			case "capacity":
				g.MaxSize = nil
			case "desired disabled":
				g.EnableDesiredCapacity = tea.Bool(false)
			case "group association":
				g.ServerGroups[0].ServerGroupId = tea.String("foreign")
			case "member duplicate":
				m.ScalingInstances[1] = m.ScalingInstances[0]
			case "member foreign":
				m.ScalingInstances[1].ScalingGroupId = tea.String("foreign")
			case "missing lifecycle":
				m.ScalingInstances[1].LifecycleState = nil
			case "unknown lifecycle":
				m.ScalingInstances[1].LifecycleState = tea.String("Unknown")
			case "missing health":
				m.ScalingInstances[1].HealthStatus = nil
			case "unknown health":
				m.ScalingInstances[1].HealthStatus = tea.String("Unknown")
			case "member truncated":
				m.TotalCount = tea.Int32(3)
			case "server duplicate":
				s.Servers[1] = s.Servers[0]
			case "server foreign":
				s.Servers[1].ServerId = tea.String("foreign")
			case "server group":
				s.Servers[1].ServerGroupId = tea.String("foreign")
			case "server type":
				s.Servers[1].ServerType = tea.String("Ip")
			case "server port":
				s.Servers[1].Port = tea.Int32(123)
			case "server status":
				s.Servers[1].Status = tea.String("Unknown")
			case "missing weight":
				s.Servers[1].Weight = nil
			case "weight range":
				s.Servers[1].Weight = tea.Int32(101)
			case "missing ip":
				s.Servers[1].ServerIp = nil
			case "public ip":
				s.Servers[1].ServerIp = tea.String("8.8.8.8")
			case "invalid ip":
				s.Servers[1].ServerIp = tea.String("10.1.1.1:6432")
			case "mapped ipv6":
				s.Servers[1].ServerIp = tea.String("::ffff:10.70.1.31")
			case "server truncated":
				s.TotalCount = tea.Int32(3)
			case "health truncated":
				h.NextToken = tea.String("more")
			case "health disabled":
				h.ListenerHealthStatus[0].ServerGroupInfos[0].HeathCheckEnabled = tea.Bool(false)
			case "health missing":
				h.ListenerHealthStatus[0].ServerGroupInfos[0].NonNormalServers = nil
			case "health foreign":
				h.ListenerHealthStatus[0].ServerGroupInfos[0].NonNormalServers = []*nlb.GetListenerHealthStatusResponseBodyListenerHealthStatusServerGroupInfosNonNormalServers{{ServerId: tea.String("foreign"), Port: tea.Int32(6432), Status: tea.String("Unhealthy")}}
			case "unknown kind":
				kind = "arbitrary"
			}
			if _, err := p.ObserveProxy(context.Background(), env, target, kind); err == nil {
				t.Fatal("accepted invalid observation")
			}
		})
	}
}

func TestProxyObservationPaginationAndEmptyArrays(t *testing.T) {
	p, env, target, _, m, s, _ := observationFixture(t)
	p.proxy.sdk.instances = func(_ context.Context, r *ess.DescribeScalingInstancesRequest, _ *dara.RuntimeOptions) (*ess.DescribeScalingInstancesResponse, error) {
		n := int(tea.Int32Value(r.PageNumber))
		if n < 1 || n > 2 {
			t.Fatal("unexpected member page")
		}
		return &ess.DescribeScalingInstancesResponse{Body: &ess.DescribeScalingInstancesResponseBody{TotalCount: tea.Int32(2), PageNumber: r.PageNumber, ScalingInstances: m.ScalingInstances[n-1 : n]}}, nil
	}
	p.proxy.sdk.servers = func(_ context.Context, r *nlb.ListServerGroupServersRequest, _ *dara.RuntimeOptions) (*nlb.ListServerGroupServersResponse, error) {
		n, next := 0, "second"
		if tea.StringValue(r.NextToken) == "second" {
			n, next = 1, ""
		} else if tea.StringValue(r.NextToken) != "" {
			t.Fatal("unexpected token")
		}
		return &nlb.ListServerGroupServersResponse{Body: &nlb.ListServerGroupServersResponseBody{TotalCount: tea.Int32(2), NextToken: tea.String(next), Servers: s.Servers[n : n+1]}}, nil
	}
	got, err := p.ObserveProxy(context.Background(), env, target, "backend")
	if err != nil || len(got.Backend.Nodes) != 2 || !got.Backend.Nodes[1].Healthy {
		t.Fatal(got, err)
	}
	for _, mode := range []string{"empty", "repeated token", "changing total", "page mismatch", "zero groups", "duplicate groups", "foreign groups"} {
		t.Run(mode, func(t *testing.T) {
			p, env, target, g, m, s, _ := observationFixture(t)
			kind := "backend"
			switch mode {
			case "empty":
				m.TotalCount = tea.Int32(0)
				m.ScalingInstances = nil
				s.TotalCount = tea.Int32(0)
				s.Servers = nil
			case "repeated token":
				s.NextToken = tea.String("repeat")
				s.TotalCount = tea.Int32(3)
				s.Servers = s.Servers[:1]
			case "changing total":
				p.proxy.sdk.instances = func(_ context.Context, r *ess.DescribeScalingInstancesRequest, _ *dara.RuntimeOptions) (*ess.DescribeScalingInstancesResponse, error) {
					return &ess.DescribeScalingInstancesResponse{Body: &ess.DescribeScalingInstancesResponseBody{TotalCount: tea.Int32(2 + tea.Int32Value(r.PageNumber)), PageNumber: r.PageNumber, ScalingInstances: m.ScalingInstances[:1]}}, nil
				}
			case "page mismatch":
				m.PageNumber = tea.Int32(2)
			default:
				p.proxy.sdk.groups = func(_ context.Context, r *ess.DescribeScalingGroupsRequest, _ *dara.RuntimeOptions) (*ess.DescribeScalingGroupsResponse, error) {
					rows := []*ess.DescribeScalingGroupsResponseBodyScalingGroups{}
					if mode == "duplicate groups" {
						rows = append(rows, g, g)
					}
					if mode == "foreign groups" {
						g.ScalingGroupId = tea.String("foreign")
						rows = append(rows, g)
					}
					return &ess.DescribeScalingGroupsResponse{Body: &ess.DescribeScalingGroupsResponseBody{TotalCount: tea.Int32(int32(len(rows))), PageNumber: r.PageNumber, ScalingGroups: rows}}, nil
				}
			}
			got, err := p.ObserveProxy(context.Background(), env, target, kind)
			if mode != "empty" {
				if err == nil {
					t.Fatal("accepted incomplete pages")
				}
				return
			}
			if err != nil || got.Backend.Nodes == nil || len(got.Backend.Nodes) != 0 {
				t.Fatal(got, err)
			}
			fleet, err := p.ObserveProxy(context.Background(), env, target, "fleet")
			if err != nil || fleet.Fleet.Instances == nil {
				t.Fatal(fleet, err)
			}
			raw, _ := json.Marshal(fleet.Fleet)
			if !strings.Contains(string(raw), `"instances":[]`) {
				t.Fatal(string(raw))
			}
		})
	}
}

func TestProxyObservationSanitizesProviderErrors(t *testing.T) {
	p, env, target, _, _, _, _ := observationFixture(t)
	p.proxy.sdk.instances = func(context.Context, *ess.DescribeScalingInstancesRequest, *dara.RuntimeOptions) (*ess.DescribeScalingInstancesResponse, error) {
		return nil, errors.New("private credential details")
	}
	_, err := p.ObserveProxy(context.Background(), env, target, "fleet")
	if err == nil || strings.Contains(err.Error(), "private") {
		t.Fatal(err)
	}
}

func TestProxyObservationRefusesBrokenPagination(t *testing.T) {
	for _, kind := range []string{"fleet", "backend"} {
		for _, mode := range []string{"missing total", "oversized total", "empty page", "nil response", "oversized page", "exhausted"} {
			t.Run(kind+"/"+mode, func(t *testing.T) {
				p, env, target, _, m, s, _ := observationFixture(t)
				if kind == "fleet" {
					switch mode {
					case "missing total":
						m.TotalCount = nil
					case "oversized total":
						m.TotalCount = tea.Int32(5001)
					case "empty page":
						m.ScalingInstances = nil
					case "nil response":
						p.proxy.sdk.instances = func(context.Context, *ess.DescribeScalingInstancesRequest, *dara.RuntimeOptions) (*ess.DescribeScalingInstancesResponse, error) {
							return nil, nil
						}
					case "oversized page":
						for len(m.ScalingInstances) < 51 {
							m.ScalingInstances = append(m.ScalingInstances, m.ScalingInstances[0])
						}
					case "exhausted":
						p.proxy.sdk.instances = func(_ context.Context, r *ess.DescribeScalingInstancesRequest, _ *dara.RuntimeOptions) (*ess.DescribeScalingInstancesResponse, error) {
							row := *m.ScalingInstances[0]
							row.InstanceId = tea.String("i-" + strconv.Itoa(int(tea.Int32Value(r.PageNumber))))
							return &ess.DescribeScalingInstancesResponse{Body: &ess.DescribeScalingInstancesResponseBody{TotalCount: tea.Int32(101), PageNumber: r.PageNumber, ScalingInstances: []*ess.DescribeScalingInstancesResponseBodyScalingInstances{&row}}}, nil
						}
					}
				} else {
					switch mode {
					case "missing total":
						s.TotalCount = nil
					case "oversized total":
						s.TotalCount = tea.Int32(10001)
					case "empty page":
						s.Servers = nil
						s.NextToken = tea.String("more")
					case "nil response":
						p.proxy.sdk.servers = func(context.Context, *nlb.ListServerGroupServersRequest, *dara.RuntimeOptions) (*nlb.ListServerGroupServersResponse, error) {
							return nil, nil
						}
					case "oversized page":
						for len(s.Servers) < 101 {
							s.Servers = append(s.Servers, s.Servers[0])
						}
					case "exhausted":
						n := 0
						p.proxy.sdk.servers = func(context.Context, *nlb.ListServerGroupServersRequest, *dara.RuntimeOptions) (*nlb.ListServerGroupServersResponse, error) {
							n++
							row := *s.Servers[0]
							row.ServerId = tea.String("i-" + strconv.Itoa(n))
							return &nlb.ListServerGroupServersResponse{Body: &nlb.ListServerGroupServersResponseBody{TotalCount: tea.Int32(101), NextToken: tea.String("page-" + strconv.Itoa(n)), Servers: []*nlb.ListServerGroupServersResponseBodyServers{&row}}}, nil
						}
					}
				}
				if _, err := p.ObserveProxy(context.Background(), env, target, kind); err == nil {
					t.Fatal("accepted incomplete page sequence")
				}
			})
		}
	}
}

func TestProxyGroupValidationPreservesRefusalCategories(t *testing.T) {
	for _, name := range []string{"foreign group", "inactive", "desired disabled", "missing desired", "missing minimum", "missing maximum", "minimum negative", "maximum below minimum", "desired below minimum", "desired above maximum", "missing association", "nil association", "foreign association", "wrong association type", "missing association port", "wrong association port"} {
		t.Run(name, func(t *testing.T) {
			p, env, target, g, _, _, _ := observationFixture(t)
			scopeRefusal := false
			switch name {
			case "foreign group":
				g.ScalingGroupId = tea.String("foreign")
				scopeRefusal = true
			case "inactive":
				g.LifecycleState = tea.String("Inactive")
			case "desired disabled":
				g.EnableDesiredCapacity = tea.Bool(false)
			case "missing desired":
				g.DesiredCapacity = nil
			case "missing minimum":
				g.MinSize = nil
			case "missing maximum":
				g.MaxSize = nil
			case "minimum negative":
				g.MinSize = tea.Int32(-1)
			case "maximum below minimum":
				g.MaxSize = tea.Int32(0)
			case "desired below minimum":
				g.DesiredCapacity = tea.Int32(0)
			case "desired above maximum":
				g.DesiredCapacity = tea.Int32(5)
			case "missing association":
				g.ServerGroups = nil
			case "nil association":
				g.ServerGroups[0] = nil
			case "foreign association":
				g.ServerGroups[0].ServerGroupId = tea.String("foreign")
				scopeRefusal = true
			case "wrong association type":
				g.ServerGroups[0].Type = tea.String("ALB")
				scopeRefusal = true
			case "missing association port":
				g.ServerGroups[0].Port = nil
				scopeRefusal = true
			case "wrong association port":
				g.ServerGroups[0].Port = tea.Int32(123)
				scopeRefusal = true
			}
			_, observationErr := p.ObserveProxy(context.Background(), env, target, "capacity")
			_, snapshotErr := p.proxy.Snapshot(context.Background(), env, "proxy")
			for _, err := range []error{observationErr, snapshotErr} {
				if scopeRefusal {
					if !errors.Is(err, domain.ErrScope) {
						t.Fatalf("expected scope refusal, got %v", err)
					}
					continue
				}
				var refusal *domain.CommandError
				if !errors.As(err, &refusal) || refusal.Code != "ProviderStateInvalid" {
					t.Fatalf("expected invalid provider state, got %v", err)
				}
			}
		})
	}
}
