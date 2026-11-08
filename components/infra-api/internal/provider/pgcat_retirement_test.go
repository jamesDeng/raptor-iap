package provider

import (
	"context"
	"errors"
	"reflect"
	"testing"

	ess "github.com/alibabacloud-go/ess-20220222/v2/client"
	nlb "github.com/alibabacloud-go/nlb-20220430/v4/client"
	"github.com/alibabacloud-go/tea/dara"
	"github.com/alibabacloud-go/tea/tea"
	"raptor-iap/infra-api/internal/domain"
)

func TestRetirementSubmitsOnlyExactDrainedOriginalNodes(t *testing.T) {
	p, env, group, members, backends, _ := proxyFixture(t)
	group.MinSize, group.DesiredCapacity = tea.Int32(2), tea.Int32(4)
	members.TotalCount = tea.Int32(4)
	backends.TotalCount = tea.Int32(4)
	for i := range backends.Servers {
		backends.Servers[i].Weight = tea.Int32(0)
	}
	for _, id := range []string{"i-three", "i-four"} {
		members.ScalingInstances = append(members.ScalingInstances, &ess.DescribeScalingInstancesResponseBodyScalingInstances{
			InstanceId: tea.String(id), ScalingGroupId: tea.String("asg-test"), LifecycleState: tea.String("Protected"), HealthStatus: tea.String("Healthy")})
		backends.Servers = append(backends.Servers, &nlb.ListServerGroupServersResponseBodyServers{
			ServerId: tea.String(id), ServerGroupId: tea.String("sgp-test"), ServerType: tea.String("Ecs"), Port: tea.Int32(6432), Status: tea.String("Available"), Weight: tea.Int32(100)})
	}
	calls := 0
	p.sdk.retire = func(_ context.Context, in *ess.RemoveInstancesRequest, runtime *dara.RuntimeOptions) (*ess.RemoveInstancesResponse, error) {
		calls++
		if tea.StringValue(in.ScalingGroupId) != "asg-test" || tea.StringValue(in.RegionId) != "ap-southeast-1" ||
			tea.StringValue(in.RemovePolicy) != "release" || !tea.BoolValue(in.DecreaseDesiredCapacity) || in.IgnoreInvalidInstance == nil ||
			*in.IgnoreInvalidInstance || !reflect.DeepEqual(tea.StringSliceValue(in.InstanceIds), []string{"i-one", "i-two"}) ||
			len(tea.StringValue(in.ClientToken)) != 64 || runtime.Autoretry == nil || *runtime.Autoretry {
			t.Fatal("retirement changed selected nodes or safety parameters")
		}
		if calls > 1 {
			return nil, errors.New("internal provider error")
		}
		return &ess.RemoveInstancesResponse{Body: &ess.RemoveInstancesResponseBody{RequestId: tea.String("request-1"), ScalingActivityId: tea.String("activity-1")}}, nil
	}
	in := domain.PgcatRetirement{ProxyTarget: domain.ProxyTarget{RequestID: "request", EnvCode: "dev", ProxyCode: "proxy", GroupID: "asg-test"},
		ActionID: "retire-old", OriginalIDs: []string{"i-two", "i-one"}, ReplacementIDs: []string{"i-four", "i-three"}}
	wrong := in
	wrong.OriginalIDs = []string{"i-one", "i-three"}
	wrong.ReplacementIDs = []string{"i-two", "i-four"}
	if _, err := p.RetirePgcat(context.Background(), env, wrong, allowPgcatWrite); err == nil || calls != 0 {
		t.Fatal("routed replacement reached removal")
	}
	out, err := p.RetirePgcat(context.Background(), env, in, allowPgcatWrite)
	if err != nil || calls != 1 || out.ProviderRequestID != "request-1" || out.ScalingActivityID != "activity-1" {
		t.Fatalf("retirement: %+v %v", out, err)
	}
	if _, err = p.RetirePgcat(context.Background(), env, in, allowPgcatWrite); err == nil || err.Error() != "SubmissionUnknown" || calls != 2 {
		t.Fatalf("uncertain outcome did not remain unknown: %v", err)
	}
}

func allowPgcatWrite(context.Context) error { return nil }
