package auth

import (
	"context"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/db"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/domain"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/testutil"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestSessionAndAdminPermissions(t *testing.T) {
	ctx := context.Background()
	p := testutil.Database(t)
	if e := db.Migrate(ctx, p); e != nil {
		t.Fatal(e)
	}
	s := NewService(p)
	admin := domain.User{Role: "admin"}
	user, e := s.CreateUser(ctx, admin, "alice", "test-password-long", "user")
	if e != nil {
		t.Fatal(e)
	}
	var hash string
	if e = p.QueryRow(ctx, "SELECT password_hash FROM raptor.users WHERE id=$1", user.ID).Scan(&hash); e != nil {
		t.Fatal(e)
	}
	if hash == "test-password-long" {
		t.Fatal("plaintext password persisted")
	}
	if _, e = s.Login(ctx, "alice", "wrong"); e == nil {
		t.Fatal("bad password accepted")
	}
	session, e := s.Login(ctx, "alice", "test-password-long")
	if e != nil {
		t.Fatal(e)
	}
	got, e := s.Authenticate(ctx, session.Token)
	if e != nil || got.ID != user.ID {
		t.Fatal("session authentication failed")
	}
	if e = RequireAdmin(got); e == nil {
		t.Fatal("regular user administered accounts")
	}
	if _, e = s.CreateUser(ctx, got, "bob", "test-password-long", "admin"); e == nil {
		t.Fatal("regular user created admin")
	}
	if _, e = p.Exec(ctx, "UPDATE raptor.sessions SET expires_at=$1", time.Now().Add(-time.Second)); e != nil {
		t.Fatal(e)
	}
	if _, e = s.Authenticate(ctx, session.Token); e == nil {
		t.Fatal("expired session accepted")
	}
	session, e = s.Login(ctx, "alice", "test-password-long")
	if e != nil {
		t.Fatal(e)
	}
	if e = s.Logout(ctx, session.Token); e != nil {
		t.Fatal(e)
	}
	if _, e = s.Authenticate(ctx, session.Token); e == nil {
		t.Fatal("logged-out session accepted")
	}
}

func TestBrowserCSRFAndBasicAuth(t *testing.T) {
	ctx := context.Background()
	p := testutil.Database(t)
	if e := db.Migrate(ctx, p); e != nil {
		t.Fatal(e)
	}
	s := NewService(p)
	_, e := s.CreateUser(ctx, domain.User{Role: "admin"}, "alice", "test-password-long", "user")
	if e != nil {
		t.Fatal(e)
	}
	session, e := s.Login(ctx, "alice", "test-password-long")
	if e != nil {
		t.Fatal(e)
	}
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) })
	for _, tc := range []struct {
		csrf string
		want int
	}{{"", 403}, {session.CSRF, 204}} {
		r := httptest.NewRequest("POST", "http://localhost/action", nil)
		r.AddCookie(&http.Cookie{Name: "raptor_session", Value: session.Token})
		r.Header.Set("X-CSRF-Token", tc.csrf)
		w := httptest.NewRecorder()
		s.Browser(next).ServeHTTP(w, r)
		if w.Code != tc.want {
			t.Fatalf("CSRF status=%d want=%d", w.Code, tc.want)
		}
	}
	h := BasicAuth(next, Credentials{Username: "gateway", Password: "private-test"})
	r := httptest.NewRequest("GET", "http://localhost/internal", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 401 {
		t.Fatal("missing service auth allowed")
	}
	r.SetBasicAuth("gateway", "private-test")
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 204 {
		t.Fatal("valid service auth rejected")
	}
}
