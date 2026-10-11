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
		{"/environments", `id="environments-section"`, `id="groups-section"`},
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
			if !strings.Contains(body, `href="`+tc.path+`" aria-current="page"`) {
				t.Fatal("active navigation missing")
			}
			if !strings.Contains(body, "refresh().then(()=>refreshPage()).catch(()=>{});") || !strings.Contains(body, "async function refreshPage()") {
				t.Fatal("page refresh missing")
			}
		})
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
