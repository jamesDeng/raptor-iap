package approval

import (
	"context"
	"errors"
)

type FleetIntent struct {
	EnvCode     string   `json:"envCode"`
	ProxyCode   string   `json:"proxyCode"`
	GroupID     string   `json:"groupId"`
	Kind        string   `json:"kind"`
	Desired     int      `json:"desired"`
	InstanceIDs []string `json:"instanceIds"`
	Protected   bool     `json:"protected"`
}
type FleetResult struct {
	Recorded bool        `json:"recorded"`
	Claimed  bool        `json:"claimed"`
	Token    string      `json:"token"`
	Intent   FleetIntent `json:"intent"`
}

func (c *Client) FleetClaim(ctx context.Context, in FleetIntent, token string) (FleetResult, error) {
	var out struct {
		Data FleetResult `json:"data"`
	}
	e := c.submissionCall(ctx, "/v1/fleet-claim", map[string]any{"envCode": in.EnvCode, "groupId": in.GroupID, "token": token, "intent": in}, &out)
	if e != nil {
		return FleetResult{}, e
	}
	if out.Data.Token == "" {
		return FleetResult{}, errors.New("fleet unavailable")
	}
	return out.Data, nil
}
func (c *Client) FleetResolve(ctx context.Context, in FleetIntent, token string) error {
	var out struct {
		Data struct {
			Resolved bool `json:"resolved"`
		} `json:"data"`
	}
	e := c.submissionCall(ctx, "/v1/fleet-resolve", map[string]any{"envCode": in.EnvCode, "groupId": in.GroupID, "token": token}, &out)
	if e != nil {
		return e
	}
	if !out.Data.Resolved {
		return errors.New("fleet unavailable")
	}
	return nil
}

func (c *Client) FleetRecord(ctx context.Context, in FleetIntent, token string) error {
	var out struct {
		Data struct {
			Recorded bool `json:"recorded"`
		} `json:"data"`
	}
	e := c.submissionCall(ctx, "/v1/fleet-record", map[string]any{"envCode": in.EnvCode, "groupId": in.GroupID, "token": token}, &out)
	if e != nil {
		return e
	}
	if !out.Data.Recorded {
		return errors.New("fleet unavailable")
	}
	return nil
}
