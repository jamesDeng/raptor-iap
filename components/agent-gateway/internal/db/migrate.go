package db

import (
	"context"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jamesDeng/raptor-iap/components/agent-gateway/migrations"
)

func Migrate(ctx context.Context, p *pgxpool.Pool) error {
	tx, err := p.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtext('gateway:migrations'))"); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, "SET LOCAL ROLE gateway_owner"); err != nil {
		return err
	}
	for _, name := range []string{"001_initial.sql", "002_live_runtime.sql", "003_progress_tickets.sql", "004_conversation.sql"} {
		b, readErr := migrations.Files.ReadFile(name)
		if readErr != nil {
			return readErr
		}
		if _, err = tx.Exec(ctx, string(b)); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}
