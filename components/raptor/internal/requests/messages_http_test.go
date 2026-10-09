package requests

import (
	"context"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/auth"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/domain"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestBrowserMessageAdmissionAuthorizationAndBounds(t *testing.T) {
	s, r, _, _ := messageSetup(t)
	ctx := context.Background()
	a := auth.NewService(s.Pool)
	user, e := a.CreateUser(ctx, domain.User{Role: "admin"}, "msguser", "test-password-long", "user")
	if e != nil {
		t.Fatal(e)
	}
	session, e := a.Login(ctx, "msguser", "test-password-long")
	if e != nil {
		t.Fatal(e)
	}
	s.Pool.Exec(ctx, "UPDATE raptor.requests SET creator_id=$2 WHERE id=$1", r.ID, user.ID)
	m := http.NewServeMux()
	s.Register(m, a)
	for _, tc := range []struct {
		cookie, csrf, body string
		want               int
	}{{"", "", `{}`, 401}, {session.Token, "", `{}`, 403}, {session.Token, session.CSRF, `{"messageId":"` + domain.NewID() + `","text":"hi","role":"admin"}`, 400}, {session.Token, session.CSRF, `{"messageId":"` + domain.NewID() + `","text":"hi"}`, 202}} {
		req := httptest.NewRequest("POST", "http://localhost/api/v1/requests/"+r.ID+"/messages", strings.NewReader(tc.body))
		if tc.cookie != "" {
			req.AddCookie(&http.Cookie{Name: "raptor_session", Value: tc.cookie})
		}
		req.Header.Set("X-CSRF-Token", tc.csrf)
		w := httptest.NewRecorder()
		m.ServeHTTP(w, req)
		if w.Code != tc.want {
			t.Fatal(w.Code, tc.want, w.Body.String())
		}
	}
	s.Pool.Exec(ctx, "UPDATE raptor.requests SET creator_id=$2 WHERE id=$1", r.ID, domain.NewID())
	req := httptest.NewRequest("POST", "http://localhost/api/v1/requests/"+r.ID+"/messages", strings.NewReader(`{"messageId":"`+domain.NewID()+`","text":"hi"}`))
	req.AddCookie(&http.Cookie{Name: "raptor_session", Value: session.Token})
	req.Header.Set("X-CSRF-Token", session.CSRF)
	w := httptest.NewRecorder()
	m.ServeHTTP(w, req)
	if w.Code != 403 {
		t.Fatal(w.Code)
	}
}
