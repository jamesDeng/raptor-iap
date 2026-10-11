package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestObservationRouteUsesAttemptAuthWithoutServiceAccess(t *testing.T) {
	called := 0
	h := New(nil, "service", "private", ProgressConfig{Observation: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called++; w.WriteHeader(401) })})
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("POST", "/v1/gateway-observations", nil))
	if called != 1 || w.Code != 401 {
		t.Fatal("not routed", called, w.Code)
	}
	w = httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("POST", "/v1/requests/request/signals", nil))
	if called != 1 || w.Code != 401 {
		t.Fatal("service route exposed")
	}
}
