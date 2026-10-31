package provider

import (
	"context"
	ess "github.com/alibabacloud-go/ess-20220222/v2/client"
	nlb "github.com/alibabacloud-go/nlb-20220430/v4/client"
	"github.com/alibabacloud-go/tea/dara"
	"github.com/alibabacloud-go/tea/tea"
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
	"raptor-iap/infra-api/internal/domain"
	"strings"
	"testing"
)

func resourceFixture(t *testing.T) (*reader, domain.ResourceContext, *ess.DescribeScalingGroupsResponseBodyScalingGroups, *nlb.ListServerGroupsResponseBody, *nlb.ListListenersResponseBody) {
	p, env, g, _, _, _ := proxyFixture(t)
	c := domain.ResourceContext{EnvCode: env.Code, Kind: "db-proxy", Code: "proxy", Cloud: "aliyun", Region: env.Region, AccountID: env.AccountID, ClusterID: "ack", RelatedResources: []domain.ResourceRef{}}
	sg := &nlb.ListServerGroupsResponseBody{TotalCount: tea.Int32(1), ServerGroups: []*nlb.ListServerGroupsResponseBodyServerGroups{{ServerGroupId: tea.String("sgp-test"), RelatedLoadBalancerIds: []*string{tea.String("nlb-test")}}}}
	ls := &nlb.ListListenersResponseBody{TotalCount: tea.Int32(1), Listeners: []*nlb.ListListenersResponseBodyListeners{{ListenerId: tea.String("lsn-test@6432"), ServerGroupId: tea.String("sgp-test"), LoadBalancerId: tea.String("nlb-test"), ListenerPort: tea.Int32(6432)}}}
	p.sdk.serverGroups = func(_ context.Context, r *nlb.ListServerGroupsRequest, _ *dara.RuntimeOptions) (*nlb.ListServerGroupsResponse, error) {
		if len(r.ServerGroupIds) != 1 || tea.StringValue(r.ServerGroupIds[0]) != "sgp-test" || tea.StringValue(r.RegionId) != c.Region {
			t.Fatal("unscoped NLB group enumeration")
		}
		return &nlb.ListServerGroupsResponse{Body: sg}, nil
	}
	p.sdk.listeners = func(_ context.Context, r *nlb.ListListenersRequest, _ *dara.RuntimeOptions) (*nlb.ListListenersResponse, error) {
		if len(r.LoadBalancerIds) != 1 || tea.StringValue(r.LoadBalancerIds[0]) != "nlb-test" {
			t.Fatal("unscoped NLB listener enumeration")
		}
		return &nlb.ListListenersResponse{Body: ls}, nil
	}
	prior := p.sdk.listener
	p.sdk.listener = func(c context.Context, q *nlb.GetListenerAttributeRequest, o *dara.RuntimeOptions) (*nlb.GetListenerAttributeResponse, error) {
		v, e := prior(c, q, o)
		v.Body.LoadBalancerId = tea.String("nlb-test")
		return v, e
	}
	r := &reader{identity: p.identity, proxy: p, evidence: "fixture"}
	r.page = func(_ context.Context, e domain.Environment, k, code string, n int) (page, error) {
		if n != 1 || e.Code != "dev" || k != "db-proxy" || code != "proxy" {
			t.Fatal("unscoped ESS discovery")
		}
		return page{Total: 1, Records: []record{{ID: "asg-test", State: "Active", Tags: map[string]string{"env": "dev", "db-proxy-code": "proxy", "target-db-code": "db"}}}}, nil
	}
	return r, c, g, sg, ls
}
func TestResourceProxyDiscoveryExactAssociation(t *testing.T) {
	r, c, _, _, _ := resourceFixture(t)
	got, e := r.ResolveResource(context.Background(), c)
	if e != nil || got.Proxy == nil || got.Proxy.ListenerID != "lsn-test@6432" || got.Proxy.TargetDBCode != "db" || got.PgproxyWorkload.Type != "ess" || len(got.Environment.Proxies) != 1 {
		t.Fatal(got, e)
	}
}
func TestResourceProxyDiscoveryFailsClosed(t *testing.T) {
	for _, mode := range []string{"account", "cloud", "region", "foreign-discovery-tag", "duplicate-group", "empty-discovery", "foreign-detail-tag", "foreign-detail-id", "duplicate-tag", "attachment", "port", "nlb-group", "nlb-duplicate", "nlb-truncated", "nlb-cycle", "foreign-load-balancer", "duplicate-listener", "listener-port", "listener-missing", "listener-truncated", "listener-cycle", "listener-attribute"} {
		t.Run(mode, func(t *testing.T) {
			r, c, g, sg, ls := resourceFixture(t)
			switch mode {
			case "account":
				r.identity = func(context.Context, domain.Environment) (string, error) { return "foreign", nil }
				r.page = func(context.Context, domain.Environment, string, string, int) (page, error) {
					t.Fatal("foreign account read cloud")
					return page{}, nil
				}
			case "cloud":
				c.Cloud = "aws"
			case "region":
				c.Region = "cn-hangzhou"
			case "foreign-discovery-tag":
				r.page = func(context.Context, domain.Environment, string, string, int) (page, error) {
					return page{Total: 1, Records: []record{{ID: "asg-test", Tags: map[string]string{"env": "foreign", "db-proxy-code": "proxy", "target-db-code": "db"}}}}, nil
				}
			case "duplicate-group":
				r.page = func(context.Context, domain.Environment, string, string, int) (page, error) {
					return page{Total: 2, Records: []record{{ID: "a", Tags: map[string]string{"env": "dev", "db-proxy-code": "proxy", "target-db-code": "db"}}, {ID: "b", Tags: map[string]string{"env": "dev", "db-proxy-code": "proxy", "target-db-code": "db"}}}}, nil
				}
			case "empty-discovery":
				r.page = func(context.Context, domain.Environment, string, string, int) (page, error) {
					return page{Total: 0}, nil
				}
			case "foreign-detail-tag":
				g.Tags[0].TagValue = tea.String("foreign")
			case "foreign-detail-id":
				g.ScalingGroupId = tea.String("foreign")
			case "duplicate-tag":
				g.Tags = append(g.Tags, g.Tags[0])
			case "attachment":
				g.ServerGroups = append(g.ServerGroups, g.ServerGroups[0])
			case "port":
				g.ServerGroups[0].Port = nil
			case "nlb-group":
				sg.ServerGroups[0].ServerGroupId = tea.String("foreign")
			case "nlb-duplicate":
				sg.ServerGroups = append(sg.ServerGroups, sg.ServerGroups[0])
				sg.TotalCount = tea.Int32(2)
			case "nlb-truncated":
				sg.TotalCount = tea.Int32(2)
			case "nlb-cycle":
				sg.NextToken = tea.String("repeat")
				sg.TotalCount = tea.Int32(2)
			case "foreign-load-balancer":
				ls.Listeners[0].LoadBalancerId = tea.String("foreign")
			case "duplicate-listener":
				ls.Listeners = append(ls.Listeners, ls.Listeners[0])
				ls.TotalCount = tea.Int32(2)
			case "listener-port":
				ls.Listeners[0].ListenerPort = tea.Int32(5432)
			case "listener-missing":
				ls.Listeners = nil
				ls.TotalCount = tea.Int32(0)
			case "listener-truncated":
				ls.TotalCount = tea.Int32(2)
			case "listener-cycle":
				ls.NextToken = tea.String("repeat")
				ls.TotalCount = tea.Int32(2)
			case "listener-attribute":
				r.proxy.sdk.listener = func(context.Context, *nlb.GetListenerAttributeRequest, *dara.RuntimeOptions) (*nlb.GetListenerAttributeResponse, error) {
					return &nlb.GetListenerAttributeResponse{Body: &nlb.GetListenerAttributeResponseBody{ListenerId: tea.String("foreign")}}, nil
				}
			}
			if _, e := r.ResolveResource(context.Background(), c); e == nil {
				t.Fatal("accepted unsafe resolution")
			}
		})
	}
}
func TestResourceApplicationTypedWorkloadsAndUIDAmbiguity(t *testing.T) {
	for _, mode := range []string{"multiple", "duplicate-uid", "missing-uid", "missing-cluster"} {
		t.Run(mode, func(t *testing.T) {
			r, c, _, _, _ := resourceFixture(t)
			c.Kind = "application"
			c.Code = "app"
			if mode == "missing-cluster" {
				c.ClusterID = ""
			}
			uid := "two"
			if mode == "duplicate-uid" {
				uid = "one"
			}
			if mode == "missing-uid" {
				uid = ""
			}
			k := fake.NewSimpleClientset(&appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "a", Namespace: "ns", UID: types.UID("one"), Labels: map[string]string{"raptor.appcode": "app"}}}, &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "b", Namespace: "ns", UID: types.UID(uid), Labels: map[string]string{"raptor.appcode": "app"}}})
			r.kube = func(context.Context, domain.Environment) (kubernetes.Interface, error) { return k, nil }
			got, e := r.ResolveResource(context.Background(), c)
			if mode != "multiple" {
				if e == nil {
					t.Fatal("accepted incomplete application identity")
				}
				return
			}
			if e != nil || len(got.ApplicationWorkloads) != 2 || got.ApplicationWorkloads[0].Type != "kubernetes" || got.ApplicationWorkloads[0].WorkloadKey == got.ApplicationWorkloads[1].WorkloadKey || strings.Contains(got.ApplicationWorkloads[0].WorkloadKey, "ns") {
				t.Fatal(got, e)
			}
		})
	}
}
func TestResourceDatabaseVerifiesEveryTag(t *testing.T) {
	for _, mode := range []string{"valid", "foreign", "duplicate"} {
		t.Run(mode, func(t *testing.T) {
			r, c, _, _, _ := resourceFixture(t)
			c.Kind = "database"
			c.Code = "db"
			r.page = func(context.Context, domain.Environment, string, string, int) (page, error) {
				tags := map[string]string{"env": "dev", "db-code": "db"}
				if mode == "foreign" {
					tags["env"] = "other"
				}
				rows := []record{{ID: "rm-real", Tags: tags}}
				if mode == "duplicate" {
					rows = append(rows, rows[0])
				}
				return page{Total: len(rows), Records: rows}, nil
			}
			got, e := r.ResolveResource(context.Background(), c)
			if mode != "valid" {
				if e == nil {
					t.Fatal("accepted foreign DB")
				}
				return
			}
			if e != nil || len(got.DatabaseWorkloads) != 1 || got.DatabaseWorkloads[0].Type != "rds" || got.DatabaseWorkloads[0].ResourceID != "rm-real" {
				t.Fatal(got, e)
			}
		})
	}
}

