package auth

import (
	"context"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/db"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/domain"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/testutil"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHTTPSDeploymentSetsSecureLoginCookieBehindProxy(t *testing.T) {
	t.Setenv("RAPTOR_SECURE_COOKIES", "true")
	p := testutil.Database(t)
	if err := db.Migrate(context.Background(), p); err != nil {
		t.Fatal(err)
	}
	s := NewService(p)
	if _, err := s.CreateUser(context.Background(), domain.User{Role: "admin"}, "proxy-cookie-user", "test-password-long", "user"); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	s.Register(mux)
	r := httptest.NewRequest("POST", "http://backend/api/v1/login", strings.NewReader(`{"username":"proxy-cookie-user","password":"test-password-long"}`))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatalf("login status %d", w.Code)
	}
	cookies := w.Result().Cookies()
	if len(cookies) != 1 || !cookies[0].Secure || !cookies[0].HttpOnly {
		t.Fatal("HTTPS deployment emitted an insecure session cookie")
	}
}
