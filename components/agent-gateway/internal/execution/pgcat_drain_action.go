package execution

import (
	"context"
	"encoding/json"
	"net/url"
	"time"
)

type PgcatDrainAction struct {
	NodeIDs    []string
	RecordedAt time.Time
}

// ReadPgcatDrainAction retrieves the action already admitted and acknowledged
// by Infra. A guessed action ID or a claimed/unknown write supplies no evidence.
func (c HTTPRaptor) ReadPgcatDrainAction(ctx context.Context, requestID, env, proxy, scopeDigest, actionID string) (PgcatDrainAction, error) {
	var response struct {
		Data struct {
			RequestID         string    `json:"requestId"`
			ActionID          string    `json:"actionId"`
			EnvCode           string    `json:"envCode"`
			ScopeDigest       string    `json:"scopeDigest"`
			State             string    `json:"state"`
			ProviderRequestID string    `json:"providerRequestId"`
			ClaimedAt         time.Time `json:"claimedAt"`
			RecordedAt        time.Time `json:"recordedAt"`
			Capability        struct {
				Capability string `json:"capability"`
				Target     struct {
					Kind string `json:"kind"`
					Code string `json:"code"`
				} `json:"target"`
			} `json:"capability"`
			Arguments json.RawMessage `json:"arguments"`
		} `json:"data"`
	}
	route := "/v1/requests/" + url.PathEscape(requestID) + "/grant-actions/" + url.PathEscape(actionID)
	if err := c.liveRead(ctx, route, &response); err != nil {
		return PgcatDrainAction{}, err
	}
	a := response.Data
	if a.RequestID != requestID || a.ActionID != actionID || a.EnvCode != env || a.ScopeDigest != scopeDigest ||
		a.State != "submitted" || a.ProviderRequestID == "" || a.ClaimedAt.IsZero() || a.RecordedAt.Before(a.ClaimedAt) ||
		a.RecordedAt.After(time.Now().Add(2*time.Second)) || a.Capability.Capability != "db-proxy.drain" ||
		a.Capability.Target.Kind != "db-proxy" || a.Capability.Target.Code != proxy {
		return PgcatDrainAction{}, ErrInvalid
	}
	var args struct {
		RequestID string   `json:"requestId"`
		EnvCode   string   `json:"envCode"`
		ProxyCode string   `json:"proxyCode"`
		NodeIDs   []string `json:"nodeIds"`
	}
	if json.Unmarshal(a.Arguments, &args) != nil || args.RequestID != requestID || args.EnvCode != env || args.ProxyCode != proxy ||
		len(args.NodeIDs) != 2 || args.NodeIDs[0] == args.NodeIDs[1] || !journalName.MatchString(args.NodeIDs[0]) || !journalName.MatchString(args.NodeIDs[1]) {
		return PgcatDrainAction{}, ErrInvalid
	}
	return PgcatDrainAction{NodeIDs: args.NodeIDs, RecordedAt: a.RecordedAt}, nil
}
