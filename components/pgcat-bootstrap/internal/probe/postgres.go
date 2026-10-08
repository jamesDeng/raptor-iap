package probe

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5"
	"github.com/jamesDeng/raptor-iap/components/pgcat-bootstrap/internal/config"
	"net/url"
	"time"
)

func ConnectionConfig(b config.Bundle) (*pgx.ConnConfig, error) {
	u := url.URL{Scheme: "postgres", Host: b.DBHost + ":5432", Path: "/" + b.Database, User: url.UserPassword(b.AppUser, b.AppPassword), RawQuery: "sslmode=disable&connect_timeout=5"}
	c, e := pgx.ParseConfig(u.String())
	if e != nil {
		return nil, errors.New("probe_configuration_failed")
	}
	c.Fallbacks = nil
	c.RuntimeParams = map[string]string{"application_name": "raptor-pgcat-bootstrap"}
	return c, nil
}
func Check(ctx context.Context, b config.Bundle) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	c, e := ConnectionConfig(b)
	if e != nil {
		return e
	}
	conn, e := pgx.ConnectConfig(ctx, c)
	if e != nil {
		return errors.New("database_unavailable")
	}
	defer func() {
		closeCtx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		conn.Close(closeCtx)
	}()
	var value int
	if e := conn.QueryRow(ctx, "SELECT 1").Scan(&value); e != nil || value != 1 {
		return errors.New("database_probe_failed")
	}
	return nil
}
