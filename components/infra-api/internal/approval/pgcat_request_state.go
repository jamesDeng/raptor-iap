package approval

import (
	"context"
	"encoding/json"
	"time"
)

type PgcatObservation struct {
	RequestID   string          `json:"requestId"`
	AttemptID   string          `json:"attemptId"`
	ScopeDigest string          `json:"scopeDigest"`
	Kind        string          `json:"kind"`
	Subject     string          `json:"subject"`
	ObservedAt  time.Time       `json:"observedAt"`
	Data        json.RawMessage `json:"data"`
}

type GrantActionDetails struct {
	RequestID         string          `json:"requestId"`
	GrantID           string          `json:"grantId"`
	ActionID          string          `json:"actionId"`
	EnvCode           string          `json:"envCode"`
	ScopeDigest       string          `json:"scopeDigest"`
	Capability        GrantCapability `json:"capability"`
	Arguments         json.RawMessage `json:"arguments"`
	State             string          `json:"state"`
	ProviderRequestID string          `json:"providerRequestId,omitempty"`
	ClaimedAt         time.Time       `json:"claimedAt"`
	RecordedAt        *time.Time      `json:"recordedAt,omitempty"`
}

type PgcatRequestState struct {
	Observations []PgcatObservation   `json:"observations"`
	Actions      []GrantActionDetails `json:"actions"`
}

func (c *Client) ReadPgcatRequestState(ctx context.Context, requestID, scopeDigest string) (PgcatRequestState, error) {
	var out struct {
		Data PgcatRequestState `json:"data"`
	}
	input := map[string]string{"requestId": requestID, "scopeDigest": scopeDigest}
	if err := c.submissionCallLimit(ctx, "/v1/infra/pgcat-request-state", input, &out, 65536); err != nil {
		return PgcatRequestState{}, err
	}
	return out.Data, nil
}
