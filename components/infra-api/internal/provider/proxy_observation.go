package provider

import (
	"context"
	"net/netip"
	"sort"
	"time"

	ess "github.com/alibabacloud-go/ess-20220222/v2/client"
	nlb "github.com/alibabacloud-go/nlb-20220430/v4/client"
	"github.com/alibabacloud-go/tea/tea"
	"raptor-iap/infra-api/internal/domain"
)

// observationGroup shares scope/tag checks, but never uses the strict command
// snapshot: pending membership is useful read evidence even before convergence.
func (p *reader) observationGroup(ctx context.Context, env domain.Environment, target domain.ProxyReadTarget) (domain.ProxyMapping, *ess.DescribeScalingGroupsResponseBodyScalingGroups, error) {
	if p.proxy == nil {
		return domain.ProxyMapping{}, nil, domain.ErrNotConfigured
	}
	if target.EnvCode != env.Code || target.ProxyCode == "" {
		return domain.ProxyMapping{}, nil, domain.ErrScope
	}
	m, err := p.proxy.mapping(ctx, env, target.ProxyCode)
	if err != nil {
		return m, nil, err
	}
	if target.GroupID != m.GroupID || target.ServerGroupID != m.ServerGroupID {
		return m, nil, domain.ErrScope
	}
	groups, err := p.proxy.groups(ctx, env, m)
	if err != nil {
		return m, nil, err
	}
	if len(groups) != 1 {
		return m, nil, providerRefusal()
	}
	if tea.StringValue(groups[0].ScalingGroupId) != m.GroupID {
		return m, nil, domain.ErrScope
	}
	g, err := p.proxy.groupDetail(ctx, env, m)
	if err != nil {
		return m, nil, err
	}
	if tea.StringValue(g.LifecycleState) != "Active" || g.EnableDesiredCapacity == nil || !*g.EnableDesiredCapacity || g.DesiredCapacity == nil || g.MinSize == nil || g.MaxSize == nil || *g.MinSize < 0 || *g.MaxSize < *g.MinSize || *g.DesiredCapacity < *g.MinSize || *g.DesiredCapacity > *g.MaxSize {
		return m, nil, providerRefusal()
	}
	if len(g.ServerGroups) != 1 || g.ServerGroups[0] == nil {
		return m, nil, providerRefusal()
	}
	sg := g.ServerGroups[0]
	if tea.StringValue(sg.ServerGroupId) != m.ServerGroupID || tea.StringValue(sg.Type) != "NLB" || sg.Port == nil || int(*sg.Port) != m.Port {
		return m, nil, domain.ErrScope
	}
	return m, g, nil
}

func (p *reader) ObserveProxy(ctx context.Context, env domain.Environment, target domain.ProxyReadTarget, kind string) (domain.ProxyObservation, error) {
	if kind != "fleet" && kind != "capacity" && kind != "backend" {
		return domain.ProxyObservation{}, domain.ErrScope
	}
	m, g, err := p.observationGroup(ctx, env, target)
	if err != nil {
		return domain.ProxyObservation{}, err
	}
	if kind == "capacity" {
		return domain.ProxyObservation{Capacity: &domain.CapacityObservation{GroupID: m.GroupID, DesiredCapacity: int(*g.DesiredCapacity), MinSize: int(*g.MinSize), MaxSize: int(*g.MaxSize), ObservedAt: time.Now().UTC()}}, nil
	}
	members, err := p.proxy.observationMembers(ctx, env, m)
	if err != nil {
		return domain.ProxyObservation{}, err
	}
	if kind == "fleet" {
		return domain.ProxyObservation{Fleet: &domain.FleetObservation{GroupID: m.GroupID, Instances: members, ObservedAt: time.Now().UTC()}}, nil
	}
	nodes, err := p.proxy.observationBackends(ctx, env, m)
	if err != nil {
		return domain.ProxyObservation{}, err
	}
	weights := map[string]bool{}
	for _, n := range nodes {
		weights[n.InstanceID] = n.Registered
	}
	bad, err := p.proxy.unhealthy(ctx, env, m, weights)
	if err != nil {
		return domain.ProxyObservation{}, err
	}
	fleet := map[string]domain.FleetNode{}
	for _, n := range members {
		fleet[n.InstanceID] = n
	}
	for i := range nodes {
		n := &nodes[i]
		member, ok := fleet[n.InstanceID]
		if !ok {
			return domain.ProxyObservation{}, domain.ErrScope
		}
		n.Healthy = (member.LifecycleState == "Protected" || member.LifecycleState == "InService") && member.HealthStatus == "Healthy" && n.Status == "Available" && n.Registered && !bad[n.InstanceID]
	}
	return domain.ProxyObservation{Backend: &domain.BackendObservation{ServerGroupID: m.ServerGroupID, ListenerID: m.ListenerID, Nodes: nodes, ObservedAt: time.Now().UTC()}}, nil
}

func observationLifecycle(state string) bool {
	switch state {
	case "InService", "Protected", "Pending", "Pending:Wait", "Standby", "Stopped", "Removing", "Removing:Wait":
		return true
	}
	return false
}

