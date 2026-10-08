package probe

import (
	"github.com/jamesDeng/raptor-iap/components/pgcat-bootstrap/internal/config"
	"testing"
)

func TestPOCConnectionConfiguration(t *testing.T) {
	t.Setenv("PGHOST", "unexpected")
	t.Setenv("PGPASSWORD", "unexpected")
	b := config.Bundle{DBHost: "db.internal", Database: "test", AppUser: "app", AppPassword: "synthetic"}
	c, e := ConnectionConfig(b)
	if e != nil {
		t.Fatal(e)
	}
	if c.Host != b.DBHost || c.Database != b.Database || c.User != b.AppUser || c.Password != b.AppPassword || c.Port != 5432 || c.TLSConfig != nil || len(c.Fallbacks) != 0 {
		t.Fatal("unexpected connection target/transport")
	}
}