func TestResourceDiscoveryBoundsAndFleetIndependentIdentity(t *testing.T) {
	r, c, g, _, _ := resourceFixture(t)
	before, e := r.ResolveResource(context.Background(), c)
	if e != nil {
		t.Fatal(e)
	}
	g.DesiredCapacity = tea.Int32(4)
	g.MinSize = tea.Int32(2)
	r.proxy.sdk.instances = func(context.Context, *ess.DescribeScalingInstancesRequest, *dara.RuntimeOptions) (*ess.DescribeScalingInstancesResponse, error) {
		t.Fatal("identity discovery enumerated fleet membership")
		return nil, nil
	}
	after, e := r.ResolveResource(context.Background(), c)
	if e != nil || *before.Proxy != *after.Proxy || before.PgproxyWorkload.WorkloadKey != after.PgproxyWorkload.WorkloadKey {
		t.Fatal("capacity changed identity", e)
	}
	pageCalls := 0
	r.page = func(ctx context.Context, _ domain.Environment, _, _ string, _ int) (page, error) {
		pageCalls++
		if _, ok := ctx.Deadline(); !ok {
			t.Fatal("missing bounded deadline")
		}
		return page{Total: 10001, Records: []record{{ID: "asg-test", Tags: map[string]string{"env": "dev", "db-proxy-code": "proxy", "target-db-code": "db"}}}}, nil
	}
	if _, e := r.ResolveResource(context.Background(), c); e == nil {
		t.Fatal("accepted excessive discovery count")
	}
	if pageCalls != 1 {
		t.Fatalf("excessive discovery total did not stop immediately: %d calls", pageCalls)
	}
}

