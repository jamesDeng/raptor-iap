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
func TestDirectGatewayCSPIsExplicitAndValidated(t *testing.T) {
	t.Setenv("RAPTOR_PROGRESS_CONNECT_ORIGIN", "wss://gateway.example")
	h, e := NewHandler("http://127.0.0.1:8871")
	if e != nil {
		t.Fatal(e)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/", nil))
	if !strings.Contains(w.Header().Get("Content-Security-Policy"), "connect-src 'self' wss://gateway.example;") {
		t.Fatal("direct Gateway blocked")
	}
	for _, origin := range []string{"wss://gateway.example; connect-src *", "ws://gateway.example", "wss://user:password@gateway.example", "wss://gateway.example/path"} {
		t.Setenv("RAPTOR_PROGRESS_CONNECT_ORIGIN", origin)
		if _, e = NewHandler("http://127.0.0.1:8871"); e == nil {
			t.Fatalf("invalid origin accepted %s", origin)
		}
	}
}
