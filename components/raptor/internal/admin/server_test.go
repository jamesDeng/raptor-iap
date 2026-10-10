package admin

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAdminDelegatesAndDoesNotLeakUpstreamErrors(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/users" {
			t.Error("wrong delegation")
		}
		w.WriteHeader(403)
	}))
	defer upstream.Close()
	h, e := NewHandler(upstream.URL)
	if e != nil {
		t.Fatal(e)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "http://127.0.0.1:8873/api/v1/users", nil))
	if w.Code != 403 {
		t.Fatal("backend admin denial not preserved")
	}
	w = httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "http://127.0.0.1:8873/", nil))
	b, _ := io.ReadAll(w.Result().Body)
	if !strings.Contains(string(b), "Raptor Admin") {
		t.Fatal("admin page missing")
	}
	if !strings.Contains(string(b), "refresh().then(()=>refreshModelProviders()).catch(()=>{});") {
		t.Fatal("model provider refresh must wait until its script is loaded")
	}
}
