package approval

import (
	"context"
	"encoding/json"
	"errors"

	"raptor-iap/infra-api/internal/resources"
)

type GrantTarget struct {
	Kind string `json:"kind"`
	Code string `json:"code"`
}

type GrantCapability struct {
	Capability  string          `json:"capability"`
	Target      GrantTarget     `json:"target"`
	Constraints json.RawMessage `json:"constraints"`
}

type GrantAction struct {
	RequestID   string          `json:"requestId"`
	GrantID     string          `json:"grantId"`
	ActionID    string          `json:"actionId"`
	EnvCode     string          `json:"envCode"`
	ScopeDigest string          `json:"scopeDigest"`
	Capability  GrantCapability `json:"capability"`
	Arguments   json.RawMessage `json:"arguments"`
}

type GrantReceipt struct {
	Claimed           bool
	State             string
	ProviderRequestID string
}

// ClaimGrant uses the proof already resolved for this Infra command. A replay
// exposes the existing receipt, but never grants a second submission.
func (c *Client) ClaimGrant(ctx context.Context, action GrantAction, proof resources.Proof) (GrantReceipt, error) {
	digest, err := resources.Digest(proof.Identity)
	if err != nil || digest != proof.ResolutionDigest || digest != action.ScopeDigest || proof.Identity.EnvCode != action.EnvCode {
		return GrantReceipt{}, errors.New("grant resource identity changed")
	}
	in := struct {
		Action GrantAction     `json:"action"`
		Proof  resources.Proof `json:"proof"`
	}{action, proof}
	var out struct {
		Data struct {
			Claimed           *bool  `json:"claimed"`
			State             string `json:"state"`
			ProviderRequestID string `json:"providerRequestId,omitempty"`
		} `json:"data"`
	}
	if err := c.submissionCall(ctx, "/v1/infra/request-grant-claim", in, &out); err != nil {
		return GrantReceipt{}, err
	}
	state, claimed, providerID := out.Data.State, out.Data.Claimed, out.Data.ProviderRequestID
	if claimed == nil || (*claimed && state != "claimed") || len(providerID) > 512 {
		return GrantReceipt{}, errors.New("invalid grant receipt")
	}
	switch state {
	case "claimed", "dispatching", "not_submitted":
		if providerID != "" {
			return GrantReceipt{}, errors.New("invalid grant receipt")
		}
	case "submitted":
		if providerID == "" {
			return GrantReceipt{}, errors.New("invalid grant receipt")
		}
	case "unknown":
	default:
		return GrantReceipt{}, errors.New("invalid grant receipt")
	}
	return GrantReceipt{Claimed: *claimed, State: state, ProviderRequestID: providerID}, nil
}

func (c *Client) DispatchGrant(ctx context.Context, action GrantAction) error {
	var out struct {
		Data struct {
			Dispatched bool `json:"dispatched"`
		} `json:"data"`
	}
	if err := c.submissionCall(ctx, "/v1/infra/request-grant-dispatch", action, &out); err != nil {
		return err
	}
	if !out.Data.Dispatched {
		return errors.New("grant dispatch was not admitted")
	}
	return nil
}

func (c *Client) RecordGrant(ctx context.Context, action GrantAction, outcome, providerID string) error {
	in := struct {
		Action            GrantAction `json:"action"`
		Outcome           string      `json:"outcome"`
		ProviderRequestID string      `json:"providerRequestId,omitempty"`
	}{action, outcome, providerID}
	var out struct {
		Data struct {
			Recorded bool `json:"recorded"`
		} `json:"data"`
	}
	if err := c.submissionCall(ctx, "/v1/infra/request-grant-record", in, &out); err != nil {
		return err
	}
	if !out.Data.Recorded {
		return errors.New("grant receipt was not recorded")
	}
	return nil
}
