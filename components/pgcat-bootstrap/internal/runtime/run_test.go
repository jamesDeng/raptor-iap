package runtime

import (
	"context"
	"errors"
	"github.com/jamesDeng/raptor-iap/components/pgcat-bootstrap/internal/config"
	"github.com/jamesDeng/raptor-iap/components/pgcat-bootstrap/internal/install"
	"os"
	"path/filepath"
	"testing"
)

func TestFailurePreventsStart(t *testing.T) {
	for _, stage := range []string{"fetch", "install", "probe"} {
		t.Run(stage, func(t *testing.T) {
			started := false
			d := Dependencies{
				Fetch: func(context.Context, config.Reference) (config.Bundle, error) {
					if stage == "fetch" {
						return config.Bundle{}, errors.New("synthetic_secret")
					}
					return config.Bundle{}, nil
				},
				Install: func(config.Bundle) (install.Paths, error) {
					if stage == "install" {
						return install.Paths{}, errors.New("synthetic_secret")
					}
					return install.Paths{}, nil
				},
				Probe: func(context.Context, config.Bundle, install.Paths) error {
					if stage == "probe" {
						return errors.New("synthetic_secret")
					}
					return nil
				},
				Start: func(context.Context, install.Paths) error { started = true; return nil }}
			if e := Run(context.Background(), config.Metadata{}, d); e == nil || started {
				t.Fatal("started after failure")
			}
		})
	}
}

func TestFailedProbeRemovesGeneration(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "generation")
	if e := os.Mkdir(dir, 0700); e != nil {
		t.Fatal(e)
	}
	d := Dependencies{Fetch: func(context.Context, config.Reference) (config.Bundle, error) { return config.Bundle{}, nil }, Install: func(config.Bundle) (install.Paths, error) { return install.Paths{Directory: dir}, nil }, Probe: func(context.Context, config.Bundle, install.Paths) error { return errors.New("unavailable") }, Start: func(context.Context, install.Paths) error { t.Fatal("started"); return nil }}
	if e := Run(context.Background(), config.Metadata{}, d); e == nil {
		t.Fatal("unexpected success")
	}
	if _, e := os.Stat(dir); !os.IsNotExist(e) {
		t.Fatal("failed generation remains")
	}
}
