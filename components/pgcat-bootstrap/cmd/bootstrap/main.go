package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"github.com/jamesDeng/raptor-iap/components/pgcat-bootstrap/internal/config"
	"github.com/jamesDeng/raptor-iap/components/pgcat-bootstrap/internal/install"
	"github.com/jamesDeng/raptor-iap/components/pgcat-bootstrap/internal/probe"
	run "github.com/jamesDeng/raptor-iap/components/pgcat-bootstrap/internal/runtime"
	"github.com/jamesDeng/raptor-iap/components/pgcat-bootstrap/internal/store"
	"io"
	"os"
	"syscall"
	"time"
)

var revision string

func readMetadata(path string) ([]byte, error) {
	f, e := os.Open(path)
	if e != nil {
		return nil, errors.New("metadata_unreadable")
	}
	defer f.Close()
	b, e := io.ReadAll(io.LimitReader(f, 16385))
	if e != nil || len(b) > 16384 {
		return nil, errors.New("metadata_invalid")
	}
	return b, nil
}
func execute(path string) error {
	if os.Getuid() == 0 {
		return errors.New("service_user_required")
	}
	data, e := readMetadata(path)
	if e != nil {
		return e
	}
	m, e := config.ParseMetadata(data, revision)
	if e != nil {
		return e
	}
	reader, e := store.NewECSReader()
	if e != nil {
		return e
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	return run.Run(ctx, m, run.Dependencies{
		Fetch: func(ctx context.Context, ref config.Reference) (config.Bundle, error) {
			b, e := (store.Store{Reader: reader}).Fetch(ctx, ref)
			if e != nil {
				return config.Bundle{}, e
			}
			return config.ParseBundle(b, m)
		},
		Install: func(b config.Bundle) (install.Paths, error) {
			// This artifact implements only the owner-approved plaintext POC mode.
			b.ServerCertificate = ""
			b.ServerPrivateKey = ""
			b.ClientCA = ""
			b.BackendCA = ""
			return install.Install("/var/lib/raptor-pgcat", b, os.Getuid(), os.Getgid(), time.Now())
		},
		Probe: func(ctx context.Context, b config.Bundle, _ install.Paths) error { return probe.Check(ctx, b) },
		Start: func(_ context.Context, p install.Paths) error {
			// Replace the bootstrap so systemd supervises PgCat and signals it directly.
			env := []string{"PATH=/usr/bin:/bin", "RUST_LOG=off"}
			if e := syscall.Exec("/opt/raptor-pgcat/pgcat", []string{"pgcat", p.Config}, env); e != nil {
				return errors.New("pgcat_start_failed")
			}
			return nil
		},
	})
}
func main() {
	path := flag.String("config", "/etc/raptor-pgcat/bootstrap.json", "nonsecret metadata file")
	flag.Parse()
	if e := execute(*path); e != nil {
		fmt.Fprintln(os.Stderr, e.Error())
		os.Exit(1)
	}
}
