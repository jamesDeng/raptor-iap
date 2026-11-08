package provider

import (
	"context"
	"errors"
	"testing"

	ess "github.com/alibabacloud-go/ess-20220222/v2/client"
	"github.com/alibabacloud-go/tea/dara"
	"github.com/alibabacloud-go/tea/tea"
	"raptor-iap/infra-api/internal/domain"
)

func TestPgcatExpansionPinsImmediateProviderActivity(t *testing.T) {
	p, env, group, _, _, _ := proxyFixture(t)
	group.MinSize, group.DesiredCapacity = tea.Int32(2), tea.Int32(2)
	calls := 0
	p.sdk.expand = func(_ context.Context, in *ess.ScaleWithAdjustmentRequest, runtime *dara.RuntimeOptions) (*ess.ScaleWithAdjustmentResponse, error) {
		calls++
		if tea.StringValue(in.ScalingGroupId) != "asg-test" || tea.StringValue(in.AdjustmentType) != "QuantityChangeInCapacity" ||
			tea.Int32Value(in.AdjustmentValue) != 2 || !tea.BoolValue(in.SyncActivity) || tea.BoolValue(in.ParallelTask) ||
			len(tea.StringValue(in.ClientToken)) != 64 || tea.BoolValue(runtime.Autoretry) {
			t.Fatal("expansion is not pinned to a synchronous positive adjustment")
		}
		return &ess.ScaleWithAdjustmentResponse{Body: &ess.ScaleWithAdjustmentResponseBody{
			RequestId: tea.String("provider-1"), ScalingActivityId: tea.String("asa-expansion")}}, nil
	}
	target := domain.ProxyTarget{RequestID: "request", EnvCode: "dev", ProxyCode: "proxy", GroupID: "asg-test"}
	cancelled := errors.New("closed request")
	if _, err := p.ExpandPgcat(context.Background(), env, target, "expand", func(context.Context) error { return cancelled }); !errors.Is(err, cancelled) || calls != 0 {
		t.Fatalf("cancelled request crossed provider boundary: %v (%d calls)", err, calls)
	}
	receipt, err := p.ExpandPgcat(context.Background(), env, target, "expand", allowPgcatWrite)
	if err != nil || calls != 1 || receipt.ScalingActivityID != "asa-expansion" || receipt.ProviderRequestID != "provider-1" {
		t.Fatalf("bad activity receipt: %+v %v (%d calls)", receipt, err, calls)
	}
	p.sdk.activities = func(_ context.Context, in *ess.DescribeScalingActivitiesRequest, _ *dara.RuntimeOptions) (*ess.DescribeScalingActivitiesResponse, error) {
		if tea.StringValue(in.ScalingGroupId) != "asg-test" {
			t.Fatal("activity read broadened beyond the pinned group")
		}
		return &ess.DescribeScalingActivitiesResponse{Body: &ess.DescribeScalingActivitiesResponseBody{
			ScalingActivities: []*ess.DescribeScalingActivitiesResponseBodyScalingActivities{
				{ScalingActivityId: tea.String("asa-other"), ScalingGroupId: tea.String("asg-test"), StatusCode: tea.String("Successful")},
				{ScalingActivityId: tea.String("asa-expansion"), ScalingGroupId: tea.String("asg-test"), StatusCode: tea.String("Successful"),
					CreatedCapacity: tea.Int32(2), CreatedInstances: tea.StringSlice([]string{"new-2", "new-1"}), EndTime: tea.String("finished")},
			}}}, nil
	}
	observed, err := p.ObservePgcatExpansion(context.Background(), env, target.GroupID, receipt.ScalingActivityID)
	if err != nil || observed.Status != "Successful" || len(observed.CreatedIDs) != 2 || observed.CreatedIDs[0] != "new-1" {
		t.Fatalf("bad activity observation: %+v %v", observed, err)
	}
}
