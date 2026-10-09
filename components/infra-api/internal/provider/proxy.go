package provider

import (
	"context"
	ess "github.com/alibabacloud-go/ess-20220222/v2/client"
	nlb "github.com/alibabacloud-go/nlb-20220430/v4/client"
	"github.com/alibabacloud-go/tea/dara"
	"github.com/alibabacloud-go/tea/tea"
	"raptor-iap/infra-api/internal/domain"
)

// Typed SDK boundaries retain presence information: omitted provider fields are
// never converted to zero capacity, healthy state or unprotected membership.
type proxySDK struct {
	groups    func(context.Context, *ess.DescribeScalingGroupsRequest, *dara.RuntimeOptions) (*ess.DescribeScalingGroupsResponse, error)
	instances func(context.Context, *ess.DescribeScalingInstancesRequest, *dara.RuntimeOptions) (*ess.DescribeScalingInstancesResponse, error)
	servers   func(context.Context, *nlb.ListServerGroupServersRequest, *dara.RuntimeOptions) (*nlb.ListServerGroupServersResponse, error)
	health    func(context.Context, *nlb.GetListenerHealthStatusRequest, *dara.RuntimeOptions) (*nlb.GetListenerHealthStatusResponse, error)
	listener  func(context.Context, *nlb.GetListenerAttributeRequest, *dara.RuntimeOptions) (*nlb.GetListenerAttributeResponse, error)
	scale     func(context.Context, *ess.ModifyScalingGroupRequest, *dara.RuntimeOptions) (*ess.ModifyScalingGroupResponse, error)
	protect   func(context.Context, *ess.SetInstancesProtectionRequest, *dara.RuntimeOptions) (*ess.SetInstancesProtectionResponse, error)
	remove    func(context.Context, *nlb.RemoveServersFromServerGroupRequest, *dara.RuntimeOptions) (*nlb.RemoveServersFromServerGroupResponse, error)
}
type ProxyBackend struct {
	identity func(context.Context, domain.Environment) (string, error)
	sdk      proxySDK
}

func providerRefusal() error        { return &domain.CommandError{Code: "ProviderStateInvalid"} }
func noRetry() *dara.RuntimeOptions { return &dara.RuntimeOptions{Autoretry: tea.Bool(false)} }
func (p *ProxyBackend) mapping(ctx context.Context, env domain.Environment, code string) (domain.ProxyMapping, error) {
	if ctx.Err() != nil {
		return domain.ProxyMapping{}, ctx.Err()
	}
	if env.Region != "ap-southeast-1" || env.AccountID == "" || env.Code == "" || p.identity == nil {
		return domain.ProxyMapping{}, domain.ErrScope
	}
	account, e := p.identity(ctx, env)
	if e != nil || account != env.AccountID {
		return domain.ProxyMapping{}, domain.ErrScope
	}
	var matches []domain.ProxyMapping
	for _, m := range env.Proxies {
		if m.Code == code {
			matches = append(matches, m)
		}
	}
	if len(matches) != 1 {
		return domain.ProxyMapping{}, domain.ErrScope
	}
	m := matches[0]
	if m.GroupID == "" || m.ServerGroupID == "" || m.ListenerID == "" || m.TargetDBCode == "" || m.Port < 1 || m.Port > 65535 {
		return domain.ProxyMapping{}, domain.ErrScope
	}
	return m, nil
}
