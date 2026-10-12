package githubcredentials

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jamesDeng/raptor-iap/components/raptor/internal/auth"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/db"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/domain"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/testutil"
)

func TestHTTPAdminCSRFAndPrivateGatewayRead(t *testing.T) {
	ctx := context.Background()
	pool := testutil.Database(t)
	if err := db.Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	store := &memorySecrets{}
	svc := &Service{Pool: pool, Secrets: store, Verify: func(context.Context, string) (string, error) { return "pat-owner", nil }}
	a := auth.NewService(pool)
	if _, err := a.CreateUser(ctx, domain.User{Role: "admin"}, "pat-admin", "test-password-long", "admin"); err != nil {
		t.Fatal(err)
	}
	if _, err := a.CreateUser(ctx, domain.User{Role: "admin"}, "pat-user", "test-password-long", "user"); err != nil {
		t.Fatal(err)
	}
	adminSession, err := a.Login(ctx, "pat-admin", "test-password-long")
	if err != nil {
		t.Fatal(err)
	}
	userSession, err := a.Login(ctx, "pat-user", "test-password-long")
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	(&HTTPService{Service: svc}).Register(mux, a, auth.Credentials{Username: "gateway-pat", Password: "gateway-secret"})
	call := func(method, path, body, cookie, csrf, username, password string) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		if cookie != "" {
			r.AddCookie(&http.Cookie{Name: "raptor_session", Value: cookie})
		}
		if csrf != "" {
			r.Header.Set("X-CSRF-Token", csrf)
		}
		if username != "" {
			r.SetBasicAuth(username, password)
		}
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		return w
	}
	const token = "github_pat_private_fixture"
	body := `{"token":"` + token + `"}`
	if got := call("PUT", "/api/v1/admin/github-pat", body, userSession.Token, userSession.CSRF, "", ""); got.Code != 403 {
		t.Fatalf("ordinary user: %d", got.Code)
	}
	if got := call("PUT", "/api/v1/admin/github-pat", body, adminSession.Token, "", "", ""); got.Code != 403 {
		t.Fatalf("missing CSRF: %d", got.Code)
	}
	if got := call("PUT", "/api/v1/admin/github-pat", body, adminSession.Token, adminSession.CSRF, "", ""); got.Code != 200 || bytes.Contains(got.Body.Bytes(), []byte(token)) {
		t.Fatalf("save status/leak: %d %s", got.Code, got.Body.String())
	}
	if got := call("GET", "/api/v1/admin/github-pat", "", adminSession.Token, "", "", ""); got.Code != 200 || bytes.Contains(got.Body.Bytes(), []byte(token)) {
		t.Fatalf("status leak: %d %s", got.Code, got.Body.String())
	}
	if got := call("GET", "/v1/private/github-pat", "", "", "", "", ""); got.Code != 401 {
		t.Fatalf("anonymous private read: %d", got.Code)
	}
	if got := call("GET", "/v1/private/github-pat", "", "", "", "gateway-pat", "wrong"); got.Code != 401 {
		t.Fatalf("wrong service identity: %d", got.Code)
	}
	got := call("GET", "/v1/private/github-pat", "", "", "", "gateway-pat", "gateway-secret")
	var decoded struct {
		Data struct {
			Token string `json:"token"`
		} `json:"data"`
	}
	if got.Code != 200 || json.Unmarshal(got.Body.Bytes(), &decoded) != nil || decoded.Data.Token != token {
		t.Fatalf("private read failed: %d", got.Code)
	}
	if got := call("DELETE", "/api/v1/admin/github-pat", "", adminSession.Token, adminSession.CSRF, "", ""); got.Code != 200 {
		t.Fatalf("delete: %d", got.Code)
	}
	if got := call("GET", "/api/v1/admin/github-pat", "", adminSession.Token, "", "", ""); !bytes.Contains(got.Body.Bytes(), []byte(`"configured":false`)) {
		t.Fatalf("delete status: %s", got.Body.String())
	}
}
