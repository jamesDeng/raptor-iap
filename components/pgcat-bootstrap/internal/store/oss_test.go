package store

import (
	"context"
	"errors"
	"github.com/jamesDeng/raptor-iap/components/pgcat-bootstrap/internal/config"
	"io"
	"strings"
	"testing"
)

type reader struct {
	response Object
	err      error
}

func (r reader) Get(context.Context, config.Reference) (Object, error) { return r.response, r.err }
func TestFetch(t *testing.T) {
	ref := config.Reference{Bucket: "bucket", Key: "key", Version: "v1"}
	for _, tc := range []struct {
		name, version, encryption, body string
		valid                           bool
	}{
		{"valid", "v1", "AES256", "{}", true}, {"version", "v2", "AES256", "{}", false}, {"plaintext", "v1", "", "{}", false}, {"oversized", "v1", "AES256", strings.Repeat("s", config.MaxBundle+1), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			obj := Object{Body: io.NopCloser(strings.NewReader(tc.body)), Version: tc.version, Encryption: tc.encryption}
			b, e := (Store{Reader: reader{response: obj}}).Fetch(context.Background(), ref)
			if (e == nil) != tc.valid {
				t.Fatalf("unexpected status: %v", e)
			}
			if tc.valid && string(b) != "{}" {
				t.Fatal("wrong content")
			}
		})
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, e := (Store{Reader: reader{}}).Fetch(ctx, ref); e == nil {
		t.Fatal("ignored cancellation")
	}
}

func TestECSReaderConstructionIgnoresStaticEnvironment(t *testing.T) {
	t.Setenv("ALIBABA_CLOUD_ACCESS_KEY_ID", "SYNTHETIC_STATIC")
	t.Setenv("ALIBABA_CLOUD_ACCESS_KEY_SECRET", "SYNTHETIC_STATIC_SECRET")
	r, e := NewECSReader()
	if e != nil || r == nil {
		t.Fatalf("constructor failed: %v", e)
	}
}

type failReader struct{ calls int }

func (r *failReader) Get(context.Context, config.Reference) (Object, error) {
	r.calls++
	return Object{}, errors.New("SYNTHETIC_SECRET signed-url token")
}
func TestRetryErrorsAreSanitized(t *testing.T) {
	r := &failReader{}
	_, e := (Store{Reader: r}).Fetch(context.Background(), config.Reference{})
	if r.calls != 3 || e == nil || strings.Contains(e.Error(), "SECRET") || strings.Contains(e.Error(), "token") {
		t.Fatalf("unsafe retry result: %d %v", r.calls, e)
	}
}

func TestSDKDebugEnvironmentCannotEnableLogging(t *testing.T) {
	t.Setenv("OSS_SDK_LOG_LEVEL", "debug")
	cfg := productionConfig(nil)
	if cfg.LogLevel == nil || *cfg.LogLevel != 0 {
		t.Fatal("SDK logging may expose signed headers")
	}
}
