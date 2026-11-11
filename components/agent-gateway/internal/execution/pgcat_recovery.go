package execution

import (
	"context"
	"encoding/json"
	"net/url"
	"strings"
	"time"
)

type PgcatJournalAction struct {
	RequestID         string          `json:"requestId"`
	ScopeDigest       string          `json:"scopeDigest"`
	EnvCode           string          `json:"envCode"`
	ActionID          string          `json:"actionId"`
	State             string          `json:"state"`
	ProviderRequestID string          `json:"providerRequestId"`
	ClaimedAt         time.Time       `json:"claimedAt"`
	RecordedAt        time.Time       `json:"recordedAt"`
	Arguments         json.RawMessage `json:"arguments"`
	Capability        struct {
		Name   string                      `json:"capability"`
		Target struct{ Kind, Code string } `json:"target"`
	} `json:"capability"`
}

type PgcatRequestJournal struct {
	Observations []PgcatObservationRecord `json:"observations"`
	Actions      []PgcatJournalAction     `json:"actions"`
}

func (c HTTPRaptor) ReadPgcatRequestJournal(ctx context.Context, b AttemptBinding, scope ReplacementScope) (PgcatRequestJournal, error) {
	var response struct {
		Data PgcatRequestJournal `json:"data"`
	}
	route := "/v1/requests/" + url.PathEscape(b.RequestID) + "/pgcat-request-state?scopeDigest=" + url.QueryEscape(scope.ResolutionDigest)
	err := c.liveRead(ctx, route, &response)
	return response.Data, err
}

// ReadPgcatPreDrainHold reads the original cohort from the authenticated
// request journal. A pending or uncertain drain also forbids recapture.
func (c HTTPRaptor) ReadPgcatPreDrainHold(ctx context.Context, b AttemptBinding, scope ReplacementScope) (json.RawMessage, error) {
	journal, err := c.ReadPgcatRequestJournal(ctx, b, scope)
	if err != nil {
		return nil, err
	}
	expanded := false
	for _, action := range journal.Actions {
		if action.RequestID != b.RequestID || action.ScopeDigest != scope.ResolutionDigest || action.EnvCode != scope.EnvCode {
			return nil, ErrInvalid
		}
		switch action.Capability.Name {
		case "db-proxy.drain", "application.restart", "db-proxy.scale-in":
			return nil, ErrInvalid
		case "db-proxy.scale-out":
			if action.State != "submitted" {
				continue
			}
			provider, activity, ok := strings.Cut(action.ProviderRequestID, ":")
			if expanded || !ok || provider == "" || !strings.HasPrefix(activity, "asa-") || action.RecordedAt.IsZero() ||
				action.Capability.Target.Kind != "db-proxy" || action.Capability.Target.Code != scope.ProxyCode {
				return nil, ErrInvalid
			}
			expanded = true
		}
	}
	if !expanded {
		return nil, nil
	}
	var hold json.RawMessage
	for _, observation := range journal.Observations {
		switch observation.Kind {
		case "drain", "probe-release", "traffic", "final-traffic", "retirement":
			return nil, ErrInvalid
		case "hold":
			if hold != nil || observation.RequestID != b.RequestID || observation.ScopeDigest != scope.ResolutionDigest ||
				observation.Subject != "fleet" || observation.ObservedAt.IsZero() {
				return nil, ErrInvalid
			}
			hold = observation.Data
		}
	}
	if hold == nil {
		return nil, ErrInvalid
	}
	return hold, nil
}
