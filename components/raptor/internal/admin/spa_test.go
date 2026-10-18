package admin

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAdminSPARoutes(t *testing.T) {
	h, err := NewHandler("http://127.0.0.1:8871")
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/", "/users", "/environment-groups", "/environments", "/environments?mode=create", "/environments/rdev.ali", "/environments/new", "/environments/rdev.ali/edit", "/ai-models", "/github-pat"} {
		t.Run(path, func(t *testing.T) {
			w := httptest.NewRecorder()
			h.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
			if w.Code != 200 || !strings.Contains(w.Body.String(), `id="app"`) || !strings.Contains(w.Body.String(), `type="module"`) {
				t.Fatalf("not SPA: %d", w.Code)
			}
			if w.Header().Get("Cache-Control") != "no-store" {
				t.Fatal("session shell must not be cached")
			}
		})
	}
	for _, path := range []string{"/unknown", "/environments//edit", "/environments/rdev.ali/other", "/assets/missing.js", "/src/main.ts", "/package.json"} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		if w.Code != 404 {
			t.Errorf("%s got %d", path, w.Code)
		}
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("POST", "/users", nil))
	if w.Code != 405 {
		t.Fatal("static routes must reject writes")
	}
}
func TestAdminPreservesProxySessionAndDenial(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/users" || r.Method != "POST" || r.Header.Get("X-CSRF-Token") != "fixture-csrf" || r.Header.Get("Cookie") != "raptor_session=fixture" {
			t.Error("request changed")
		}
		w.Header().Set("Set-Cookie", "raptor_session=; Max-Age=0; Path=/; HttpOnly")
		w.WriteHeader(403)
	}))
	defer upstream.Close()
	h, err := NewHandler(upstream.URL)
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("POST", "/api/v1/users", strings.NewReader(`{}`))
	r.Header.Set("X-CSRF-Token", "fixture-csrf")
	r.Header.Set("Cookie", "raptor_session=fixture")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 403 || !strings.Contains(w.Header().Get("Set-Cookie"), "Max-Age=0") {
		t.Fatal("backend response changed")
	}
}
func TestAdminRejectsInvalidBackend(t *testing.T) {
	for _, url := range []string{"", "ftp://localhost", "http://user:password@localhost"} {
		if _, e := NewHandler(url); e == nil {
			t.Errorf("accepted %s", url)
		}
	}
}