func (p *ProxyBackend) observationMembers(ctx context.Context, env domain.Environment, m domain.ProxyMapping) ([]domain.FleetNode, error) {
	if p.sdk.instances == nil {
		return nil, domain.ErrNotConfigured
	}
	rows := []domain.FleetNode{}
	total := -1
	seen := map[string]bool{}
	for page := int32(1); page <= 100; page++ {
		r, err := p.sdk.instances(ctx, &ess.DescribeScalingInstancesRequest{RegionId: tea.String(env.Region), ScalingGroupId: tea.String(m.GroupID), PageNumber: tea.Int32(page), PageSize: tea.Int32(50)}, noRetry())
		if err != nil || r == nil || r.Body == nil || r.Body.TotalCount == nil || r.Body.PageNumber == nil || *r.Body.PageNumber != page || *r.Body.TotalCount < 0 || *r.Body.TotalCount > 5000 {
			return nil, providerRefusal()
		}
		b := r.Body
		if total < 0 {
			total = int(*b.TotalCount)
		}
		if int(*b.TotalCount) != total || len(b.ScalingInstances) > 50 {
			return nil, providerRefusal()
		}
		for _, n := range b.ScalingInstances {
			if n == nil || n.InstanceId == nil || *n.InstanceId == "" || seen[*n.InstanceId] || tea.StringValue(n.ScalingGroupId) != m.GroupID || !observationLifecycle(tea.StringValue(n.LifecycleState)) || (tea.StringValue(n.HealthStatus) != "Healthy" && tea.StringValue(n.HealthStatus) != "Unhealthy") {
				return nil, providerRefusal()
			}
			seen[*n.InstanceId] = true
			rows = append(rows, domain.FleetNode{InstanceID: *n.InstanceId, ProtectedFromScaleIn: *n.LifecycleState == "Protected", LifecycleState: *n.LifecycleState, HealthStatus: *n.HealthStatus})
		}
		if len(rows) > total {
			return nil, providerRefusal()
		}
		if len(rows) == total {
			sort.Slice(rows, func(i, j int) bool { return rows[i].InstanceID < rows[j].InstanceID })
			return rows, nil
		}
		if len(b.ScalingInstances) == 0 {
			return nil, providerRefusal()
		}
	}
	return nil, providerRefusal()
}

func (p *ProxyBackend) observationBackends(ctx context.Context, env domain.Environment, m domain.ProxyMapping) ([]domain.BackendNode, error) {
	if p.sdk.servers == nil {
		return nil, domain.ErrNotConfigured
	}
	rows := []domain.BackendNode{}
	total := -1
	seen, tokens := map[string]bool{}, map[string]bool{}
	token := ""
	for page := 0; page < 100; page++ {
		r, err := p.sdk.servers(ctx, &nlb.ListServerGroupServersRequest{RegionId: tea.String(env.Region), ServerGroupId: tea.String(m.ServerGroupID), MaxResults: tea.Int32(100), NextToken: tea.String(token)}, noRetry())
		if err != nil || r == nil || r.Body == nil || r.Body.TotalCount == nil || *r.Body.TotalCount < 0 || *r.Body.TotalCount > 10000 {
			return nil, providerRefusal()
		}
		b := r.Body
		if total < 0 {
			total = int(*b.TotalCount)
		}
		if int(*b.TotalCount) != total || len(b.Servers) > 100 {
			return nil, providerRefusal()
		}
		for _, n := range b.Servers {
			if n == nil || n.ServerId == nil || *n.ServerId == "" || seen[*n.ServerId] || tea.StringValue(n.ServerGroupId) != m.ServerGroupID || tea.StringValue(n.ServerType) != "Ecs" || n.Port == nil || int(*n.Port) != m.Port || n.Weight == nil || *n.Weight < 0 || *n.Weight > 100 {
				return nil, providerRefusal()
			}
			status := tea.StringValue(n.Status)
			if status != "Available" && status != "Adding" && status != "Configuring" && status != "Removing" {
				return nil, providerRefusal()
			}
			ip, err := netip.ParseAddr(tea.StringValue(n.ServerIp))
			if err != nil || !ip.Is4() || !ip.IsPrivate() {
				return nil, providerRefusal()
			}
			seen[*n.ServerId] = true
			rows = append(rows, domain.BackendNode{InstanceID: *n.ServerId, PrivateIP: *n.ServerIp, Status: status, Registered: *n.Weight > 0})
		}
		token = tea.StringValue(b.NextToken)
		if len(rows) > total {
			return nil, providerRefusal()
		}
		if token == "" {
			if len(rows) != total {
				return nil, providerRefusal()
			}
			sort.Slice(rows, func(i, j int) bool { return rows[i].InstanceID < rows[j].InstanceID })
			return rows, nil
		}
		if len(b.Servers) == 0 || tokens[token] || len(rows) >= total {
			return nil, providerRefusal()
		}
		tokens[token] = true
	}
	return nil, providerRefusal()
}
