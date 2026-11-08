package provider

import (
	"context"
	"slices"
	"time"

	ess "github.com/alibabacloud-go/ess-20220222/v2/client"
	"github.com/alibabacloud-go/tea/tea"
	"raptor-iap/infra-api/internal/domain"
	"raptor-iap/infra-api/internal/policy"
)

func (p *ProxyBackend) ExpandPgcat(ctx context.Context, env domain.Environment, target domain.ProxyTarget, actionID string, admit func(context.Context) error) (domain.PgcatScalingReceipt, error) {
	var out domain.PgcatScalingReceipt
	if p.sdk.expand == nil {
		return out, domain.ErrNotConfigured
	}
	if target.RequestID == "" || actionID == "" || target.EnvCode != env.Code {
		return out, domain.ErrScope
	}
	groups, err := p.Snapshot(ctx, env, target.ProxyCode)
	if err != nil {
		return out, err
	}
	snapshot, err := policy.ResolveProxy(groups, env.Code, target.ProxyCode, target.GroupID)
	if err != nil {
		return out, err
	}
	if snapshot.Desired != 2 || snapshot.Min != 2 || snapshot.Max != 4 || len(snapshot.Nodes) != 2 {
		return out, &domain.CommandError{Code: "TargetChanged"}
	}
	if ctx.Err() != nil {
		return out, ctx.Err()
	}
	// Use an immediate scale activity so the provider returns an ID for the
	// exact creation event. A positive adjustment cannot scale down the group.
	if err := admit(ctx); err != nil {
		return out, err
	}
	response, err := p.sdk.expand(ctx, &ess.ScaleWithAdjustmentRequest{
		ScalingGroupId: tea.String(snapshot.GroupID), AdjustmentType: tea.String("QuantityChangeInCapacity"),
		AdjustmentValue: tea.Int32(2), SyncActivity: tea.Bool(true), ParallelTask: tea.Bool(false),
		ClientToken: tea.String(pgcatActivityToken(target.RequestID, actionID)),
	}, noRetry())
	if err != nil || response == nil || response.Body == nil || tea.StringValue(response.Body.RequestId) == "" ||
		tea.StringValue(response.Body.ScalingActivityId) == "" || tea.StringValue(response.Body.ActivityType) == "CapacityChange" {
		return out, &domain.CommandError{Code: "SubmissionUnknown"}
	}
	return domain.PgcatScalingReceipt{ProviderRequestID: *response.Body.RequestId, ScalingActivityID: *response.Body.ScalingActivityId}, nil
}

func (p *ProxyBackend) ObservePgcatExpansion(ctx context.Context, env domain.Environment, groupID, activity string) (domain.PgcatExpansionObservation, error) {
	result := domain.PgcatExpansionObservation{ScalingActivityID: activity, Status: "Pending", ObservedAt: time.Now().UTC(), CreatedIDs: []string{}}
	if p.sdk.activities == nil {
		return result, domain.ErrNotConfigured
	}
	if _, err := p.target(ctx, env, groupID, false); err != nil {
		return result, err
	}
	out, err := p.sdk.activities(ctx, &ess.DescribeScalingActivitiesRequest{RegionId: tea.String(env.Region),
		ScalingGroupId: tea.String(groupID), PageNumber: tea.Int32(1), PageSize: tea.Int32(50)}, noRetry())
	if err != nil || out == nil || out.Body == nil || len(out.Body.ScalingActivities) > 50 {
		return result, providerRefusal()
	}
	found := false
	for _, row := range out.Body.ScalingActivities {
		if row == nil || tea.StringValue(row.ScalingActivityId) != activity {
			continue
		}
		if found || tea.StringValue(row.ScalingGroupId) != groupID {
			return result, domain.ErrScope
		}
		found = true
		result.Status = tea.StringValue(row.StatusCode)
		switch result.Status {
		case "Pending", "InProgress", "Failed", "Warning", "Rejected":
		case "Successful":
			if row.CreatedCapacity == nil || *row.CreatedCapacity != 2 || len(row.CreatedInstances) != 2 || tea.StringValue(row.EndTime) == "" {
				return result, providerRefusal()
			}
			result.CreatedIDs = tea.StringSliceValue(row.CreatedInstances)
			slices.Sort(result.CreatedIDs)
			if result.CreatedIDs[0] == "" || result.CreatedIDs[0] == result.CreatedIDs[1] {
				return result, providerRefusal()
			}
		default:
			return result, providerRefusal()
		}
	}
	return result, nil
}

func (p *reader) ObservePgcatExpansion(ctx context.Context, env domain.Environment, groupID, activity string) (domain.PgcatExpansionObservation, error) {
	if p.proxy == nil {
		return domain.PgcatExpansionObservation{}, domain.ErrNotConfigured
	}
	return p.proxy.ObservePgcatExpansion(ctx, env, groupID, activity)
}
