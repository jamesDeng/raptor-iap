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

func TestBrowserNodeFlowAcceptsOnlyLocalCallback(t *testing.T) {
	node, e := exec.LookPath("node")
	if e != nil {
		t.Skip("node unavailable")
	}
	helper := filepath.Join(t.TempDir(), "helper.mjs")
	script := `process.stdout.write(JSON.stringify({type:'browser_challenge',authorizationUrl:'https://auth.openai.com/oauth/authorize?state=state-1&client_id=synthetic&code_challenge=synthetic&redirect_uri=http%3A%2F%2Flocalhost%3A1455%2Fauth%2Fcallback',expiresAt:new Date(Date.now()+60000).toISOString()})+'\n');for await(const line of process.stdin){process.stdout.write(JSON.stringify({type:'credential',credential:{type:'oauth',access:'synthetic',refresh:'secret-fixture',expires:12345}})+'\n');break}`
	if e = os.WriteFile(helper, []byte(script), 0600); e != nil {
		t.Fatal(e)
	}
	attempt, e := NewNodeFlow(node, helper).StartBrowser(context.Background())
	if e != nil {
		t.Fatal(e)
	}
	if attempt.Challenge().AuthorizationURL == "" {
		t.Fatal("missing authorization URL")
	}
	if e = attempt.SubmitRedirect("https://evil.example/auth/callback?code=bad&state=state-1"); e == nil {
		t.Fatal("accepted foreign callback")
	}
	if e = attempt.SubmitRedirect("http://localhost:1455/auth/callback?code=one-time&state=wrong"); e == nil {
		t.Fatal("accepted wrong state")
	}
	if e = attempt.SubmitRedirect("http://localhost:1455/auth/callback?code=one-time&state=state-1"); e != nil {
		t.Fatal(e)
	}
	if e = attempt.SubmitRedirect("http://localhost:1455/auth/callback?code=one-time&state=state-1"); e == nil {
		t.Fatal("accepted repeated callback")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, e = attempt.Await(ctx); e != nil {
		t.Fatal(e)
	}
}
