package execution

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5"
)

func (s *Store) AllowApplyRetry(ctx context.Context, id, run string) (bool, error) {
	if run == "" || len(run) > 200 {
		return false, ErrInvalid
	}
	var n int
	e := s.Pool.QueryRow(ctx, "INSERT INTO gateway.apply_retries(request_id,original_run_id,count) VALUES($1,$2,1) ON CONFLICT(request_id,original_run_id) DO UPDATE SET count=gateway.apply_retries.count+1 WHERE gateway.apply_retries.count<2 RETURNING count", id, run).Scan(&n)
	if errors.Is(e, pgx.ErrNoRows) {
		return false, nil
	}
	return e == nil, e
}
