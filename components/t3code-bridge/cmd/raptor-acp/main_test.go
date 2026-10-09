package main

import (
	"context"
	"github.com/jamesDeng/raptor-iap/components/t3code-bridge/internal/acp"
	"github.com/jamesDeng/raptor-iap/components/t3code-bridge/internal/config"
	"github.com/jamesDeng/raptor-iap/components/t3code-bridge/internal/state"
	"io"
	"strings"
	"testing"
)

func TestDiscoveryDoesNotOwnState(t *testing.T) {
	dir := t.TempDir() + "/state"
	owner, e := state.Open(dir)
	if e != nil {
		t.Fatal(e)
	}
	defer owner.Close()
	a := &lazyAgent{cfg: config.Config{StateDir: dir}}
	defer a.close()
	if e = acp.Serve(context.Background(), strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":1}}`+"\n"), io.Discard, a); e != nil {
		t.Fatal(e)
	}
	if a.store != nil {
		t.Fatal("discovery acquired state")
	}
	if _, e = a.get(); e == nil {
		t.Fatal("session bypassed exclusive owner")
	}
}
