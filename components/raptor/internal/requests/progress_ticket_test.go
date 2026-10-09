package requests

import (
	"context"
	"encoding/json"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/auth"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/domain"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGatewayProgressTicketKeepsServiceCredentialsServerSide(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u, p, ok := r.BasicAuth()
		if !ok || u != "service" || p != "private" {
			t.Error("missing service authentication")
		}
		if r.Method != "POST" || r.URL.Path != "/v1/requests/request/progress-ticket" {
			t.Error("incorrect ticket route")
		}
		var in map[string]string
		json.NewDecoder(r.Body).Decode(&in)
		if in["subject"] != "user" || in["origin"] != "http://localhost:3790" {
			t.Error("incorrect binding")
		}
		json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"token": "opaque", "url": "ws://localhost:8874/v1/progress"}})
	}))
	defer server.Close()
	g := HTTPGateway{BaseURL: server.URL, Username: "service", Password: "private"}
	v, e := g.ProgressTicket(context.Background(), "request", "user", "http://localhost:3790")
	if e != nil {
		t.Fatal(e)
	}
	if v["token"] != "opaque" {
		t.Fatal(v)
	}
	if _, ok := v["password"]; ok {
		t.Fatal("credentials returned")
	}
}

type ticketGateway struct{ calls int }

func (g *ticketGateway) Execution(context.Context, string) (map[string]any, error) { return nil, nil }
func (g *ticketGateway) Progress(context.Context, string, int64) ([]GatewayEvent, error) {
	return nil, nil
}
func (g *ticketGateway) ProgressTicket(_ context.Context, id, subject, origin string) (map[string]any, error) {
	g.calls++
	return map[string]any{"token": "opaque", "url": "ws://localhost:8874/v1/progress"}, nil
}
func TestBrowserTicketRequiresSessionCSRFOriginAndExistingAgentRequest(t *testing.T) {
	s, in, _ := setup(t)
	ctx := context.Background()
	a := auth.NewService(s.Pool)
	user, e := a.CreateUser(ctx, domain.User{Role: "admin"}, "alice", "test-password-long", "user")
	if e != nil {
		t.Fatal(e)
	}
	session, e := a.Login(ctx, "alice", "test-password-long")
	if e != nil {
		t.Fatal(e)
	}
	request, e := s.Create(ctx, user, "ticket-test", in)
	if e != nil {
		t.Fatal(e)
	}
	g := &ticketGateway{}
	s.Gateway = g
	m := http.NewServeMux()
	s.Register(m, a)
	for _, tc := range []struct {
		cookie, csrf, origin string
		want                 int
	}{{"", "", "http://localhost", 401}, {session.Token, "", "http://localhost", 403}, {session.Token, session.CSRF, "http://evil.example", 403}, {session.Token, session.CSRF, "http://localhost", 200}} {
		r := httptest.NewRequest("POST", "http://localhost/api/v1/requests/"+request.ID+"/progress-ticket", nil)
		if tc.cookie != "" {
			r.AddCookie(&http.Cookie{Name: "raptor_session", Value: tc.cookie})
		}
		r.Header.Set("X-CSRF-Token", tc.csrf)
		r.Header.Set("Origin", tc.origin)
		w := httptest.NewRecorder()
		m.ServeHTTP(w, r)
		if w.Code != tc.want {
			t.Fatalf("got %d want %d: %s", w.Code, tc.want, w.Body.String())
		}
	}
	if g.calls != 1 {
		t.Fatal("unauthorized ticket issued")
	}
	if _, e = s.Pool.Exec(ctx, "UPDATE raptor.requests SET definition=jsonb_set(definition,'{type}','\"direct\"') WHERE id=$1", request.ID); e != nil {
		t.Fatal(e)
	}
	r := httptest.NewRequest("POST", "http://localhost/api/v1/requests/"+request.ID+"/progress-ticket", nil)
	r.AddCookie(&http.Cookie{Name: "raptor_session", Value: session.Token})
	r.Header.Set("X-CSRF-Token", session.CSRF)
	r.Header.Set("Origin", "http://localhost")
	w := httptest.NewRecorder()
	m.ServeHTTP(w, r)
	if w.Code != 501 || g.calls != 1 {
		t.Fatal("direct request attempted Gateway subscription")
	}
}
