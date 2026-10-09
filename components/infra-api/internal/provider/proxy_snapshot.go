package provider

import (
	"context"
	ess "github.com/alibabacloud-go/ess-20220222/v2/client"
	nlb "github.com/alibabacloud-go/nlb-20220430/v4/client"
	"github.com/alibabacloud-go/tea/tea"
	"raptor-iap/infra-api/internal/domain"
	"raptor-iap/infra-api/internal/policy"
	"sort"
)

func (p *ProxyBackend) groups(ctx context.Context, env domain.Environment, m domain.ProxyMapping) ([]*ess.DescribeScalingGroupsResponseBodyScalingGroups, error) {
	if p.sdk.groups == nil {
		return nil, domain.ErrNotConfigured
	}
	var rows []*ess.DescribeScalingGroupsResponseBodyScalingGroups
	total := -1
	seen := map[string]bool{}
	for page := int32(1); page <= 100; page++ {
		r, e := p.sdk.groups(ctx, &ess.DescribeScalingGroupsRequest{RegionId: tea.String(env.Region), PageNumber: tea.Int32(page), PageSize: tea.Int32(50), Tags: []*ess.DescribeScalingGroupsRequestTags{{Key: tea.String("env"), Value: tea.String(env.Code)}, {Key: tea.String("db-proxy-code"), Value: tea.String(m.Code)}}}, noRetry())
		if e != nil {
			return nil, providerRefusal()
		}
		if r == nil || r.Body == nil || r.Body.TotalCount == nil || r.Body.PageNumber == nil || *r.Body.PageNumber != page || *r.Body.TotalCount < 0 || *r.Body.TotalCount > 5000 {
			return nil, providerRefusal()
		}
		b := r.Body
		if total < 0 {
			total = int(*b.TotalCount)
		}
		if int(*b.TotalCount) != total {
			return nil, providerRefusal()
		}
		for _, g := range b.ScalingGroups {
			if g == nil || g.ScalingGroupId == nil || *g.ScalingGroupId == "" || seen[*g.ScalingGroupId] {
				return nil, providerRefusal()
			}
			seen[*g.ScalingGroupId] = true
			tags := map[string]string{}
			for _, t := range g.Tags {
				if t == nil || t.TagKey == nil || t.TagValue == nil {
					return nil, providerRefusal()
				}
				if _, ok := tags[*t.TagKey]; ok {
					return nil, providerRefusal()
				}
				tags[*t.TagKey] = *t.TagValue
			}
			if tags["env"] != env.Code || tags["db-proxy-code"] != m.Code {
				return nil, domain.ErrScope
			}
			rows = append(rows, g)
		}
		if len(rows) > total {
			return nil, providerRefusal()
		}
		if len(rows) == total {
			return rows, nil
		}
		if len(b.ScalingGroups) == 0 {
			return nil, providerRefusal()
		}
	}
	return nil, providerRefusal()
}
func (p *ProxyBackend) members(ctx context.Context, env domain.Environment, m domain.ProxyMapping) ([]policy.Node, error) {
	if p.sdk.instances == nil {
		return nil, domain.ErrNotConfigured
	}
	var rows []policy.Node
	total := -1
	seen := map[string]bool{}
	for page := int32(1); page <= 100; page++ {
		r, e := p.sdk.instances(ctx, &ess.DescribeScalingInstancesRequest{RegionId: tea.String(env.Region), ScalingGroupId: tea.String(m.GroupID), PageNumber: tea.Int32(page), PageSize: tea.Int32(50)}, noRetry())
		if e != nil {
			return nil, providerRefusal()
		}
		if r == nil || r.Body == nil || r.Body.TotalCount == nil || r.Body.PageNumber == nil || *r.Body.PageNumber != page || *r.Body.TotalCount < 0 || *r.Body.TotalCount > 5000 {
			return nil, providerRefusal()
		}
		b := r.Body
		if total < 0 {
			total = int(*b.TotalCount)
		}
		if int(*b.TotalCount) != total {
			return nil, providerRefusal()
		}
		for _, n := range b.ScalingInstances {
			if n == nil || n.InstanceId == nil || *n.InstanceId == "" || seen[*n.InstanceId] || tea.StringValue(n.ScalingGroupId) != m.GroupID || n.LifecycleState == nil || n.HealthStatus == nil {
				return nil, providerRefusal()
			}
			if *n.LifecycleState != "Protected" && *n.LifecycleState != "InService" {
				return nil, providerRefusal()
			}
			if *n.HealthStatus != "Healthy" && *n.HealthStatus != "Unhealthy" {
				return nil, providerRefusal()
			}
			seen[*n.InstanceId] = true
			rows = append(rows, policy.Node{ID: *n.InstanceId, Protected: *n.LifecycleState == "Protected", Healthy: *n.HealthStatus == "Healthy"})
		}
		if len(rows) > total {
			return nil, providerRefusal()
		}
		if len(rows) == total {
			sort.Slice(rows, func(i, j int) bool { return rows[i].ID < rows[j].ID })
			return rows, nil
		}
		if len(b.ScalingInstances) == 0 {
			return nil, providerRefusal()
		}
	}
	return nil, providerRefusal()
}
func (p *ProxyBackend) backends(ctx context.Context, env domain.Environment, m domain.ProxyMapping) (map[string]bool, error) {
	if p.sdk.servers == nil {
		return nil, domain.ErrNotConfigured
	}
	rows := map[string]bool{}
	total := -1
	tokens := map[string]bool{}
	token := ""
	for page := 0; page < 100; page++ {
		r, e := p.sdk.servers(ctx, &nlb.ListServerGroupServersRequest{RegionId: tea.String(env.Region), ServerGroupId: tea.String(m.ServerGroupID), MaxResults: tea.Int32(100), NextToken: tea.String(token)}, noRetry())
		if e != nil {
			return nil, providerRefusal()
		}
		if r == nil || r.Body == nil || r.Body.TotalCount == nil || *r.Body.TotalCount < 0 || *r.Body.TotalCount > 10000 {
			return nil, providerRefusal()
		}
		b := r.Body
		if total < 0 {
			total = int(*b.TotalCount)
		}
		if int(*b.TotalCount) != total {
			return nil, providerRefusal()
		}
		for _, s := range b.Servers {
			if s == nil || s.ServerId == nil || *s.ServerId == "" || tea.StringValue(s.ServerGroupId) != m.ServerGroupID || tea.StringValue(s.ServerType) != "Ecs" || s.Port == nil || int(*s.Port) != m.Port || tea.StringValue(s.Status) != "Available" || s.Weight == nil || *s.Weight < 0 || *s.Weight > 100 {
				return nil, providerRefusal()
			}
			if _, ok := rows[*s.ServerId]; ok {
				return nil, providerRefusal()
			}
			rows[*s.ServerId] = *s.Weight > 0
		}
		token = tea.StringValue(b.NextToken)
		if len(rows) > total {
			return nil, providerRefusal()
		}
		if token == "" {
			if len(rows) != total {
				return nil, providerRefusal()
			}
			return rows, nil
		}
		if len(b.Servers) == 0 || tokens[token] || len(rows) >= total {
			return nil, providerRefusal()
		}
		tokens[token] = true
	}
	return nil, providerRefusal()
}
func (p *ProxyBackend) unhealthy(ctx context.Context, env domain.Environment, m domain.ProxyMapping, backends map[string]bool) (map[string]bool, error) {
	if p.sdk.health == nil || p.sdk.listener == nil {
		return nil, domain.ErrNotConfigured
	}
	a, e := p.sdk.listener(ctx, &nlb.GetListenerAttributeRequest{RegionId: tea.String(env.Region), ListenerId: tea.String(m.ListenerID)}, noRetry())
	if e != nil || a == nil || a.Body == nil || tea.StringValue(a.Body.ListenerId) != m.ListenerID || tea.StringValue(a.Body.ServerGroupId) != m.ServerGroupID || a.Body.ListenerPort == nil || int(*a.Body.ListenerPort) != m.Port || tea.StringValue(a.Body.ListenerStatus) != "Running" {
		return nil, providerRefusal()
	}
	r, e := p.sdk.health(ctx, &nlb.GetListenerHealthStatusRequest{RegionId: tea.String(env.Region), ListenerId: tea.String(m.ListenerID)}, noRetry())
	// This operation exposes NextToken in its response, but has no request token
	// in the official SDK/API. A truncated health result must refuse.
	if e != nil || r == nil || r.Body == nil || tea.StringValue(r.Body.NextToken) != "" || len(r.Body.ListenerHealthStatus) != 1 {
		return nil, providerRefusal()
	}
	l := r.Body.ListenerHealthStatus[0]
	if l == nil || tea.StringValue(l.ListenerId) != m.ListenerID || l.ListenerPort == nil || int(*l.ListenerPort) != m.Port || len(l.ServerGroupInfos) != 1 {
		return nil, providerRefusal()
	}
	g := l.ServerGroupInfos[0]
	if g == nil || tea.StringValue(g.ServerGroupId) != m.ServerGroupID || g.HeathCheckEnabled == nil || !*g.HeathCheckEnabled || g.NonNormalServers == nil {
		return nil, providerRefusal()
	}
	bad := map[string]bool{}
	for _, n := range g.NonNormalServers {
		if n == nil || n.ServerId == nil || n.Port == nil || int(*n.Port) != m.Port || bad[*n.ServerId] {
			return nil, providerRefusal()
		}
		if _, ok := backends[*n.ServerId]; !ok {
			return nil, providerRefusal()
		}
		status := tea.StringValue(n.Status)
		if status != "Initial" && status != "Unhealthy" && status != "Unavailable" {
			return nil, providerRefusal()
		}
		bad[*n.ServerId] = true
	}
	return bad, nil
}

