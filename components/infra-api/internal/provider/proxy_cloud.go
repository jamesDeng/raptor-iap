package provider

import (
	"errors"
	openapi "github.com/alibabacloud-go/darabonba-openapi/v2/client"
	ess "github.com/alibabacloud-go/ess-20220222/v2/client"
	nlb "github.com/alibabacloud-go/nlb-20220430/v4/client"
)

// NewProxyBackend shares the existing credential-chain/account identity rules.
// It does not enable commands: startup must explicitly wire the gated runtime.
func NewProxyBackend() (*ProxyBackend, error) {
	r, e := New()
	if e != nil {
		return nil, e
	}
	return r.(*reader).proxy, nil
}
func cloudProxy(config func(string) *openapi.Config) (proxySDK, error) {
	ec, e := ess.NewClient(config("ess.ap-southeast-1.aliyuncs.com"))
	if e != nil {
		return proxySDK{}, errors.New("provider unavailable")
	}
	nc, e := nlb.NewClient(config("nlb.ap-southeast-1.aliyuncs.com"))
	if e != nil {
		return proxySDK{}, errors.New("provider unavailable")
	}
	return proxySDK{serverGroups: nc.ListServerGroupsWithContext, listeners: nc.ListListenersWithContext, groups: ec.DescribeScalingGroupsWithContext, instances: ec.DescribeScalingInstancesWithContext, scale: ec.ModifyScalingGroupWithContext, protect: ec.SetInstancesProtectionWithContext, servers: nc.ListServerGroupServersWithContext, health: nc.GetListenerHealthStatusWithContext, listener: nc.GetListenerAttributeWithContext, remove: nc.RemoveServersFromServerGroupWithContext}, nil
}
