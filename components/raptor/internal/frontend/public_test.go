package frontend

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestPublicFrontendDeniesManagementBeforeProxy(t *testing.T) {
	t.Setenv("RAPTOR_PUBLIC_FRONTEND", "true")
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) }))
	defer upstream.Close()
	h, err := NewHandler(upstream.URL)
	if err != nil {
		t.Fatal(err)
	}
	for _, identity := range []string{"", "user-session", "admin-session"} {
		for _, c := range []struct{ method, path string }{{"GET", "/api/v1/users"}, {"POST", "/api/v1/users"}, {"POST", "/api/v1/environment-groups"}, {"POST", "/api/v1/environments"}, {"PATCH", "/api/v1/environments/rdev.ali"}, {"GET", "/api/v1/x/../users"}} {
			r := httptest.NewRequest(c.method, c.path, nil)
			if identity != "" {
				r.AddCookie(&http.Cookie{Name: "raptor_session", Value: identity})
			}
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != 403 {
				t.Errorf("%s %s identity=%q reached public proxy: %d", c.method, c.path, identity, w.Code)
			}
		}
	}
	for _, c := range []struct{ method, path string }{{"GET", "/api/v1/environments"}, {"GET", "/api/v1/environment-groups"}, {"POST", "/api/v1/requests"}, {"POST", "/api/v1/login"}} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest(c.method, c.path, nil))
		if w.Code != 204 {
			t.Errorf("ordinary %s %s blocked", c.method, c.path)
		}
	}
}
