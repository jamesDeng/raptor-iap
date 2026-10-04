package frontend

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestFrontendServesPOC(t *testing.T) {
	h, e := NewHandler("http://127.0.0.1:8871")
	if e != nil {
		t.Fatal(e)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/", nil))
	if w.Code != 200 || !strings.Contains(w.Body.String(), "RAPTOR") {
		t.Fatal("frontend missing")
	}
}