// Filtered ESS enumeration returns only the filter tags. Hydrate the exact
// registered group without filters before verifying its database association.
func (p *ProxyBackend) groupDetail(ctx context.Context, env domain.Environment, m domain.ProxyMapping) (*ess.DescribeScalingGroupsResponseBodyScalingGroups, error) {
	r, e := p.sdk.groups(ctx, &ess.DescribeScalingGroupsRequest{RegionId: tea.String(env.Region), ScalingGroupIds: []*string{tea.String(m.GroupID)}, PageNumber: tea.Int32(1), PageSize: tea.Int32(1)}, noRetry())
	if e != nil || r == nil || r.Body == nil || r.Body.TotalCount == nil || *r.Body.TotalCount != 1 || r.Body.PageNumber == nil || *r.Body.PageNumber != 1 || len(r.Body.ScalingGroups) != 1 || r.Body.ScalingGroups[0] == nil {
		return nil, providerRefusal()
	}
	g := r.Body.ScalingGroups[0]
	if tea.StringValue(g.ScalingGroupId) != m.GroupID {
		return nil, domain.ErrScope
	}
	tags := map[string]string{}
	for _, tag := range g.Tags {
		if tag == nil || tag.TagKey == nil || tag.TagValue == nil {
			return nil, providerRefusal()
		}
		if _, exists := tags[*tag.TagKey]; exists {
			return nil, providerRefusal()
		}
		tags[*tag.TagKey] = *tag.TagValue
	}
	if tags["env"] != env.Code || tags["db-proxy-code"] != m.Code || tags["target-db-code"] != m.TargetDBCode {
		return nil, domain.ErrScope
	}
	return g, nil
}

