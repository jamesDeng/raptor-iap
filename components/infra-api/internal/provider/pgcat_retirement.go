package provider

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"slices"
	"time"

	ess "github.com/alibabacloud-go/ess-20220222/v2/client"
	"github.com/alibabacloud-go/tea/tea"
	"raptor-iap/infra-api/internal/domain"
	"raptor-iap/infra-api/internal/policy"
)

func (p *ProxyBackend) RetirePgcat(ctx context.Context, env domain.Environment, in domain.PgcatRetirement, admit func(context.Context) error) (domain.PgcatScalingReceipt, error) {
	var out domain.PgcatScalingReceipt
	if p.sdk.retire == nil {
		return out, domain.ErrNotConfigured
	}
	if in.RequestID == "" || in.ActionID == "" || in.EnvCode != env.Code {
		return out, domain.ErrScope
	}
	groups, err := p.Snapshot(ctx, env, in.ProxyCode)
	if err != nil {
		return out, err
	}
	snapshot, err := policy.ResolveProxy(groups, env.Code, in.ProxyCode, in.GroupID)
	if err != nil {
		return out, err
	}
	if err := policy.TargetedPgcatRetirement(snapshot, in.OriginalIDs, in.ReplacementIDs); err != nil {
		return out, err
	}
	if ctx.Err() != nil {
		return out, ctx.Err()
	}
	ids := slices.Clone(in.OriginalIDs)
	slices.Sort(ids)
	if err := admit(ctx); err != nil {
		return out, err
	}
	result, err := p.sdk.retire(ctx, &ess.RemoveInstancesRequest{
		RegionId: tea.String(env.Region), ScalingGroupId: tea.String(snapshot.GroupID),
		InstanceIds: tea.StringSlice(ids), RemovePolicy: tea.String("release"),
		DecreaseDesiredCapacity: tea.Bool(true), IgnoreInvalidInstance: tea.Bool(false),
		ClientToken: tea.String(pgcatActivityToken(in.RequestID, in.ActionID)),
	}, noRetry())
	if err != nil || result == nil || result.Body == nil || tea.StringValue(result.Body.RequestId) == "" ||
		tea.StringValue(result.Body.ScalingActivityId) == "" || len(result.Body.IgnoredInstances) != 0 {
		return out, &domain.CommandError{Code: "SubmissionUnknown"}
	}
	return domain.PgcatScalingReceipt{ProviderRequestID: *result.Body.RequestId, ScalingActivityID: *result.Body.ScalingActivityId}, nil
}

func pgcatActivityToken(requestID, actionID string) string {
	digest := sha256.Sum256([]byte(requestID + "\x00" + actionID))
	return hex.EncodeToString(digest[:])
}

func (p *reader) ObservePgcatRetirement(ctx context.Context, env domain.Environment, target domain.ProxyReadTarget, activity string) (domain.PgcatRetirementObservation, error) {
	var result domain.PgcatRetirementObservation
	if p.proxy == nil || p.proxy.sdk.activities == nil {
		return result, domain.ErrNotConfigured
	}
	mapping, _, err := p.observationGroup(ctx, env, target)
	if err != nil {
		return result, err
	}
	out, err := p.proxy.sdk.activities(ctx, &ess.DescribeScalingActivitiesRequest{RegionId: tea.String(env.Region),
		ScalingGroupId: tea.String(mapping.GroupID), PageNumber: tea.Int32(1), PageSize: tea.Int32(50)}, noRetry())
	if err != nil || out == nil || out.Body == nil || len(out.Body.ScalingActivities) > 50 {
		return result, providerRefusal()
	}
	result = domain.PgcatRetirementObservation{ScalingActivityID: activity, Status: "Pending", ObservedAt: time.Now().UTC(), DestroyedIDs: []string{}}
	found := false
	for _, row := range out.Body.ScalingActivities {
		if row == nil || tea.StringValue(row.ScalingActivityId) != activity {
			continue
		}
		if found || tea.StringValue(row.ScalingGroupId) != mapping.GroupID {
			return domain.PgcatRetirementObservation{}, domain.ErrScope
		}
		found = true
		result.Status = tea.StringValue(row.StatusCode)
		switch result.Status {
		case "Pending", "InProgress", "Failed", "Warning", "Rejected":
		case "Successful":
			if row.DestroyedCapacity == nil || *row.DestroyedCapacity != 2 || len(row.DestroyedInstances) != 2 || tea.StringValue(row.EndTime) == "" {
				return domain.PgcatRetirementObservation{}, providerRefusal()
			}
			result.DestroyedIDs = tea.StringSliceValue(row.DestroyedInstances)
			slices.Sort(result.DestroyedIDs)
			if result.DestroyedIDs[0] == "" || result.DestroyedIDs[0] == result.DestroyedIDs[1] {
				return domain.PgcatRetirementObservation{}, providerRefusal()
			}
		default:
			return domain.PgcatRetirementObservation{}, providerRefusal()
		}
	}
	return result, nil
}
