package requests

import (
	"context"
	"encoding/json"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/domain"
)

type RestartItem struct {
	Target  domain.RestartTarget `json:"target"`
	State   string               `json:"state"`
	Details json.RawMessage      `json:"details"`
}

func (s *Service) InsertRestartTarget(ctx context.Context, id string, target domain.RestartTarget) error {
	b, e := json.Marshal(target)
	if e != nil {
		return e
	}
	_, e = s.Pool.Exec(ctx, "INSERT INTO raptor.targets(request_id,target_key,target) VALUES($1,$2,$3)", id, TargetKey(target), b)
	return e
}
func (s *Service) RestartItems(ctx context.Context, id string) ([]RestartItem, error) {
	rows, e := s.Pool.Query(ctx, "SELECT target,state,details FROM raptor.targets WHERE request_id=$1 ORDER BY target_key", id)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []RestartItem{}
	for rows.Next() {
		var item RestartItem
		var target []byte
		if e = rows.Scan(&target, &item.State, &item.Details); e != nil {
			return nil, e
		}
		if json.Unmarshal(target, &item.Target) != nil {
			return nil, domain.ErrUnavailable
		}
		out = append(out, item)
	}
	return out, rows.Err()
}
func (s *Service) setRestartState(ctx context.Context, id string, t domain.RestartTarget, state string, details any) error {
	b, e := json.Marshal(details)
	if e != nil {
		return e
	}
	_, e = s.Pool.Exec(ctx, "UPDATE raptor.targets SET state=$3,details=$4 WHERE request_id=$1 AND target_key=$2", id, TargetKey(t), state, b)
	return e
}
