package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestBasicAuth(t *testing.T) {
	h := New(nil, "service", "private-test-password")
	r := httptest.NewRequest("GET", "/v1/requests/id", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusUnauthorized {
		t.Fatal("unauthenticated access")
	}
	r = httptest.NewRequest("GET", "/unknown", nil)
	r.SetBasicAuth("service", "private-test-password")
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 404 {
		t.Fatal("valid service authentication rejected")
	}
}
