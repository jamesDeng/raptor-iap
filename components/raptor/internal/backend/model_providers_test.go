package backend

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jamesDeng/raptor-iap/components/raptor/internal/db"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/domain"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/testutil"
)

func TestModelProviderAdminRoutesRequireRoleAndCSRF(t *testing.T) {
	ctx := context.Background()
	pool := testutil.Database(t)
	if err := db.Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	s := New(pool)
	s.ConfigureModelProviders(bytes.Repeat([]byte{1}, 32), "/usr/bin/node", "/private/helper.mjs")
	if _, err := s.Auth.CreateUser(ctx, domain.User{Role: "admin"}, "model-admin", "private-test-password", "admin"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Auth.CreateUser(ctx, domain.User{Role: "admin"}, "model-user", "private-test-password", "user"); err != nil {
		t.Fatal(err)
	}
	admin, err := s.Auth.Login(ctx, "model-admin", "private-test-password")
	if err != nil {
		t.Fatal(err)
	}
	user, err := s.Auth.Login(ctx, "model-user", "private-test-password")
	if err != nil {
		t.Fatal(err)
	}
	call := func(method, path, token, csrf string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, strings.NewReader(`{"expectedVersion":0,"enabled":[],"defaultModelId":""}`))
		r.AddCookie(&http.Cookie{Name: "raptor_session", Value: token})
		if csrf != "" {
			r.Header.Set("X-CSRF-Token", csrf)
		}
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		return w
	}
	if w := call("GET", "/api/v1/admin/model-providers", user.Token, ""); w.Code != 403 {
		t.Fatalf("user got admin catalog: %d", w.Code)
	}
	if w := call("PUT", "/api/v1/admin/model-providers/codex/models", admin.Token, ""); w.Code != 403 {
		t.Fatalf("mutation without CSRF: %d", w.Code)
	}
	if w := call("GET", "/api/v1/model-providers", user.Token, ""); w.Code != 200 || strings.Contains(w.Body.String(), "private-test-password") {
		t.Fatalf("public catalog: %d %s", w.Code, w.Body.String())
	}
	if w := call("GET", "/api/v1/admin/model-providers", admin.Token, ""); w.Code != 200 || !strings.Contains(w.Body.String(), "codex") || strings.Contains(w.Body.String(), "private-test-password") {
		t.Fatalf("admin catalog: %d %s", w.Code, w.Body.String())
	}
}
