package execution

import (
	"context"
	"encoding/json"
	"net/url"
	"reflect"
	"slices"
	"strings"
)

type pgcatCompletionState struct {
	Observations []PgcatObservationRecord `json:"observations"`
	Actions      []struct {
		RequestID         string `json:"requestId"`
		EnvCode           string `json:"envCode"`
		ScopeDigest       string `json:"scopeDigest"`
		ActionID          string `json:"actionId"`
		State             string `json:"state"`
		ProviderRequestID string `json:"providerRequestId"`
		Capability        struct {
			Name   string `json:"capability"`
			Target struct {
				Kind string `json:"kind"`
				Code string `json:"code"`
			} `json:"target"`
		} `json:"capability"`
	} `json:"actions"`
}

func (c HTTPRaptor) VerifyPgcatCompletion(ctx context.Context, binding AttemptBinding, result LiveResult) error {
	if ValidateLiveResult(result, binding) != nil || result.Replacement == nil || binding.ApprovalProtocol != "request-grants-v1" ||
		!result.Replacement.Scope.ResourceValidFor(binding) {
		return ErrInvalid
	}
	scope := result.Replacement.Scope.ResolutionDigest
	route := "/v1/requests/" + url.PathEscape(binding.RequestID) + "/pgcat-request-state?scopeDigest=" + url.QueryEscape(scope)
	raw, err := c.accessCall(ctx, "GET", route, nil)
	if err != nil {
		return err
	}
	var envelope struct {
		Data pgcatCompletionState `json:"data"`
	}
	if json.Unmarshal(raw, &envelope) != nil {
		return ErrInvalid
	}
	return verifyPgcatCompletionState(binding, result.Replacement, envelope.Data)
}

func verifyPgcatCompletionState(binding AttemptBinding, result *ReplacementConvergence, saved pgcatCompletionState) error {
	scope := result.Scope.ResolutionDigest
	verified := map[string]bool{}
	for _, row := range saved.Observations {
		if row.RequestID != binding.RequestID || row.ScopeDigest != scope {
			return ErrInvalid
		}
		switch row.Kind {
		case "traffic", "final-traffic":
			var trusted PgcatResultTraffic
			want := result.Traffic
			if row.Kind == "final-traffic" {
				want = result.FinalTraffic
			}
			if row.Subject != "fleet" || json.Unmarshal(row.Data, &trusted) != nil || !reflect.DeepEqual(*want, trusted) {
				return ErrInvalid
			}
			verified[row.Kind] = true
		case "retirement":
			var trusted PgcatResultRetirement
			want := result.Retirement
			if row.Subject != "fleet" || json.Unmarshal(row.Data, &trusted) != nil || trusted.ActionID != want.ActionID ||
				trusted.ScalingActivityID != want.ScalingActivityID || trusted.Status != want.Status ||
				!slices.Equal(trusted.DestroyedIDs, want.DestroyedIDs) || trusted.ObservedAt.After(result.FinalTraffic.ObservedAt) {
				return ErrInvalid
			}
			verified[row.Kind] = true
		}
	}
	for _, action := range saved.Actions {
		if action.ActionID != result.Retirement.ActionID {
			continue
		}
		provider, activity, ok := strings.Cut(action.ProviderRequestID, ":")
		if action.RequestID != binding.RequestID || action.EnvCode != binding.EnvCode || action.ScopeDigest != scope ||
			action.Capability.Name != "db-proxy.scale-in" || action.Capability.Target.Kind != "db-proxy" ||
			action.Capability.Target.Code != binding.ObjectCode || action.State != "submitted" ||
			!ok || provider == "" || activity != result.Retirement.ScalingActivityID {
			return ErrInvalid
		}
		verified["action"] = true
	}
	if !verified["traffic"] || !verified["final-traffic"] || !verified["retirement"] || !verified["action"] {
		return ErrInvalid
	}
	return nil
}
