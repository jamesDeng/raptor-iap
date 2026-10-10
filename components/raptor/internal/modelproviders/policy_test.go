package modelproviders

import (
	"context"
	"errors"
	"testing"

	"github.com/jamesDeng/raptor-iap/components/raptor/internal/db"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/testutil"
)

func TestModelPolicyRequiresExplicitEnablementAndCurrentCatalog(t *testing.T) {
	ctx := context.Background()
	pool := testutil.Database(t)
	if err := db.Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	store := NewCredentialStore(pool, make([]byte, 32), "v1")
	if _, err := store.PutConnectedCredential(ctx, "codex", "owner", []byte(`{"type":"oauth","access":"a","refresh":"r","expires":2000000000000}`)); err != nil {
		t.Fatal(err)
	}
	p := PolicyStore{Pool: pool}
	if err := p.ReplaceCatalog(ctx, "codex", []DiscoveredModel{{ModelID: "gpt-6-sol", DisplayName: "GPT 6 Sol"}, {ModelID: "gpt-6-luna", DisplayName: "GPT 6 Luna"}}); err != nil {
		t.Fatal(err)
	}
	models, err := p.ListEnabled(ctx)
	if err != nil || len(models) != 0 {
		t.Fatalf("unexpected default enablement: %v %v", models, err)
	}
	var adminID string
	if err = pool.QueryRow(ctx, `INSERT INTO raptor.users(id,username,password_hash,role) VALUES(gen_random_uuid(),'policy-admin','x','admin') RETURNING id`).Scan(&adminID); err != nil {
		t.Fatal(err)
	}
	if err = p.SetPolicy(ctx, adminID, "codex", 0, []ModelID{"unknown"}, "unknown"); !errors.Is(err, ErrInvalidModel) {
		t.Fatalf("unknown enabled: %v", err)
	}
	if err = p.SetPolicy(ctx, adminID, "codex", 0, []ModelID{"gpt-6-sol"}, "gpt-6-sol"); err != nil {
		t.Fatal(err)
	}
	if _, err = p.ValidateSelection(ctx, "codex", "gpt-6-luna"); !errors.Is(err, ErrInvalidModel) {
		t.Fatalf("disabled model accepted: %v", err)
	}
	if version, err := p.ValidateSelection(ctx, "codex", "gpt-6-sol"); err != nil || version != 1 {
		t.Fatalf("enabled: %d %v", version, err)
	}
	if err = p.SetPolicy(ctx, adminID, "codex", 0, []ModelID{"gpt-6-luna"}, "gpt-6-luna"); !errors.Is(err, ErrPolicyConflict) {
		t.Fatalf("stale policy accepted: %v", err)
	}
	if err = p.ReplaceCatalog(ctx, "codex", []DiscoveredModel{{ModelID: "gpt-6-luna", DisplayName: "GPT 6 Luna"}}); err != nil {
		t.Fatal(err)
	}
	models, err = p.ListEnabled(ctx)
	if err != nil || len(models) != 0 {
		t.Fatalf("unavailable model listed: %v %v", models, err)
	}
}
