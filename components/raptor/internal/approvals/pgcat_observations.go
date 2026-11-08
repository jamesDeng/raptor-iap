package approvals

import (
	"context"
	"encoding/json"
	"time"

	"github.com/jamesDeng/raptor-iap/components/raptor/internal/domain"
)

// PgcatObservation contains data recorded by the controller-side observer.
// Only its authenticated service route can write it.
type PgcatObservation struct {
	RequestID   string          `json:"requestId"`
	AttemptID   string          `json:"attemptId"`
	ScopeDigest string          `json:"scopeDigest"`
	Kind        string          `json:"kind"`
	Subject     string          `json:"subject"`
	ObservedAt  time.Time       `json:"observedAt"`
	Data        json.RawMessage `json:"data"`
}

func (s *Service) RecordPgcatObservation(ctx context.Context, in PgcatObservation) error {
	now := time.Now()
	if (!grantUUID(in.RequestID) || !validGrantDigest(in.ScopeDigest)) || !grantUUID(in.AttemptID) || !grantIdentifier(in.Subject) ||
		in.ObservedAt.IsZero() || in.ObservedAt.After(now.Add(2*time.Second)) || now.Sub(in.ObservedAt) > 30*time.Second {
		return domain.ErrInvalid
	}
	switch in.Kind {
	case "hold", "drain", "probe-release", "traffic", "final-traffic":
		if in.Subject != "fleet" {
			return domain.ErrInvalid
		}
	case "sql":
	default:
		return domain.ErrInvalid
	}
	data, err := canonicalGrantObject(in.Data)
	if err != nil || len(data) > 4096 {
		return domain.ErrInvalid
	}
	if in.Kind == "hold" || in.Kind == "drain" || in.Kind == "probe-release" {
		var proof struct {
			ObservationID string `json:"observationId"`
			RequestID     string `json:"requestId"`
			ScopeDigest   string `json:"scopeDigest"`
		}
		if json.Unmarshal(data, &proof) != nil || !grantUUID(proof.ObservationID) ||
			proof.RequestID != in.RequestID || proof.ScopeDigest != in.ScopeDigest {
			return domain.ErrInvalid
		}
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return domain.ErrUnavailable
	}
	defer tx.Rollback(ctx)
	state, err := lockGrantRequest(ctx, tx, in.RequestID)
	if err != nil {
		return err
	}
	if state.kind != "agent" || state.control != "" || state.status != "running" {
		return domain.ErrConflict
	}

	var active bool
	err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM raptor.agent_attempt_access
		WHERE request_id=$1 AND attempt_id=$2 AND NOT revoked AND expires_at>clock_timestamp()
		  AND binding->>'approvalProtocol'='request-grants-v1'
		  AND binding->>'executionMode'='replacement' AND replacement_scope->>'resolutionDigest'=$3)`,
		in.RequestID, in.AttemptID, in.ScopeDigest).Scan(&active)
	if err != nil {
		return domain.ErrUnavailable
	}
	if !active {
		return domain.ErrForbidden
	}

	tag, err := tx.Exec(ctx, `INSERT INTO raptor.pgcat_observations AS saved
		(request_id,attempt_id,scope_digest,kind,subject,observed_at,data) VALUES($1,$2,$3,$4,$5,$6,$7)
		ON CONFLICT (request_id,kind,subject) DO UPDATE SET attempt_id=excluded.attempt_id,
			observed_at=excluded.observed_at,data=excluded.data
		WHERE saved.scope_digest=excluded.scope_digest AND
		  ((saved.observed_at < excluded.observed_at AND
		    saved.data->>'observationId' IS NOT DISTINCT FROM excluded.data->>'observationId')
		    OR (saved.observed_at=excluded.observed_at AND saved.data=excluded.data))`,
		in.RequestID, in.AttemptID, in.ScopeDigest, in.Kind, in.Subject, in.ObservedAt, data)
	if err != nil {
		return domain.ErrUnavailable
	}
	if tag.RowsAffected() != 1 {
		return domain.ErrConflict
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.ErrUnavailable
	}
	return nil
}

func (s *Service) PgcatObservations(ctx context.Context, requestID, scopeDigest string) ([]PgcatObservation, error) {
	if !grantUUID(requestID) || !validGrantDigest(scopeDigest) {
		return nil, domain.ErrInvalid
	}
	rows, err := s.Pool.Query(ctx, `SELECT attempt_id::text,kind,subject,observed_at,data
		FROM raptor.pgcat_observations WHERE request_id=$1 AND scope_digest=$2
		ORDER BY kind,subject LIMIT 17`, requestID, scopeDigest)
	if err != nil {
		return nil, domain.ErrUnavailable
	}
	defer rows.Close()
	result := make([]PgcatObservation, 0)
	for rows.Next() {
		out := PgcatObservation{RequestID: requestID, ScopeDigest: scopeDigest}
		if err := rows.Scan(&out.AttemptID, &out.Kind, &out.Subject, &out.ObservedAt, &out.Data); err != nil {
			return nil, domain.ErrUnavailable
		}
		result = append(result, out)
	}
	if rows.Err() != nil || len(result) > 16 {
		return nil, domain.ErrUnavailable
	}
	return result, nil
}

type PgcatRequestState struct {
	Observations []PgcatObservation   `json:"observations"`
	Actions      []GrantActionDetails `json:"actions"`
}

func (s *Service) ReadPgcatRequestState(ctx context.Context, requestID, scopeDigest string) (PgcatRequestState, error) {
	observations, err := s.PgcatObservations(ctx, requestID, scopeDigest)
	if err != nil {
		return PgcatRequestState{}, err
	}
	rows, err := s.Pool.Query(ctx, `SELECT a.grant_id::text,a.action_id,g.env_code,a.capability,a.arguments,a.state,
		a.provider_request_id,a.claimed_at,a.recorded_at FROM raptor.request_grant_actions a
		JOIN raptor.request_grants g ON g.request_id=a.request_id AND g.id=a.grant_id
		WHERE a.request_id=$1 AND g.scope_digest=$2 ORDER BY a.claimed_at,a.action_id LIMIT 33`, requestID, scopeDigest)
	if err != nil {
		return PgcatRequestState{}, domain.ErrUnavailable
	}
	defer rows.Close()
	out := PgcatRequestState{Observations: observations, Actions: []GrantActionDetails{}}
	for rows.Next() {
		item := GrantActionDetails{RequestID: requestID, ScopeDigest: scopeDigest}
		if err := rows.Scan(&item.GrantID, &item.ActionID, &item.EnvCode, &item.Capability, &item.Arguments, &item.State,
			&item.ProviderRequestID, &item.ClaimedAt, &item.RecordedAt); err != nil {
			return PgcatRequestState{}, domain.ErrUnavailable
		}
		out.Actions = append(out.Actions, item)
	}
	if rows.Err() != nil || len(out.Actions) > 32 {
		return PgcatRequestState{}, domain.ErrUnavailable
	}
	return out, nil
}
