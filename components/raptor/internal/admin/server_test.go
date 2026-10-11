package admin

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAdminPages(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/users" {
			t.Error("wrong delegation")
		}
		w.WriteHeader(403)
	}))
	defer upstream.Close()
	h, err := NewHandler(upstream.URL)
	if err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "http://127.0.0.1:8873/api/v1/users", nil))
	if w.Code != 403 {
		t.Fatal("backend admin denial not preserved")
	}
	for _, tc := range []struct{ path, want, absent string }{
		{"/users", `id="users-section"`, `id="groups-section"`},
		{"/environment-groups", `id="groups-section"`, `id="environments-section"`},
		{"/environments", `id="environment-search"`, `id="env-form"`},
		{"/environments?mode=create", `id="env-form"`, `id="environment-search"`},
		{"/environments/rdev.ali", `id="environment-detail"`, `id="env-form"`},
		{"/environments/new", `id="environment-detail"`, `id="env-form"`},
		{"/environments/rdev.ali/edit", `id="env-form"`, `id="environment-search"`},
		{"/ai-models", `id="model-admin"`, `id="users-section"`},
	} {
		t.Run(tc.path, func(t *testing.T) {
			w := httptest.NewRecorder()
			h.ServeHTTP(w, httptest.NewRequest("GET", "http://127.0.0.1:8873"+tc.path, nil))
			b, _ := io.ReadAll(w.Result().Body)
			body := string(b)
			if w.Code != 200 || !strings.Contains(body, tc.want) || strings.Contains(body, tc.absent) {
				t.Fatalf("wrong page %s: status %d", tc.path, w.Code)
			}
			active := strings.Split(tc.path, "?")[0]
			if strings.HasPrefix(tc.path, "/environments/") {
				active = "/environments"
			}
			if !strings.Contains(body, `href="`+active+`" aria-current="page"`) {
				t.Fatal("active navigation missing")
			}
			if !strings.Contains(body, "refresh().then(()=>refreshPage()).catch(()=>{});") || !strings.Contains(body, "async function refreshPage()") {
				t.Fatal("page refresh missing")
			}
		})
	}
	for _, path := range []string{"/environments/rdev.ali/other", "/environments//edit"} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", "http://127.0.0.1:8873"+path, nil))
		if w.Code != 404 {
			t.Errorf("%s: expected 404, got %d", path, w.Code)
		}
	}
	for _, tc := range []struct {
		path   string
		wants  []string
		absent string
	}{
		{"/environments", []string{`href="/environments?mode=create"`, `id="environment-search"`, `id="environment-list"`}, `name="edit"`},
		{"/environments/rdev.ali", []string{`id="environment-detail"`, `id="edit-environment"`, `id="detail-repositories"`}, `id="env-form"`},
		{"/environments/rdev.ali/edit", []string{`id="env-form"`, `id="cancel-environment"`, `id="repository-rows"`}, `name="edit"`},
	} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", "http://127.0.0.1:8873"+tc.path, nil))
		body := w.Body.String()
		for _, want := range tc.wants {
			if !strings.Contains(body, want) {
				t.Errorf("%s: missing %s", tc.path, want)
			}
		}
		if strings.Contains(body, tc.absent) {
			t.Errorf("%s: contains %s", tc.path, tc.absent)
		}
	}
	w = httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "http://127.0.0.1:8873/", nil))
	if !strings.Contains(w.Body.String(), "location.hash") {
		t.Fatal("legacy section links not mapped")
	}
	w = httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "http://127.0.0.1:8873/unknown", nil))
	if w.Code != 404 {
		t.Fatal("unknown page should be 404")
	}
}
