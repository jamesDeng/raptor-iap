package provider

import (
	"context"
	ess "github.com/alibabacloud-go/ess-20220222/v2/client"
	nlb "github.com/alibabacloud-go/nlb-20220430/v4/client"
	"github.com/alibabacloud-go/tea/dara"
	"github.com/alibabacloud-go/tea/tea"
	"raptor-iap/infra-api/internal/domain"
	"testing"
)

func proxyFixture(t *testing.T) (*ProxyBackend, domain.Environment, *ess.DescribeScalingGroupsResponseBodyScalingGroups, *ess.DescribeScalingInstancesResponseBody, *nlb.ListServerGroupServersResponseBody, *nlb.GetListenerHealthStatusResponseBody) {
	t.Helper()
	env := domain.Environment{Code: "dev", AccountID: "123", Region: "ap-southeast-1", Proxies: []domain.ProxyMapping{{Code: "proxy", GroupID: "asg-test", ServerGroupID: "sgp-test", ListenerID: "lsn-test@6432", Port: 6432, TargetDBCode: "db"}}}
	g := &ess.DescribeScalingGroupsResponseBodyScalingGroups{ScalingGroupId: tea.String("asg-test"), LifecycleState: tea.String("Active"), EnableDesiredCapacity: tea.Bool(true), DesiredCapacity: tea.Int32(2), MinSize: tea.Int32(1), MaxSize: tea.Int32(4), Tags: []*ess.DescribeScalingGroupsResponseBodyScalingGroupsTags{{TagKey: tea.String("env"), TagValue: tea.String("dev")}, {TagKey: tea.String("db-proxy-code"), TagValue: tea.String("proxy")}, {TagKey: tea.String("target-db-code"), TagValue: tea.String("db")}}, ServerGroups: []*ess.DescribeScalingGroupsResponseBodyScalingGroupsServerGroups{{ServerGroupId: tea.String("sgp-test"), Port: tea.Int32(6432), Type: tea.String("NLB")}}}
	members := &ess.DescribeScalingInstancesResponseBody{TotalCount: tea.Int32(2), PageNumber: tea.Int32(1), PageSize: tea.Int32(50), ScalingInstances: []*ess.DescribeScalingInstancesResponseBodyScalingInstances{{InstanceId: tea.String("i-one"), ScalingGroupId: tea.String("asg-test"), LifecycleState: tea.String("Protected"), HealthStatus: tea.String("Healthy")}, {InstanceId: tea.String("i-two"), ScalingGroupId: tea.String("asg-test"), LifecycleState: tea.String("InService"), HealthStatus: tea.String("Healthy")}}}
	servers := &nlb.ListServerGroupServersResponseBody{TotalCount: tea.Int32(2), Servers: []*nlb.ListServerGroupServersResponseBodyServers{{ServerId: tea.String("i-one"), ServerGroupId: tea.String("sgp-test"), ServerType: tea.String("Ecs"), Port: tea.Int32(6432), Status: tea.String("Available"), Weight: tea.Int32(100)}, {ServerId: tea.String("i-two"), ServerGroupId: tea.String("sgp-test"), ServerType: tea.String("Ecs"), Port: tea.Int32(6432), Status: tea.String("Available"), Weight: tea.Int32(100)}}}
	health := &nlb.GetListenerHealthStatusResponseBody{ListenerHealthStatus: []*nlb.GetListenerHealthStatusResponseBodyListenerHealthStatus{{ListenerId: tea.String("lsn-test@6432"), ListenerPort: tea.Int32(6432), ServerGroupInfos: []*nlb.GetListenerHealthStatusResponseBodyListenerHealthStatusServerGroupInfos{{ServerGroupId: tea.String("sgp-test"), HeathCheckEnabled: tea.Bool(true), NonNormalServers: []*nlb.GetListenerHealthStatusResponseBodyListenerHealthStatusServerGroupInfosNonNormalServers{}}}}}}
	p := &ProxyBackend{identity: func(context.Context, domain.Environment) (string, error) { return "123", nil }}
	p.sdk.groups = func(_ context.Context, r *ess.DescribeScalingGroupsRequest, _ *dara.RuntimeOptions) (*ess.DescribeScalingGroupsResponse, error) {
		if tea.StringValue(r.RegionId) != "ap-southeast-1" || len(r.Tags) != 2 {
			t.Fatal("unscoped group read")
		}
		return &ess.DescribeScalingGroupsResponse{Body: &ess.DescribeScalingGroupsResponseBody{TotalCount: tea.Int32(1), PageNumber: r.PageNumber, PageSize: r.PageSize, ScalingGroups: []*ess.DescribeScalingGroupsResponseBodyScalingGroups{g}}}, nil
	}
	p.sdk.instances = func(_ context.Context, r *ess.DescribeScalingInstancesRequest, _ *dara.RuntimeOptions) (*ess.DescribeScalingInstancesResponse, error) {
		if tea.StringValue(r.ScalingGroupId) != "asg-test" {
			t.Fatal("foreign group")
		}
		return &ess.DescribeScalingInstancesResponse{Body: members}, nil
	}
	p.sdk.servers = func(_ context.Context, r *nlb.ListServerGroupServersRequest, _ *dara.RuntimeOptions) (*nlb.ListServerGroupServersResponse, error) {
		if tea.StringValue(r.ServerGroupId) != "sgp-test" {
			t.Fatal("foreign backend")
		}
		return &nlb.ListServerGroupServersResponse{Body: servers}, nil
	}
	p.sdk.health = func(_ context.Context, r *nlb.GetListenerHealthStatusRequest, _ *dara.RuntimeOptions) (*nlb.GetListenerHealthStatusResponse, error) {
		if tea.StringValue(r.ListenerId) != "lsn-test@6432" {
			t.Fatal("foreign listener")
		}
		return &nlb.GetListenerHealthStatusResponse{Body: health}, nil
	}
	p.sdk.listener = func(_ context.Context, r *nlb.GetListenerAttributeRequest, _ *dara.RuntimeOptions) (*nlb.GetListenerAttributeResponse, error) {
		return &nlb.GetListenerAttributeResponse{Body: &nlb.GetListenerAttributeResponseBody{ListenerId: r.ListenerId, ListenerPort: tea.Int32(6432), ListenerStatus: tea.String("Running"), ServerGroupId: tea.String("sgp-test")}}, nil
	}
	return p, env, g, members, servers, health
}
func TestProxySnapshotKeepsAllMembersAndProtection(t *testing.T) {
	p, env, _, _, servers, _ := proxyFixture(t)
	// Deregistered member remains in ESS inventory, rather than disappearing.
	servers.Servers = servers.Servers[:1]
	servers.TotalCount = tea.Int32(1)
	rows, e := p.Snapshot(context.Background(), env, "proxy")
	if e != nil {
		t.Fatal(e)
	}
	if len(rows) != 1 || len(rows[0].Nodes) != 2 || rows[0].Desired != 2 {
		t.Fatalf("incomplete snapshot: %+v", rows)
	}
	nodes := rows[0].Nodes
	if nodes[0].ID != "i-one" || !nodes[0].Protected || !nodes[0].Registered || !nodes[0].Healthy || nodes[1].ID != "i-two" || nodes[1].Protected || nodes[1].Registered || nodes[1].Healthy {
		t.Fatalf("wrong node state: %+v", nodes)
	}
}
func TestProxySnapshotRefusesIncompleteOrForeignState(t *testing.T) {
	cases := []string{"account", "region", "tag", "group", "capacity", "desired disabled", "membership truncated", "duplicate member", "foreign member", "missing protection", "pending", "missing health", "duplicate backend", "foreign backend", "wrong port", "removing backend", "missing backend count", "health disabled", "missing health list", "health truncated", "foreign unhealthy", "duplicate tag"}
	for _, name := range cases {
		t.Run(name, func(t *testing.T) {
			p, env, g, m, s, h := proxyFixture(t)
			switch name {
			case "account":
				env.AccountID = "other"
			case "region":
				env.Region = "cn-hangzhou"
			case "tag":
				g.Tags[2].TagValue = tea.String("other-db")
			case "group":
				g.ScalingGroupId = tea.String("asg-other")
			case "capacity":
				g.MaxSize = nil
			case "desired disabled":
				g.EnableDesiredCapacity = tea.Bool(false)
			case "membership truncated":
				m.TotalCount = tea.Int32(3)
			case "duplicate member":
				m.ScalingInstances[1] = m.ScalingInstances[0]
			case "foreign member":
				m.ScalingInstances[1].ScalingGroupId = tea.String("asg-other")
			case "missing protection":
				m.ScalingInstances[1].LifecycleState = nil
			case "pending":
				m.ScalingInstances[1].LifecycleState = tea.String("Pending")
			case "missing health":
				m.ScalingInstances[1].HealthStatus = nil
			case "duplicate backend":
				s.Servers[1] = s.Servers[0]
			case "foreign backend":
				s.Servers[1].ServerId = tea.String("i-foreign")
			case "wrong port":
				s.Servers[1].Port = tea.Int32(123)
			case "removing backend":
				s.Servers[1].Status = tea.String("Removing")
			case "missing backend count":
				s.TotalCount = nil
			case "health disabled":
				h.ListenerHealthStatus[0].ServerGroupInfos[0].HeathCheckEnabled = tea.Bool(false)
			case "missing health list":
				h.ListenerHealthStatus[0].ServerGroupInfos[0].NonNormalServers = nil
			case "health truncated":
				h.NextToken = tea.String("more")
			case "foreign unhealthy":
				h.ListenerHealthStatus[0].ServerGroupInfos[0].NonNormalServers = []*nlb.GetListenerHealthStatusResponseBodyListenerHealthStatusServerGroupInfosNonNormalServers{{ServerId: tea.String("foreign"), Port: tea.Int32(6432), Status: tea.String("Unhealthy")}}
			case "duplicate tag":
				g.Tags = append(g.Tags, g.Tags[0])
			}
			if _, e := p.Snapshot(context.Background(), env, "proxy"); e == nil {
				t.Fatal("accepted unsafe provider snapshot")
			}
		})
	}
}

