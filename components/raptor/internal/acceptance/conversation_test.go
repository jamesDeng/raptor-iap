package acceptance

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/backend"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/db"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/domain"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/requests"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/testutil"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestConversationEndToEndOutboxAndReplay(t *testing.T) {
	ctx := context.Background()
	p := testutil.Database(t)
	if e := db.Migrate(ctx, p); e != nil {
		t.Fatal(e)
	}
	binary := filepath.Join(t.TempDir(), "gateway-fixture")
	build := exec.Command("go", "build", "-o", binary, "./internal/acceptance/testserver")
	build.Dir = "../../../agent-gateway"
	if raw, e := build.CombinedOutput(); e != nil {
		t.Fatalf("fixture build: %s", raw)
	}
	u, e := url.Parse(os.Getenv("TEST_DATABASE_URL"))
	if e != nil {
		t.Fatal(e)
	}
	u.Path = "/" + p.Config().ConnConfig.Database
	cmd := exec.Command(binary)
	cmd.Env = append(os.Environ(), "TEST_DATABASE_URL="+u.String())
	out, e := cmd.StdoutPipe()
	if e != nil {
		t.Fatal(e)
	}
	cmd.Stderr = os.Stderr
	if e = cmd.Start(); e != nil {
		t.Fatal(e)
	}
	defer func() { cmd.Process.Kill(); cmd.Wait() }()
	line := bufio.NewScanner(out)
	if !line.Scan() {
		t.Fatal("Gateway did not start")
	}
	gateway := requests.HTTPGateway{BaseURL: line.Text(), Username: "fixture-service", Password: "fixture-password"}
	h := backend.New(p)
	h.Requests.Gateway = gateway
	h.Requests.ConversationEnabled = true
	user, e := h.Auth.CreateUser(ctx, domain.User{Role: "admin"}, "conversation-user", "fixture-password-2026", "user")
	if e != nil {
		t.Fatal(e)
	}
	id := domain.NewID()
	_, e = p.Exec(ctx, `INSERT INTO raptor.requests(id,creator_id,idempotency_key,input_hash,definition,schema_hash,status) VALUES($1,$2,'conversation-fixture','fixture','{"type":"agent","operations":[{"name":"application.question","parameters":{"question":"health?"}}]}'::jsonb,'fixture','queued')`, id, user.ID)
	if e != nil {
		t.Fatal(e)
	}
	if e = gateway.PutRequest(ctx, id); e != nil {
		t.Fatal(e)
	}
	server := httptest.NewServer(h)
	defer server.Close()
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar}
	call := func(path string, body any, csrf string) (int, map[string]any) {
		raw, _ := json.Marshal(body)
		req, _ := http.NewRequest("POST", server.URL+"/api/v1"+path, bytes.NewReader(raw))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-CSRF-Token", csrf)
		resp, e := client.Do(req)
		if e != nil {
			t.Fatal(e)
		}
		defer resp.Body.Close()
		raw, e = io.ReadAll(resp.Body)
		if e != nil {
			t.Fatal(e)
		}
		var v map[string]any
		if e = json.Unmarshal(raw, &v); e != nil {
			t.Fatal(e)
		}
		return resp.StatusCode, v
	}
	code, login := call("/login", map[string]string{"username": "conversation-user", "password": "fixture-password-2026"}, "")
	if code != 200 {
		t.Fatal(code, login)
	}
	csrf := login["data"].(map[string]any)["csrf"].(string)
	messageID := domain.NewID()
	input := map[string]string{"messageId": messageID, "text": "/skill:literal {{unchanged}}"}
	code, _ = call("/requests/"+id+"/messages", input, "")
	if code != 403 {
		t.Fatal("CSRF accepted", code)
	}
	code, _ = call("/requests/"+id+"/messages", input, csrf)
	if code != 202 {
		t.Fatal(code)
	}
	if e = h.Requests.DispatchPending(ctx, gateway); e != nil {
		t.Fatal(e)
	}
	code, _ = call("/requests/"+id+"/messages", input, csrf)
	if code != 202 {
		t.Fatal("lost acknowledgement retry", code)
	}
	if e = h.Requests.DispatchPending(ctx, gateway); e != nil {
		t.Fatal(e)
	}
	messages, e := h.Requests.ListMessages(ctx, id, 0)
	if e != nil || len(messages) != 1 || messages[0].Status != "queued" {
		t.Fatal(messages, e)
	}
	events, e := gateway.Progress(ctx, id, 0)
	if e != nil {
		t.Fatal(e)
	}
	count := 0
	for _, v := range events {
		if v.Kind == "message" {
			count++
		}
	}
	if count != 1 {
		t.Fatal("duplicate queue replay", count)
	}
}
