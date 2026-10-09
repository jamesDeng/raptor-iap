package provider

import (
	"context"
	ess "github.com/alibabacloud-go/ess-20220222/v2/client"
	nlb "github.com/alibabacloud-go/nlb-20220430/v4/client"
	"github.com/alibabacloud-go/tea/tea"
	"raptor-iap/infra-api/internal/domain"
	"raptor-iap/infra-api/internal/policy"
)

func (p *ProxyBackend) target(ctx context.Context, env domain.Environment, id string, backend bool) (domain.ProxyMapping, error) {
	code := ""
	matches := 0
	for _, m := range env.Proxies {
		candidate := m.GroupID
		if backend {
			candidate = m.ServerGroupID
		}
		if candidate == id {
			code = m.Code
			matches++
		}
	}
	if matches != 1 {
		return domain.ProxyMapping{}, domain.ErrScope
	}
	return p.mapping(ctx, env, code)
}
func (p *ProxyBackend) Scale(ctx context.Context, env domain.Environment, groupID string, desired int) (string, error) {
	m, e := p.target(ctx, env, groupID, false)
	if e != nil {
		return "", e
	}
	if desired < 0 || desired > 2147483647 {
		return "", domain.ErrScope
	}
	if p.sdk.scale == nil {
		return "", domain.ErrNotConfigured
	}
	groups, e := p.groups(ctx, env, m)
	if e != nil {
		return "", e
	}
	if len(groups) != 1 {
		return "", providerRefusal()
	}
	g := groups[0]
	if tea.StringValue(g.ScalingGroupId) != groupID || tea.StringValue(g.LifecycleState) != "Active" || g.EnableDesiredCapacity == nil || !*g.EnableDesiredCapacity || g.MinSize == nil || g.MaxSize == nil || desired < int(*g.MinSize) || desired > int(*g.MaxSize) {
		return "", providerRefusal()
	}
	if ctx.Err() != nil {
		return "", ctx.Err()
	}
	// The caller owns the exact approval claim. A transport failure or missing
	// acknowledgement must retain that claim as unknown, never retry this call.
	r, e := p.sdk.scale(ctx, &ess.ModifyScalingGroupRequest{ScalingGroupId: tea.String(groupID), DesiredCapacity: tea.Int32(int32(desired))}, noRetry())
	if e != nil || r == nil || r.Body == nil || tea.StringValue(r.Body.RequestId) == "" {
		return "", &domain.CommandError{Code: "SubmissionUnknown"}
	}
	return *r.Body.RequestId, nil
}
func (p *ProxyBackend) Protect(ctx context.Context, env domain.Environment, groupID string, ids []string, protected bool) error {
	m, e := p.target(ctx, env, groupID, false)
	if e != nil {
		return e
	}
	if p.sdk.protect == nil {
		return domain.ErrNotConfigured
	}
	nodes, e := p.members(ctx, env, m)
	if e != nil {
		return e
	}
	if e = policy.Membership(policy.ProxySnapshot{Nodes: nodes}, ids); e != nil {
		return e
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	r, e := p.sdk.protect(ctx, &ess.SetInstancesProtectionRequest{ScalingGroupId: tea.String(groupID), InstanceIds: tea.StringSlice(ids), ProtectedFromScaleIn: tea.Bool(protected)}, noRetry())
	if e != nil || r == nil || r.Body == nil || tea.StringValue(r.Body.RequestId) == "" {
		return &domain.CommandError{Code: "SubmissionUnknown"}
	}
	return nil
}
func (p *ProxyBackend) Deregister(ctx context.Context, env domain.Environment, serverGroupID string, ids []string) error {
	m, e := p.target(ctx, env, serverGroupID, true)
	if e != nil {
		return e
	}
	if p.sdk.remove == nil {
		return domain.ErrNotConfigured
	}
	nodes, e := p.members(ctx, env, m)
	if e != nil {
		return e
	}
	if e = policy.Membership(policy.ProxySnapshot{Nodes: nodes}, ids); e != nil {
		return e
	}
	backends, e := p.backends(ctx, env, m)
	if e != nil {
		return e
	}
	servers := make([]*nlb.RemoveServersFromServerGroupRequestServers, 0, len(ids))
	for _, id := range ids {
		if _, ok := backends[id]; !ok {
			return domain.ErrScope
		}
		servers = append(servers, &nlb.RemoveServersFromServerGroupRequestServers{ServerId: tea.String(id), ServerType: tea.String("Ecs"), Port: tea.Int32(int32(m.Port))})
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	r, e := p.sdk.remove(ctx, &nlb.RemoveServersFromServerGroupRequest{RegionId: tea.String(env.Region), ServerGroupId: tea.String(serverGroupID), Servers: servers}, noRetry())
	if e != nil || r == nil || r.Body == nil || tea.StringValue(r.Body.RequestId) == "" || tea.StringValue(r.Body.JobId) == "" || tea.StringValue(r.Body.ServerGroupId) != serverGroupID {
		return &domain.CommandError{Code: "SubmissionUnknown"}
	}
	// This acknowledges the provider job. Runtime receipts say submitted; only
	// subsequent reads can establish completion.
	return nil
}