func TestResourceListenerDiscoveryConsumesAllBoundedPages(t *testing.T) {
	r, c, _, _, ls := resourceFixture(t)
	calls := 0
	r.proxy.sdk.listeners = func(_ context.Context, q *nlb.ListListenersRequest, _ *dara.RuntimeOptions) (*nlb.ListListenersResponse, error) {
		calls++
		switch tea.StringValue(q.NextToken) {
		case "":
			return &nlb.ListListenersResponse{Body: &nlb.ListListenersResponseBody{TotalCount: tea.Int32(2), NextToken: tea.String("page2"), Listeners: []*nlb.ListListenersResponseBodyListeners{{ListenerId: tea.String("unrelated"), ServerGroupId: tea.String("sgp-other"), LoadBalancerId: tea.String("nlb-test"), ListenerPort: tea.Int32(9999)}}}}, nil
		case "page2":
			ls.TotalCount = tea.Int32(2)
			return &nlb.ListListenersResponse{Body: ls}, nil
		}
		t.Fatal("unexpected pagination token")
		return nil, nil
	}
	got, e := r.ResolveResource(context.Background(), c)
	if e != nil || got.Proxy.ListenerID != "lsn-test@6432" || calls != 2 {
		t.Fatal(got, e, calls)
	}
}
func TestResourceListenerIncompleteUnrelatedRelationshipDenied(t *testing.T) {
	r, c, _, _, ls := resourceFixture(t)
	ls.TotalCount = tea.Int32(2)
	ls.Listeners = append(ls.Listeners, &nlb.ListListenersResponseBodyListeners{ListenerId: tea.String("unknown"), LoadBalancerId: tea.String("nlb-test")})
	if _, e := r.ResolveResource(context.Background(), c); e == nil {
		t.Fatal("incomplete listener relationship accepted")
	}
}
