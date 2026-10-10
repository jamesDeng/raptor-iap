package modelproviders

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestConnectNodeFlowUsesPrivatePipe(t *testing.T) {
	node, e := exec.LookPath("node")
	if e != nil {
		t.Skip("node unavailable")
	}
	helper := filepath.Join(t.TempDir(), "helper.mjs")
	script := `process.stdout.write(JSON.stringify({type:'challenge',verificationUrl:'https://auth.openai.com/codex/device',userCode:'ABCD-EFGH',expiresAt:new Date(Date.now()+60000).toISOString()})+'\n');setTimeout(()=>process.stdout.write(JSON.stringify({type:'credential',credential:{type:'oauth',access:'synthetic',refresh:'secret-fixture',expires:12345}})+'\n'),10);`
	if e = os.WriteFile(helper, []byte(script), 0600); e != nil {
		t.Fatal(e)
	}
	attempt, e := NewNodeFlow(node, helper).Start(context.Background())
	if e != nil {
		t.Fatal(e)
	}
	if attempt.Challenge().UserCode != "ABCD-EFGH" {
		t.Fatalf("bad challenge: %+v", attempt.Challenge())
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	raw, e := attempt.Await(ctx)
	if e != nil || !strings.Contains(string(raw), "secret-fixture") {
		t.Fatalf("credential not delivered privately: %v", e)
	}
}

func TestConnectNodeFlowRejectsUntrustedChallenge(t *testing.T) {
	node, e := exec.LookPath("node")
	if e != nil {
		t.Skip("node unavailable")
	}
	helper := filepath.Join(t.TempDir(), "helper.mjs")
	if e = os.WriteFile(helper, []byte(`process.stdout.write(JSON.stringify({type:'challenge',verificationUrl:'https://evil.example/device',userCode:'code',expiresAt:new Date(Date.now()+60000).toISOString()})+'\n')`), 0600); e != nil {
		t.Fatal(e)
	}
	if _, e = NewNodeFlow(node, helper).Start(context.Background()); e == nil {
		t.Fatal("untrusted challenge accepted")
	}
}
