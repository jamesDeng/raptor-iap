package runtime

import (
	"context"
	"errors"
	"github.com/jamesDeng/raptor-iap/components/pgcat-bootstrap/internal/config"
	"github.com/jamesDeng/raptor-iap/components/pgcat-bootstrap/internal/install"
	"os"
)

type Dependencies struct {
	Fetch   func(context.Context, config.Reference) (config.Bundle, error)
	Install func(config.Bundle) (install.Paths, error)
	Probe   func(context.Context, config.Bundle, install.Paths) error
	Start   func(context.Context, install.Paths) error
}

func Run(ctx context.Context, m config.Metadata, d Dependencies) error {
	if d.Fetch == nil || d.Install == nil || d.Probe == nil || d.Start == nil {
		return errors.New("runtime_configuration_failed")
	}
	b, e := d.Fetch(ctx, m.Reference)
	if e != nil {
		return errors.New("bootstrap_retrieval_failed")
	}
	p, e := d.Install(b)
	if e != nil {
		return errors.New("bootstrap_installation_failed")
	}
	started := false
	defer func() {
		if !started && p.Directory != "" {
			_ = os.RemoveAll(p.Directory)
		}
	}()
	if e = d.Probe(ctx, b, p); e != nil {
		return errors.New("bootstrap_database_probe_failed")
	}
	if ctx.Err() != nil {
		return errors.New("bootstrap_cancelled")
	}
	e = d.Start(ctx, p)
	started = e == nil
	return e
}
