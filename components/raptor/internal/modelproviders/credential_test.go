package modelproviders

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jamesDeng/raptor-iap/components/raptor/internal/db"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/testutil"
)

func TestCredentialKeyRequiresPrivateRegularFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "key")
	if e := os.WriteFile(path, bytes.Repeat([]byte{9}, 32), 0600); e != nil {
		t.Fatal(e)
	}
	key, e := ReadEncryptionKey(path)
	if e != nil || len(key) != 32 {
		t.Fatal("private key rejected")
	}
	if e = os.Chmod(path, 0644); e != nil {
		t.Fatal(e)
	}
	if _, e = ReadEncryptionKey(path); e == nil {
		t.Fatal("world-readable key accepted")
	}
	if e = os.Chmod(path, 0600); e != nil {
		t.Fatal(e)
	}
	link := filepath.Join(t.TempDir(), "link")
	if e = os.Symlink(path, link); e != nil {
		t.Fatal(e)
	}
	if _, e = ReadEncryptionKey(link); e == nil {
		t.Fatal("symlink key accepted")
	}
}

func TestCredentialEncryptionBindsProviderAndKey(t *testing.T) {
	key := bytes.Repeat([]byte{7}, 32)
	secret := []byte(`{"type":"oauth","access":"private-access","refresh":"private-refresh","expires":12345}`)
	encrypted, e := EncryptCredential(secret, key, "v1", ProviderID("codex"))
	if e != nil {
		t.Fatal(e)
	}
	if bytes.Contains(encrypted.Data, []byte("private-refresh")) {
		t.Fatal("plaintext in ciphertext")
	}
	clear, e := DecryptCredential(encrypted, key, ProviderID("codex"))
	if e != nil || !bytes.Equal(clear, secret) {
		t.Fatal("round trip failed")
	}
	if _, e = DecryptCredential(encrypted, bytes.Repeat([]byte{8}, 32), ProviderID("codex")); e == nil {
		t.Fatal("wrong key accepted")
	}
	if _, e = DecryptCredential(encrypted, key, ProviderID("other")); e == nil {
		t.Fatal("ciphertext moved across providers")
	}
}

func TestCredentialStoreSerializesRefreshAndPreservesOldOnFailure(t *testing.T) {
	ctx := context.Background()
	pool := testutil.Database(t)
	if e := db.Migrate(ctx, pool); e != nil {
		t.Fatal(e)
	}
	store := NewCredentialStore(pool, bytes.Repeat([]byte{3}, 32), "v1")
	first := []byte(`{"type":"oauth","access":"access-one","refresh":"refresh-one","expires":2000000000000}`)
	record, e := store.PutConnectedCredential(ctx, "codex", "owner", first)
	if e != nil || record.ConnectionVersion != 1 || record.Generation != 1 {
		t.Fatalf("initial save: %+v %v", record, e)
	}
	var raw []byte
	if e = pool.QueryRow(ctx, "SELECT ciphertext FROM raptor.model_provider_credentials WHERE provider_id='codex'").Scan(&raw); e != nil || bytes.Contains(raw, []byte("refresh-one")) {
		t.Fatal("credential persisted in cleartext")
	}
	loaded, e := store.LoadCredential(ctx, "codex")
	if e != nil || !bytes.Equal(loaded.Credential, first) {
		t.Fatal("load mismatch")
	}
	second := []byte(`{"type":"oauth","access":"access-two","refresh":"refresh-two","expires":2000000000000}`)
	next, e := store.ReplaceCredential(ctx, "codex", 1, 1, second)
	if e != nil || next.Generation != 2 || next.ConnectionVersion != 1 {
		t.Fatalf("refresh: %+v %v", next, e)
	}
	if _, e = store.ReplaceCredential(ctx, "codex", 1, 1, first); !errors.Is(e, ErrStaleGeneration) {
		t.Fatalf("stale refresh: %v", e)
	}
	if _, e = store.ReplaceCredential(ctx, "codex", 1, 2, []byte("invalid")); e == nil {
		t.Fatal("bad refresh accepted")
	}
	loaded, e = store.LoadCredential(ctx, "codex")
	if e != nil || !bytes.Equal(loaded.Credential, second) {
		t.Fatal("failed refresh replaced valid token")
	}
	if !loaded.ExpiresAt.After(time.Now()) {
		t.Fatal("expiry lost")
	}
	connected, e := store.PutConnectedCredential(ctx, "codex", "owner", first)
	if e != nil || connected.ConnectionVersion != 2 {
		t.Fatal("reauthorization did not advance connection version")
	}
	if _, e = store.ReplaceCredential(ctx, "codex", 1, 2, second); !errors.Is(e, ErrStaleGeneration) {
		t.Fatal("old connection refreshed newly authorized account")
	}
}
