package provider

import (
	"context"
	"errors"
	ess "github.com/alibabacloud-go/ess-20220222/v2/client"
	nlb "github.com/alibabacloud-go/nlb-20220430/v4/client"
	"github.com/alibabacloud-go/tea/dara"
	"github.com/alibabacloud-go/tea/tea"
	"strings"
	"testing"
)

func TestProxyScaleAcknowledgementAndUncertainty(t *testing.T) {
	for _, kind := range []string{"ack", "timeout", "missing ack", "cancelled", "foreign group"} {
		t.Run(kind, func(t *testing.T) {
			p, env, _, _, _, _ := proxyFixture(t)
			calls := 0
			p.sdk.scale = func(_ context.Context, r *ess.ModifyScalingGroupRequest, o *dara.RuntimeOptions) (*ess.ModifyScalingGroupResponse, error) {
				calls++
				if tea.StringValue(r.ScalingGroupId) != "asg-test" || r.DesiredCapacity == nil || *r.DesiredCapacity != 3 || r.MinSize != nil || r.MaxSize != nil || o.Autoretry == nil || *o.Autoretry {
					t.Fatal("unsafe scale request")
				}
				if kind == "timeout" {
					return nil, errors.New("secret provider error")
				}
				if kind == "missing ack" {
					return &ess.ModifyScalingGroupResponse{Body: &ess.ModifyScalingGroupResponseBody{}}, nil
				}
				return &ess.ModifyScalingGroupResponse{Body: &ess.ModifyScalingGroupResponseBody{RequestId: tea.String("request-123")}}, nil
			}
			ctx := context.Background()
			group := "asg-test"
			if kind == "cancelled" {
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}
			if kind == "foreign group" {
				group = "asg-other"
			}
			ack, e := p.Scale(ctx, env, group, 3)
			if kind == "ack" {
				if e != nil || ack != "request-123" || calls != 1 {
					t.Fatalf("ack=%q calls=%d error=%v", ack, calls, e)
				}
			} else {
				if e == nil || ack != "" || strings.Contains(e.Error(), "secret") {
					t.Fatalf("unsafe uncertainty: ack=%q error=%v", ack, e)
				}
				want := 1
				if kind == "cancelled" || kind == "foreign group" {
					want = 0
				}
				if calls != want {
					t.Fatalf("calls=%d want=%d", calls, want)
				}
			}
		})
	}
}
func TestProxyProtectAndDeregisterUsePinnedTargets(t *testing.T) {
	p, env, _, _, _, _ := proxyFixture(t)
	protected, removed := 0, 0
	p.sdk.protect = func(_ context.Context, r *ess.SetInstancesProtectionRequest, o *dara.RuntimeOptions) (*ess.SetInstancesProtectionResponse, error) {
		protected++
		if tea.StringValue(r.ScalingGroupId) != "asg-test" || len(r.InstanceIds) != 1 || tea.StringValue(r.InstanceIds[0]) != "i-two" || r.ProtectedFromScaleIn == nil || *r.ProtectedFromScaleIn || o.Autoretry == nil || *o.Autoretry {
			t.Fatal("unsafe protection request")
		}
		return &ess.SetInstancesProtectionResponse{Body: &ess.SetInstancesProtectionResponseBody{RequestId: tea.String("protect-1")}}, nil
	}
	p.sdk.remove = func(_ context.Context, r *nlb.RemoveServersFromServerGroupRequest, o *dara.RuntimeOptions) (*nlb.RemoveServersFromServerGroupResponse, error) {
		removed++
		if tea.StringValue(r.RegionId) != "ap-southeast-1" || tea.StringValue(r.ServerGroupId) != "sgp-test" || len(r.Servers) != 1 || tea.StringValue(r.Servers[0].ServerId) != "i-two" || tea.StringValue(r.Servers[0].ServerType) != "Ecs" || r.Servers[0].Port == nil || *r.Servers[0].Port != 6432 || o.Autoretry == nil || *o.Autoretry {
			t.Fatal("unsafe deregistration request")
		}
		return &nlb.RemoveServersFromServerGroupResponse{Body: &nlb.RemoveServersFromServerGroupResponseBody{RequestId: tea.String("remove-1"), JobId: tea.String("job-1"), ServerGroupId: tea.String("sgp-test")}}, nil
	}
	if e := p.Protect(context.Background(), env, "asg-test", []string{"i-two"}, false); e != nil {
		t.Fatal(e)
	}
	if e := p.Deregister(context.Background(), env, "sgp-test", []string{"i-two"}); e != nil {
		t.Fatal(e)
	}
	for _, ids := range [][]string{nil, {"i-foreign"}, {"i-two", "i-two"}} {
		if e := p.Protect(context.Background(), env, "asg-test", ids, false); e == nil {
			t.Fatal("unsafe protection accepted")
		}
		if e := p.Deregister(context.Background(), env, "sgp-test", ids); e == nil {
			t.Fatal("unsafe removal accepted")
		}
	}
	if protected != 1 || removed != 1 {
		t.Fatalf("unexpected mutations: %d/%d", protected, removed)
	}
}
