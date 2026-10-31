package provider

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	nlb "github.com/alibabacloud-go/nlb-20220430/v4/client"
	"github.com/alibabacloud-go/tea/tea"
	"raptor-iap/infra-api/internal/domain"
	"sort"
	"time"
)

// NewResourceBackend shares existing credential-chain adapters. It performs no
// discovery until called and does not wire any active server or mutation path.
func NewResourceBackend() (interface {
	ResolveResource(context.Context, domain.ResourceContext) (domain.ResolvedResource, error)
}, error) {
	r, e := New()
	if e != nil {
		return nil, e
	}
	return r.(*reader), nil
}
func (p *reader) ResolveResource(ctx context.Context, c domain.ResourceContext) (domain.ResolvedResource, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if ctx.Err() != nil {
		return domain.ResolvedResource{}, ctx.Err()
	}
	if c.Cloud != "aliyun" || c.Region != "ap-southeast-1" || c.AccountID == "" || c.EnvCode == "" || c.Code == "" || (c.Kind != "application" && c.Kind != "database" && c.Kind != "db-proxy") {
		return domain.ResolvedResource{}, domain.ErrScope
	}
	env := domain.Environment{Code: c.EnvCode, AccountID: c.AccountID, Region: c.Region, ClusterID: c.ClusterID}
	account, e := p.Identity(ctx, env)
	if e != nil {
		return domain.ResolvedResource{}, e
	}
	if account != c.AccountID {
		return domain.ResolvedResource{}, domain.ErrScope
	}
	out := domain.ResolvedResource{Context: c, Environment: env}
	if c.Kind == "application" {
		rows, e := p.applications(ctx, env, c.Code)
		if e != nil {
			return domain.ResolvedResource{}, e
		}
		out.ApplicationWorkloads = []domain.ApplicationWorkload{}
		uids := map[string]bool{}
		names := map[string]bool{}
		for _, d := range rows {
			key := d.Namespace + "/" + d.Name
			if d.UID == "" || d.Namespace == "" || d.Name == "" || d.ClusterID != c.ClusterID || d.EnvCode != c.EnvCode || d.ObjectCode != c.Code || uids[d.UID] || names[key] {
				return domain.ResolvedResource{}, domain.ErrIdentity
			}
			uids[d.UID] = true
			names[key] = true
			target := domain.Target{EnvCode: env.Code, AppCode: c.Code, ClusterID: env.ClusterID, Namespace: d.Namespace, Name: d.Name, UID: d.UID}
			out.ApplicationWorkloads = append(out.ApplicationWorkloads, domain.ApplicationWorkload{Type: "kubernetes", WorkloadKey: workloadKey(target), Target: target, ClusterID: d.ClusterID, Namespace: d.Namespace, Name: d.Name, UID: d.UID, State: d.State})
		}
		sort.Slice(out.ApplicationWorkloads, func(i, j int) bool {
			return out.ApplicationWorkloads[i].WorkloadKey < out.ApplicationWorkloads[j].WorkloadKey
		})
		return out, nil
	}
	rows, e := p.discover(ctx, env, c.Kind, c.Code)
	if e != nil {
		return domain.ResolvedResource{}, e
	}
	tag := "db-code"
	if c.Kind == "db-proxy" {
		tag = "db-proxy-code"
	}
	for _, r := range rows {
		if r.Tags["env"] != env.Code || r.Tags[tag] != c.Code {
			return domain.ResolvedResource{}, domain.ErrScope
		}
	}
	if c.Kind == "database" {
		out.DatabaseWorkloads = []domain.DatabaseWorkload{}
		for _, r := range rows {
			out.DatabaseWorkloads = append(out.DatabaseWorkloads, domain.DatabaseWorkload{Type: "rds", Code: c.Code, ResourceID: r.ID, Name: r.Name, State: r.State, Endpoint: r.Endpoint})
		}
		sort.Slice(out.DatabaseWorkloads, func(i, j int) bool { return out.DatabaseWorkloads[i].ResourceID < out.DatabaseWorkloads[j].ResourceID })
		return out, nil
	}
	if len(rows) != 1 || rows[0].Tags["target-db-code"] == "" || p.proxy == nil || p.proxy.sdk.groups == nil {
		return domain.ResolvedResource{}, domain.ErrIdentity
	}
	m := domain.ProxyMapping{Code: c.Code, GroupID: rows[0].ID, TargetDBCode: rows[0].Tags["target-db-code"]}
	// Fetch exact group detail even when tag filtering returns full metadata.
	g, e := p.proxy.groupDetail(ctx, env, m)
	if e != nil {
		return domain.ResolvedResource{}, e
	}
	if tea.StringValue(g.ScalingGroupId) != m.GroupID || len(g.ServerGroups) != 1 || g.ServerGroups[0] == nil {
		return domain.ResolvedResource{}, domain.ErrScope
	}
	sg := g.ServerGroups[0]
	if tea.StringValue(sg.Type) != "NLB" || tea.StringValue(sg.ServerGroupId) == "" || sg.Port == nil || *sg.Port < 1 || *sg.Port > 65535 {
		return domain.ResolvedResource{}, domain.ErrScope
	}
	m.ServerGroupID = *sg.ServerGroupId
	m.Port = int(*sg.Port)
	lb, e := p.proxy.resourceLoadBalancer(ctx, env, m)
	if e != nil {
		return domain.ResolvedResource{}, e
	}
	m.ListenerID, e = p.proxy.resourceListener(ctx, env, m, lb)
	if e != nil {
		return domain.ResolvedResource{}, e
	}
	out.Proxy = &m
	out.Environment.Proxies = []domain.ProxyMapping{m}
	out.PgproxyWorkload = &domain.PgproxyWorkload{Type: "ess", Code: c.Code, WorkloadKey: workloadKey(struct{ Env, Code, Group string }{env.Code, c.Code, m.GroupID}), State: tea.StringValue(g.LifecycleState), TargetDBCode: m.TargetDBCode}
	return out, nil
}
func workloadKey(identity any) string {
	b, _ := json.Marshal(identity)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
func (p *ProxyBackend) resourceLoadBalancer(ctx context.Context, env domain.Environment, m domain.ProxyMapping) (string, error) {
	if p.sdk.serverGroups == nil {
		return "", domain.ErrNotConfigured
	}
	pages := newTokenPages()
	lb := ""
	for n := 0; n < 100; n++ {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		r, e := p.sdk.serverGroups(ctx, &nlb.ListServerGroupsRequest{RegionId: tea.String(env.Region), ServerGroupIds: []*string{tea.String(m.ServerGroupID)}, NextToken: tea.String(pages.token), MaxResults: tea.Int32(100)}, noRetry())
		if e != nil || r == nil || r.Body == nil {
			return "", providerRefusal()
		}
		b := r.Body
		for _, g := range b.ServerGroups {
			if g == nil || tea.StringValue(g.ServerGroupId) != m.ServerGroupID || len(g.RelatedLoadBalancerIds) != 1 || tea.StringValue(g.RelatedLoadBalancerIds[0]) == "" || !pages.add(tea.StringValue(g.ServerGroupId)) {
				return "", domain.ErrScope
			}
			lb = tea.StringValue(g.RelatedLoadBalancerIds[0])
		}
		done, e := pages.next(b.TotalCount, tea.StringValue(b.NextToken), len(b.ServerGroups))
		if e != nil {
			return "", e
		}
		if done {
			if len(pages.ids) != 1 || lb == "" {
				return "", domain.ErrScope
			}
			return lb, nil
		}
	}
	return "", providerRefusal()
}
func (p *ProxyBackend) resourceListener(ctx context.Context, env domain.Environment, m domain.ProxyMapping, lb string) (string, error) {
	if p.sdk.listeners == nil || p.sdk.listener == nil {
		return "", domain.ErrNotConfigured
	}
	pages := newTokenPages()
	matches := []string{}
	for n := 0; n < 100; n++ {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		r, e := p.sdk.listeners(ctx, &nlb.ListListenersRequest{RegionId: tea.String(env.Region), LoadBalancerIds: []*string{tea.String(lb)}, NextToken: tea.String(pages.token), MaxResults: tea.Int32(100)}, noRetry())
		if e != nil || r == nil || r.Body == nil {
			return "", providerRefusal()
		}
		b := r.Body
		for _, l := range b.Listeners {
			if l == nil || tea.StringValue(l.LoadBalancerId) != lb || tea.StringValue(l.ServerGroupId) == "" || l.ListenerPort == nil || *l.ListenerPort < 1 || *l.ListenerPort > 65535 || !pages.add(tea.StringValue(l.ListenerId)) {
				return "", domain.ErrScope
			}
			if tea.StringValue(l.ServerGroupId) == m.ServerGroupID {
				if l.ListenerPort == nil || int(*l.ListenerPort) != m.Port {
					return "", domain.ErrScope
				}
				matches = append(matches, *l.ListenerId)
			}
		}
		done, e := pages.next(b.TotalCount, tea.StringValue(b.NextToken), len(b.Listeners))
		if e != nil {
			return "", e
		}
		if done {
			if len(matches) != 1 {
				return "", domain.ErrIdentity
			}
			if ctx.Err() != nil {
				return "", ctx.Err()
			}
			r, e := p.sdk.listener(ctx, &nlb.GetListenerAttributeRequest{RegionId: tea.String(env.Region), ListenerId: tea.String(matches[0])}, noRetry())
			if e != nil || r == nil || r.Body == nil {
				return "", providerRefusal()
			}
			a := r.Body
			if tea.StringValue(a.ListenerId) != matches[0] || tea.StringValue(a.LoadBalancerId) != lb || tea.StringValue(a.ServerGroupId) != m.ServerGroupID || a.ListenerPort == nil || int(*a.ListenerPort) != m.Port || tea.StringValue(a.ListenerStatus) != "Running" {
				return "", domain.ErrScope
			}
			return matches[0], nil
		}
	}
	return "", providerRefusal()
}