func (p *ProxyBackend) Snapshot(ctx context.Context, env domain.Environment, code string) ([]policy.ProxySnapshot, error) {
	m, e := p.mapping(ctx, env, code)
	if e != nil {
		return nil, e
	}
	groups, e := p.groups(ctx, env, m)
	if e != nil {
		return nil, e
	}
	if len(groups) != 1 {
		return nil, providerRefusal()
	}
	if tea.StringValue(groups[0].ScalingGroupId) != m.GroupID {
		return nil, domain.ErrScope
	}
	g, e := p.groupDetail(ctx, env, m)
	if e != nil {
		return nil, e
	}
	if tea.StringValue(g.ScalingGroupId) != m.GroupID || tea.StringValue(g.LifecycleState) != "Active" || g.EnableDesiredCapacity == nil || !*g.EnableDesiredCapacity || g.DesiredCapacity == nil || g.MinSize == nil || g.MaxSize == nil || *g.MinSize < 0 || *g.MaxSize < *g.MinSize || *g.DesiredCapacity < *g.MinSize || *g.DesiredCapacity > *g.MaxSize {
		return nil, providerRefusal()
	}
	if len(g.ServerGroups) != 1 || g.ServerGroups[0] == nil {
		return nil, providerRefusal()
	}
	sg := g.ServerGroups[0]
	if tea.StringValue(sg.ServerGroupId) != m.ServerGroupID || tea.StringValue(sg.Type) != "NLB" || sg.Port == nil || int(*sg.Port) != m.Port {
		return nil, domain.ErrScope
	}
	nodes, e := p.members(ctx, env, m)
	if e != nil {
		return nil, e
	}
	if len(nodes) != int(*g.DesiredCapacity) {
		return nil, providerRefusal()
	}
	backends, e := p.backends(ctx, env, m)
	if e != nil {
		return nil, e
	}
	bad, e := p.unhealthy(ctx, env, m, backends)
	if e != nil {
		return nil, e
	}
	members := map[string]bool{}
	for i := range nodes {
		n := &nodes[i]
		members[n.ID] = true
		weight, exists := backends[n.ID]
		n.Registered = exists
		n.Healthy = n.Healthy && exists && weight && !bad[n.ID]
	}
	for id := range backends {
		if !members[id] {
			return nil, domain.ErrScope
		}
	}
	return []policy.ProxySnapshot{{EnvCode: env.Code, ProxyCode: code, GroupID: m.GroupID, ServerGroupID: m.ServerGroupID, Desired: int(*g.DesiredCapacity), Min: int(*g.MinSize), Max: int(*g.MaxSize), Nodes: nodes}}, nil
}
