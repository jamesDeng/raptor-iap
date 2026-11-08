package execution

import (
	"context"
	"encoding/json"
	"net/url"
	"strconv"
	"strings"
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

type PgcatObservationRecord struct {
	RequestID   string          `json:"requestId"`
	AttemptID   string          `json:"attemptId"`
	ScopeDigest string          `json:"scopeDigest"`
	Kind        string          `json:"kind"`
	Subject     string          `json:"subject"`
	ObservedAt  time.Time       `json:"observedAt"`
	Data        json.RawMessage `json:"data"`
}

func (c HTTPRaptor) RecordPgcatObservation(ctx context.Context, evidence PgcatObservationRecord) error {
	body, err := json.Marshal(evidence)
	if err != nil || len(body) > 8192 {
		return ErrInvalid
	}
	route := "/v1/requests/" + url.PathEscape(evidence.RequestID) + "/pgcat-observations"
	raw, err := c.accessCall(ctx, "POST", route, body)
	if err != nil {
		return err
	}
	var out struct {
		Data struct {
			Recorded bool `json:"recorded"`
		}
	}
	if json.Unmarshal(raw, &out) != nil || !out.Data.Recorded {
		return ErrUnavailable
	}
	return nil
}

type PgcatRestartAction struct {
	Generation int64
	ClaimedAt  time.Time
	RecordedAt time.Time
}

func (c HTTPRaptor) ReadPgcatRestartAction(ctx context.Context, requestID, env, app, uid, scopeDigest, actionID string) (PgcatRestartAction, error) {
	var out struct {
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
				Capability string                      `json:"capability"`
				Target     struct{ Kind, Code string } `json:"target"`
			} `json:"capability"`
		}
	}
	route := "/v1/requests/" + url.PathEscape(requestID) + "/grant-actions/" + url.PathEscape(actionID)
	if err := c.liveRead(ctx, route, &out); err != nil {
		return PgcatRestartAction{}, err
	}
	a := out.Data
	generation, err := strconv.ParseInt(strings.TrimPrefix(a.ProviderRequestID, uid+":"), 10, 64)
	if err != nil || generation < 1 || !strings.HasPrefix(a.ProviderRequestID, uid+":") || a.RequestID != requestID ||
		a.ActionID != actionID || a.EnvCode != env || a.ScopeDigest != scopeDigest || a.State != "submitted" ||
		a.ClaimedAt.IsZero() || a.RecordedAt.Before(a.ClaimedAt) || a.RecordedAt.After(time.Now().Add(2*time.Second)) ||
		a.Capability.Capability != "application.restart" || a.Capability.Target.Kind != "application" || a.Capability.Target.Code != app {
		return PgcatRestartAction{}, ErrInvalid
	}
	return PgcatRestartAction{Generation: generation, ClaimedAt: a.ClaimedAt, RecordedAt: a.RecordedAt}, nil
}
