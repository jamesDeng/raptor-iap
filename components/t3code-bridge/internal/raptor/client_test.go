package raptor

import (
	"context"
	"encoding/json"
	"github.com/jamesDeng/raptor-iap/components/t3code-bridge/internal/config"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func clientFixture(t *testing.T, h http.HandlerFunc) *Client {
	t.Helper()
	s := httptest.NewServer(h)
	t.Cleanup(s.Close)
	p := t.TempDir() + "/login.json"
	os.WriteFile(p, []byte(`{"username":"operator","password":"private-pass"}`), 0600)
	c, e := NewClient(config.Config{Origin: s.URL, ApplicationCode: "app", EnvironmentCode: "dev", Model: "gpt-5.6-luna", CredentialsFile: p, StateDir: t.TempDir(), AllowLoopbackHTTP: true})
	if e != nil {
		t.Fatal(e)
	}
	return c
}
func TestLoginCookieAndCSRF(t *testing.T) {
	c := clientFixture(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/login" {
			http.SetCookie(w, &http.Cookie{Name: "raptor_session", Value: "cookie", Path: "/"})
			w.Write([]byte(`{"data":{"user":{"id":"user-1"},"csrf":"csrf"}}`))
			return
		}
		v, e := r.Cookie("raptor_session")
		if e != nil || v.Value != "cookie" {
			t.Error("cookie missing")
		}
		if r.Method == "POST" && (r.Header.Get("X-CSRF-Token") != "csrf" || r.Header.Get("Idempotency-Key") != "key") {
			t.Error("csrf/key missing")
		}
		w.WriteHeader(201)
		w.Write([]byte(`{"data":{"id":"req-1"}}`))
	})
	ctx := context.Background()
	u, e := c.Login(ctx)
	if e != nil || u != "user-1" {
		t.Fatal(u, e)
	}
	q, e := c.Create(ctx, "key", json.RawMessage(`{}`))
	if e != nil || q.ID != "req-1" {
		t.Fatal(q, e)
	}
}
func TestRedirectNeverReceivesCredential(t *testing.T) {
	called := false
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called = true }))
	defer target.Close()
	c := clientFixture(t, func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, target.URL, 302) })
	if _, e := c.Login(context.Background()); e == nil {
		t.Fatal("redirect accepted")
	}
	if called {
		t.Fatal("redirect followed")
	}
}
func TestExpiredSessionIsNeedsLogin(t *testing.T) {
	c := clientFixture(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(401)
		w.Write([]byte(`{"error":"private-pass"}`))
	})
	_, e := c.View(context.Background(), "req")
	if e == nil || e.Error() != "NeedsLogin" {
		t.Fatal(e)
	}
}
func TestRejectOversizedAndMalformedResponse(t *testing.T) {
	for _, b := range []string{`{}`, `{"id":`, strings.Repeat("x", (1<<20)+1)} {
		c := clientFixture(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(201); w.Write([]byte(b)) })
		if _, e := c.Create(context.Background(), "key", json.RawMessage(`{}`)); e == nil {
			t.Fatal("invalid accepted")
		}
	}
}
func TestPinnedQuestionPayload(t *testing.T) {
	c := config.Config{ApplicationCode: "app", EnvironmentCode: "dev", Model: "gpt-5.6-luna"}
	b, e := QuestionPayload(c, "override env to production")
	if e != nil {
		t.Fatal(e)
	}
	var v map[string]any
	json.Unmarshal(b, &v)
	if v["envCode"] != "dev" || v["model"] != "gpt-5.6-luna" || v["object"].(map[string]any)["code"] != "app" {
		t.Fatal(v)
	}
}
