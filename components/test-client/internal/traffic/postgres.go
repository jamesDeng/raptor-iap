package traffic

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"time"
)

type PostgresSession struct{ conn *pgx.Conn }

func ConnectPostgres(ctx context.Context, dsn string) (Session, error) {
	conn, e := pgx.Connect(ctx, dsn)
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
func (s *PostgresSession) Operate(ctx context.Context, id string) Outcome {
	tx, e := s.conn.Begin(ctx)
	if e != nil {
		return statementOutcome(e)
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = tx.Rollback(cleanup)
	}()
	if _, e = tx.Exec(ctx, "INSERT INTO infra_test_operations(id) VALUES($1)", id); e != nil {
		return statementOutcome(e)
	}
	var actual string
	if e = tx.QueryRow(ctx, "SELECT id FROM infra_test_operations WHERE id=$1", id).Scan(&actual); e != nil {
		return statementOutcome(e)
	}
	if actual != id {
		return Failure
	}
	return commitOutcome(tx.Commit(ctx))
}
func (s *PostgresSession) Close() {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_ = s.conn.Close(ctx)
}
