package traffic

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"net/netip"
	"time"
)

type PostgresSession struct{ conn *pgx.Conn }

func ConnectPostgres(ctx context.Context, dsn string) (Session, error) {
	cfg, e := postgresConfig(dsn)
	if e != nil {
		return nil, e
	}
	conn, e := pgx.ConnectConfig(ctx, cfg)
	if e != nil {
		return nil, e
	}
	return &PostgresSession{conn}, nil
}
func statementOutcome(err error) Outcome {
	if errors.Is(err, context.DeadlineExceeded) {
		return Timeout
	}
	return Failure
}
func commitOutcome(err error) Outcome {
	if err == nil {
		return Success
	}
	var pe *pgconn.PgError
	if errors.As(err, &pe) {
		switch pe.Code {
		case "40001", "40P01", "25P02", "23505":
			return Failure
		}
	}
	return Ambiguous
}
func (s *PostgresSession) Operate(ctx context.Context, id string) OperationResult {
	tx, e := s.conn.Begin(ctx)
	if e != nil {
		return OperationResult{Outcome: statementOutcome(e)}
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = tx.Rollback(cleanup)
	}()
	if _, e = tx.Exec(ctx, "INSERT INTO infra_test_operations(id) VALUES($1)", id); e != nil {
		return OperationResult{Outcome: statementOutcome(e)}
	}
	var actual, proxy string
	if e = tx.QueryRow(ctx, "SELECT id, COALESCE(host(inet_client_addr()), '') FROM infra_test_operations WHERE id=$1", id).Scan(&actual, &proxy); e != nil {
		return OperationResult{Outcome: statementOutcome(e)}
	}
	if actual != id {
		return OperationResult{Outcome: Failure}
	}
	outcome := commitOutcome(tx.Commit(ctx))
	result := OperationResult{Outcome: outcome}
	if outcome == Success {
		result.ProxyIP, _ = netip.ParseAddr(proxy)
	}
	return result
}
func (s *PostgresSession) Close() {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_ = s.conn.Close(ctx)
}

// Named statement caches are connection-local and incompatible with the POC's
// PgCat transaction pooling. Exec retains parameter binding without named caches.
func postgresConfig(dsn string) (*pgx.ConnConfig, error) {
	cfg, err := pgx.ParseConfig(dsn)
	if err != nil {
		return nil, err
	}
	cfg.DefaultQueryExecMode = pgx.QueryExecModeExec
	return cfg, nil
}
