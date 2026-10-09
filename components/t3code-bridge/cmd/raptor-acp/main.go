package main

import (
	"context"
	"flag"
	"fmt"
	"github.com/jamesDeng/raptor-iap/components/t3code-bridge/internal/acp"
	"github.com/jamesDeng/raptor-iap/components/t3code-bridge/internal/bridge"
	"github.com/jamesDeng/raptor-iap/components/t3code-bridge/internal/config"
	"github.com/jamesDeng/raptor-iap/components/t3code-bridge/internal/raptor"
	"github.com/jamesDeng/raptor-iap/components/t3code-bridge/internal/state"
	"os"
	"os/signal"
	"sync"
	"syscall"
)

func run() error {
	p := flag.String("config", "", "Absolute path to private bridge configuration")
	flag.Parse()
	cfg, e := config.Load(*p)
	if e != nil {
		return e
	}
	c, e := raptor.NewClient(cfg)
	if e != nil {
		return e
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	a := &lazyAgent{cfg: cfg, client: c}
	defer a.close()
	return acp.Serve(ctx, os.Stdin, os.Stdout, a)
}
func main() {
	if e := run(); e != nil {
		fmt.Fprintln(os.Stderr, "Raptor ACP bridge:", e)
		os.Exit(1)
	}
}

// T3 keeps discovery processes alive; they must not own session state.
type lazyAgent struct {
	mu     sync.Mutex
	cfg    config.Config
	client *raptor.Client
	runner *bridge.Runner
	store  *state.Store
}

func (a *lazyAgent) get() (*bridge.Runner, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.runner != nil {
		return a.runner, nil
	}
	s, e := state.Open(a.cfg.StateDir)
	if e != nil {
		return nil, e
	}
	a.store = s
	a.runner = bridge.New(a.client, s, a.cfg)
	return a.runner, nil
}
func (a *lazyAgent) close() {
	if a.store != nil {
		a.store.Close()
	}
}
func (a *lazyAgent) NewSession(c context.Context) (string, error) {
	r, e := a.get()
	if e != nil {
		return "", e
	}
	return r.NewSession(c)
}
func (a *lazyAgent) LoadSession(c context.Context, id string, emit func(bridge.Update) error) error {
	r, e := a.get()
	if e != nil {
		return e
	}
	return r.LoadSession(c, id, emit)
}
func (a *lazyAgent) Prompt(c context.Context, id, text string, emit func(bridge.Update) error) (bridge.Outcome, error) {
	return a.PromptReady(c, id, text, emit, func() {})
}
func (a *lazyAgent) PromptReady(c context.Context, id, text string, emit func(bridge.Update) error, ready func()) (bridge.Outcome, error) {
	r, e := a.get()
	if e != nil {
		ready()
		return bridge.Outcome{}, e
	}
	return r.PromptReady(c, id, text, emit, ready)
}
func (a *lazyAgent) Cancel(c context.Context, id string) error {
	r, e := a.get()
	if e != nil {
		return e
	}
	return r.Cancel(c, id)
}
