package githubcredentials

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"

	"github.com/aliyun/aliyun-oss-go-sdk/oss"
)

type fakeBucket struct{ objects map[string][]byte }

func (b *fakeBucket) PutObject(key string, in io.Reader, _ ...oss.Option) error {
	if b.objects == nil {
		b.objects = map[string][]byte{}
	}
	raw, _ := io.ReadAll(in)
	b.objects[key] = raw
	return nil
}
func (b *fakeBucket) GetObject(key string, _ ...oss.Option) (io.ReadCloser, error) {
	raw, ok := b.objects[key]
	if !ok {
		return nil, errors.New("missing")
	}
	return io.NopCloser(bytes.NewReader(raw)), nil
}
func (b *fakeBucket) DeleteObject(key string, _ ...oss.Option) error {
	delete(b.objects, key)
	return nil
}
func TestOSSSecretsRoundTripAndRejectsForeignKey(t *testing.T) {
	ctx := context.Background()
	bucket := &fakeBucket{}
	store := OSSSecrets{Bucket: bucket}
	if err := store.Put(ctx, "github-pat/a", []byte("github_pat_fixture")); err != nil {
		t.Fatal(err)
	}
	raw, err := store.Get(ctx, "github-pat/a")
	if err != nil || string(raw) != "github_pat_fixture" {
		t.Fatalf("read failed: %v", err)
	}
	if err = store.Put(ctx, "other/a", []byte("x")); err == nil {
		t.Fatal("foreign prefix accepted")
	}
	if err = store.Delete(ctx, "github-pat/a"); err != nil || len(bucket.objects) != 0 {
		t.Fatalf("delete failed: %v", err)
	}
}
