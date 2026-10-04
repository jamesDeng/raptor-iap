package db

import (
	"context"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jamesDeng/raptor-iap/components/raptor/migrations"
)

func Migrate(ctx context.Context, p *pgxpool.Pool) error {
	tx, err := p.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtext('raptor:migrations'))"); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, "SET LOCAL ROLE raptor_owner"); err != nil {
		return err
	}
	b, err := migrations.Files.ReadFile("001_initial.sql")
	if err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, string(b)); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
