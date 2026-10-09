package provider

import (
	"errors"
	openapi "github.com/alibabacloud-go/darabonba-openapi/v2/client"
	ess "github.com/alibabacloud-go/ess-20220222/v2/client"
	nlb "github.com/alibabacloud-go/nlb-20220430/v4/client"
	"github.com/alibabacloud-go/tea/tea"
	"github.com/aliyun/credentials-go/credentials"
)

// NewProxyBackend shares the existing credential-chain/account identity rules.
// It does not enable commands: startup must explicitly wire the gated runtime.
func NewProxyBackend() (*ProxyBackend, error) {
	reader, e := New()
	if e != nil {
		return nil, e
	}
	cred, e := credentials.NewCredential(nil)
	if e != nil {
		return nil, errors.New("credential provider unavailable")
	}
	config := func(endpoint string) *openapi.Config {
		return &openapi.Config{Credential: cred, Endpoint: tea.String(endpoint), ConnectTimeout: tea.Int(5000), ReadTimeout: tea.Int(10000)}
	}
	sdk, e := cloudProxy(config)
	if e != nil {
		return nil, e
	}
	return &ProxyBackend{identity: reader.Identity, sdk: sdk}, nil
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
	return proxySDK{groups: ec.DescribeScalingGroupsWithContext, instances: ec.DescribeScalingInstancesWithContext, scale: ec.ModifyScalingGroupWithContext, protect: ec.SetInstancesProtectionWithContext, servers: nc.ListServerGroupServersWithContext, health: nc.GetListenerHealthStatusWithContext, listener: nc.GetListenerAttributeWithContext, remove: nc.RemoveServersFromServerGroupWithContext}, nil
}
