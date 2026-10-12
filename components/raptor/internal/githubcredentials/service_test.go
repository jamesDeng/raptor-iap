package githubcredentials

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/jamesDeng/raptor-iap/components/raptor/internal/db"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/testutil"
)

type memorySecrets struct {
	values      map[string][]byte
	deleteFails bool
}

func (m *memorySecrets) Put(_ context.Context, key string, value []byte) error {
	if m.values == nil {
		m.values = map[string][]byte{}
	}
	m.values[key] = append([]byte(nil), value...)
	return nil
}
func (m *memorySecrets) Get(_ context.Context, key string) ([]byte, error) {
	v, ok := m.values[key]
	if !ok {
		return nil, errors.New("missing")
	}
	return append([]byte(nil), v...), nil
}
func (m *memorySecrets) Delete(_ context.Context, key string) error {
	if m.deleteFails {
		return errors.New("synthetic delete failure")
	}
	delete(m.values, key)
	return nil
}

func TestSaveReplaceDeleteNeverReturnsPAT(t *testing.T) {
	ctx := context.Background()
	pool := testutil.Database(t)
	if err := db.Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	secrets := &memorySecrets{}
	service := Service{Pool: pool, Secrets: secrets, Verify: func(_ context.Context, token string) (string, error) {
		if !strings.HasPrefix(token, "github_pat_") {
			return "", errors.New("invalid")
		}
		return "owner", nil
	}}
	first, err := service.Save(ctx, "admin", "github_pat_first")
	serialized, _ := json.Marshal(first)
	if err != nil || !first.Configured || first.Owner != "owner" || strings.Contains(string(serialized), "github_pat_first") {
		t.Fatalf("save failed or leaked PAT: %+v %v", first, err)
	}
	loaded, err := service.Load(ctx)
	if err != nil || string(loaded) != "github_pat_first" {
		t.Fatalf("load failed: %v", err)
	}
	second, err := service.Save(ctx, "admin", "github_pat_second")
	if err != nil || !second.Configured {
		t.Fatalf("replace failed: %v", err)
	}
	loaded, err = service.Load(ctx)
	if err != nil || string(loaded) != "github_pat_second" {
		t.Fatalf("replacement lost: %v", err)
	}
	if len(secrets.values) != 1 {
		t.Fatalf("stale PAT object retained: %d", len(secrets.values))
	}
	if err = service.Delete(ctx, "admin"); err != nil {
		t.Fatal(err)
	}
	status, err := service.Status(ctx)
	if err != nil || status.Configured || len(secrets.values) != 0 {
		t.Fatalf("delete retained PAT: %+v %v", status, err)
	}
}

func TestFailedSecretDeleteKeepsReferenceForRetry(t *testing.T) {
	ctx := context.Background()
	pool := testutil.Database(t)
	if err := db.Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	secrets := &memorySecrets{}
	service := Service{Pool: pool, Secrets: secrets, Verify: func(context.Context, string) (string, error) { return "owner", nil }}
	if _, err := service.Save(ctx, "admin", "github_pat_first"); err != nil {
		t.Fatal(err)
	}
	secrets.deleteFails = true
	if err := service.Delete(ctx, "admin"); err == nil {
		t.Fatal("secret delete failure reported success")
	}
	status, err := service.Status(ctx)
	if err != nil || !status.Configured {
		t.Fatalf("failed delete lost retriable reference: %+v %v", status, err)
	}
	secrets.deleteFails = false
	if err = service.Delete(ctx, "admin"); err != nil {
		t.Fatalf("retry failed: %v", err)
	}
}

func TestReplacementCleanupRemainsRetryable(t *testing.T) {
	ctx := context.Background()
	pool := testutil.Database(t)
	if err := db.Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	secrets := &memorySecrets{}
	service := Service{Pool: pool, Secrets: secrets, Verify: func(context.Context, string) (string, error) { return "owner", nil }}
	if _, err := service.Save(ctx, "admin", "github_pat_first"); err != nil {
		t.Fatal(err)
	}
	secrets.deleteFails = true
	if _, err := service.Save(ctx, "admin", "github_pat_second"); err == nil {
		t.Fatal("stale object cleanup failure reported success")
	}
	if len(secrets.values) != 2 {
		t.Fatalf("expected one stale object pending cleanup, got %d", len(secrets.values))
	}
	secrets.deleteFails = false
	if err := service.Cleanup(ctx); err != nil {
		t.Fatal(err)
	}
	if len(secrets.values) != 1 {
		t.Fatalf("stale PAT object not removed: %d", len(secrets.values))
	}
}
