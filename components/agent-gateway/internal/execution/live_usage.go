package execution

import (
	"bytes"
	"context"
	"encoding/json"
)

func (s *Store) SaveSegmentUsage(ctx context.Context, attempt, owner string, usage []Usage) error {
	if len(usage) > 10 {
		return ErrInvalid
	}
	for _, u := range usage {
		if u.Input < 0 || u.Output < 0 || u.TotalTokens < 0 || u.Input > 10000000 || u.Output > 10000000 || u.TotalTokens > 10000000 {
			return ErrInvalid
		}
	}
	if usage == nil {
		usage = []Usage{}
	}
	tx, _, e := s.liveTx(ctx, attempt, owner)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	if _, e = bindingFrom(ctx, tx, attempt); e != nil {
		return e
	}
	raw, _ := json.Marshal(usage)
	var previous []byte
	if e = tx.QueryRow(ctx, "SELECT segment_usage FROM gateway.attempts WHERE id=$1", attempt).Scan(&previous); e != nil {
		return e
	}
	if len(previous) > 0 {
		var saved []Usage
		if json.Unmarshal(previous, &saved) != nil {
			return ErrInvalid
		}
		normalized, _ := json.Marshal(saved)
		if !bytes.Equal(raw, normalized) {
			return ErrInvalid
		}
		return tx.Commit(ctx)
	}
	if _, e = tx.Exec(ctx, "UPDATE gateway.attempts SET segment_usage=$2 WHERE id=$1", attempt, raw); e != nil {
		return e
	}
	return tx.Commit(ctx)
}
func (s *Store) RequestUsage(ctx context.Context, request string) (Usage, error) {
	var u Usage
	e := s.Pool.QueryRow(ctx, `SELECT COALESCE(SUM((v->>'input')::bigint),0),COALESCE(SUM((v->>'output')::bigint),0),COALESCE(SUM((v->>'totalTokens')::bigint),0) FROM gateway.attempts a CROSS JOIN LATERAL jsonb_array_elements(a.segment_usage) v WHERE request_id=$1`, request).Scan(&u.Input, &u.Output, &u.TotalTokens)
	return u, e
}