func TestProxySnapshotCompletesBothPaginationStyles(t *testing.T) {
	p, env, _, members, servers, _ := proxyFixture(t)
	allMembers, allServers := members.ScalingInstances, servers.Servers
	p.sdk.instances = func(_ context.Context, r *ess.DescribeScalingInstancesRequest, _ *dara.RuntimeOptions) (*ess.DescribeScalingInstancesResponse, error) {
		page := tea.Int32Value(r.PageNumber)
		if page < 1 || page > 2 {
			t.Fatalf("unexpected page %d", page)
		}
		return &ess.DescribeScalingInstancesResponse{Body: &ess.DescribeScalingInstancesResponseBody{TotalCount: tea.Int32(2), PageNumber: r.PageNumber, PageSize: r.PageSize, ScalingInstances: allMembers[page-1 : page]}}, nil
	}
	p.sdk.servers = func(_ context.Context, r *nlb.ListServerGroupServersRequest, _ *dara.RuntimeOptions) (*nlb.ListServerGroupServersResponse, error) {
		token := tea.StringValue(r.NextToken)
		index := 0
		next := "page-two"
		if token == "page-two" {
			index = 1
			next = ""
		} else if token != "" {
			t.Fatal("wrong pagination token")
		}
		return &nlb.ListServerGroupServersResponse{Body: &nlb.ListServerGroupServersResponseBody{TotalCount: tea.Int32(2), NextToken: tea.String(next), Servers: allServers[index : index+1]}}, nil
	}
	rows, e := p.Snapshot(context.Background(), env, "proxy")
	if e != nil {
		t.Fatal(e)
	}
	if len(rows) != 1 || len(rows[0].Nodes) != 2 || !rows[0].Nodes[1].Registered {
		t.Fatalf("lost second page: %+v", rows)
	}
}
func TestProxySnapshotUsesActualUnhealthyAndWeight(t *testing.T) {
	for _, kind := range []string{"nlb unhealthy", "zero weight", "ess unhealthy"} {
		t.Run(kind, func(t *testing.T) {
			p, env, _, m, s, h := proxyFixture(t)
			switch kind {
			case "nlb unhealthy":
				h.ListenerHealthStatus[0].ServerGroupInfos[0].NonNormalServers = []*nlb.GetListenerHealthStatusResponseBodyListenerHealthStatusServerGroupInfosNonNormalServers{{ServerId: tea.String("i-two"), Port: tea.Int32(6432), Status: tea.String("Unhealthy")}}
			case "zero weight":
				s.Servers[1].Weight = tea.Int32(0)
			case "ess unhealthy":
				m.ScalingInstances[1].HealthStatus = tea.String("Unhealthy")
			}
			rows, e := p.Snapshot(context.Background(), env, "proxy")
			if e != nil {
				t.Fatal(e)
			}
			if !rows[0].Nodes[1].Registered || rows[0].Nodes[1].Healthy {
				t.Fatal("unhealthy node counted as healthy registered capacity")
			}
		})
	}
}
func TestProxySnapshotRejectsZeroAndMultipleGroups(t *testing.T) {
	for _, n := range []int{0, 2} {
		p, env, g, _, _, _ := proxyFixture(t)
		rows := []*ess.DescribeScalingGroupsResponseBodyScalingGroups{}
		for i := 0; i < n; i++ {
			rows = append(rows, g)
		}
		p.sdk.groups = func(_ context.Context, r *ess.DescribeScalingGroupsRequest, _ *dara.RuntimeOptions) (*ess.DescribeScalingGroupsResponse, error) {
			return &ess.DescribeScalingGroupsResponse{Body: &ess.DescribeScalingGroupsResponseBody{TotalCount: tea.Int32(int32(n)), PageNumber: r.PageNumber, ScalingGroups: rows}}, nil
		}
		if _, e := p.Snapshot(context.Background(), env, "proxy"); e == nil {
			t.Fatalf("accepted %d groups", n)
		}
	}
}
