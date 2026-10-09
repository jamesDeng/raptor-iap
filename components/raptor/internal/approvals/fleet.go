package approvals

import (
	"context"
	"encoding/json"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/domain"
	"strings"
)

type FleetInput struct {
	EnvCode string          `json:"envCode"`
	GroupID string          `json:"groupId"`
	Token   string          `json:"token"`
	Intent  json.RawMessage `json:"intent,omitempty"`
}
type FleetResult struct {
	Recorded bool            `json:"recorded"`
	Claimed  bool            `json:"claimed"`
	Token    string          `json:"token"`
	Intent   json.RawMessage `json:"intent"`
}

func fleetValid(in FleetInput) bool {
	for _, v := range []string{in.EnvCode, in.GroupID, in.Token} {
		if v == "" || len(v) > 256 || strings.TrimSpace(v) != v {
			return false
		}
	}
	return true
}

// Claims never expire: process death and provider uncertainty retain exclusion.
// Only the trusted Infra service may resolve after authoritative reconciliation.
func (s *Service) FleetClaim(ctx context.Context, in FleetInput) (FleetResult, error) {
	out := FleetResult{}
	if !fleetValid(in) || len(in.Intent) == 0 || len(in.Intent) > 4096 || !json.Valid(in.Intent) {
		return out, domain.ErrInvalid
	}
	tx, e := s.Pool.Begin(ctx)
	if e != nil {
		return out, domain.ErrUnavailable
	}
	defer tx.Rollback(ctx)
	tag, e := tx.Exec(ctx, `INSERT INTO raptor.fleet_mutations(env_code,group_id,token,intent) VALUES($1,$2,$3,$4) ON CONFLICT DO NOTHING`, in.EnvCode, in.GroupID, in.Token, in.Intent)
	if e != nil {
		return out, domain.ErrUnavailable
	}
	e = tx.QueryRow(ctx, `SELECT token,intent,recorded FROM raptor.fleet_mutations WHERE env_code=$1 AND group_id=$2`, in.EnvCode, in.GroupID).Scan(&out.Token, &out.Intent, &out.Recorded)
	if e != nil {
		return out, domain.ErrUnavailable
	}
	out.Claimed = tag.RowsAffected() == 1
	if e = tx.Commit(ctx); e != nil {
		return FleetResult{}, domain.ErrUnavailable
	}
	return out, nil
}
func (s *Service) FleetResolve(ctx context.Context, in FleetInput) error {
	if !fleetValid(in) {
		return domain.ErrInvalid
	}
	tag, e := s.Pool.Exec(ctx, `DELETE FROM raptor.fleet_mutations WHERE env_code=$1 AND group_id=$2 AND token=$3`, in.EnvCode, in.GroupID, in.Token)
	if e != nil {
		return domain.ErrUnavailable
	}
	if tag.RowsAffected() != 1 {
		return domain.ErrConflict
	}
	return nil
}

func (s *Service) FleetRecord(ctx context.Context, in FleetInput) error {
	if !fleetValid(in) {
		return domain.ErrInvalid
	}
	tag, e := s.Pool.Exec(ctx, `UPDATE raptor.fleet_mutations SET recorded=true WHERE env_code=$1 AND group_id=$2 AND token=$3`, in.EnvCode, in.GroupID, in.Token)
	if e != nil {
		return domain.ErrUnavailable
	}
	if tag.RowsAffected() != 1 {
		return domain.ErrConflict
	}
	return nil
}
