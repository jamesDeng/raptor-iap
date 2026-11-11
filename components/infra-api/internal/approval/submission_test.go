package approval

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPgcatRequestStateUsesItsOwnResponseLimit(t *testing.T) {
	large := `{"data":{"observations":[{"data":{"evidence":"` + strings.Repeat("x", 8700) + `"}}],"actions":[]}}`
	for _, tc := range []struct {
		name, body string
		want       bool
	}{
		{"journal above 8 KiB", large, true},
		{"journal above 64 KiB", strings.Replace(large, strings.Repeat("x", 8700), strings.Repeat("x", 65536), 1), false},
		{"duplicate key in large journal", strings.Replace(large, `"actions":[]`, `"actions":[],"actions":[]`, 1), false},
		{"unknown field in large journal", strings.Replace(large, `"actions":[]`, `"actions":[],"extra":true`, 1), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPost || r.URL.Path != "/v1/infra/pgcat-request-state" {
					t.Error("wrong journal endpoint")
				}
				_, _ = w.Write([]byte(tc.body))
			}))
			defer s.Close()
			c, err := New(s.URL, "service", "private", s.Client())
			if err != nil {
				t.Fatal(err)
			}
			state, err := c.ReadPgcatRequestState(t.Context(), "request", "scope")
			if (err == nil) != tc.want || (tc.want && len(state.Observations) != 1) {
				t.Fatalf("unexpected journal result: %v", err)
			}
		})
	}
}

func TestSubmissionClientExactWireAndAuth(t *testing.T) {
	b := Binding{RequestID: "request", ActionID: "action", EnvCode: "env", ProxyCode: "proxy", GroupID: "group", DesiredCapacity: 3}
	calls := 0
	s := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u, p, ok := r.BasicAuth()
		if !ok || u != "service" || p != "private" {
			t.Error("missing service auth")
		}
		var raw map[string]json.RawMessage
		if e := json.NewDecoder(r.Body).Decode(&raw); e != nil {
			t.Error(e)
		}
		calls++
		switch r.URL.Path {
		case "/v1/approval-claim":
			if _, ok := raw["desiredCapacity"]; ok {
				t.Error("unexpected flat binding")
			}
			if string(raw["actionId"]) != `"action"` {
				t.Error("wrong action")
			}
			w.Write([]byte(`{"data":{"claimed":true}}`))
		case "/v1/approval-record":
			if string(raw["outcome"]) != `"submitted"` || string(raw["providerRequestId"]) != `"ack"` {
				t.Error("wrong receipt")
			}
			w.Write([]byte(`{"data":{"recorded":true}}`))
		default:
			t.Error("wrong path")
		}
	}))
	defer s.Close()
	c, e := New(s.URL, "service", "private", s.Client())
	if e != nil {
		t.Fatal(e)
	}
	yes, e := c.Claim(context.Background(), b)
	if e != nil || !yes {
		t.Fatal(e)
	}
	if e = c.Record(context.Background(), b, "submitted", "ack"); e != nil {
		t.Fatal(e)
	}
	if calls != 2 {
		t.Fatal(calls)
	}
}
func TestSubmissionClientFailsClosed(t *testing.T) {
	for _, body := range []string{`{"data":{}}`, `{"data":{"claimed":false}}`, `{"data":{"claimed":true},"extra":1}`, `{"data":{"claimed":true}} trailing`} {
		t.Run(body, func(t *testing.T) {
			s := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(body)) }))
			defer s.Close()
			c, _ := New(s.URL, "u", "p", s.Client())
			yes, e := c.Claim(context.Background(), Binding{RequestID: "r", ActionID: "a", EnvCode: "e", ProxyCode: "p", GroupID: "g", DesiredCapacity: 1})
			if yes {
				t.Fatal("unsafe response allowed")
			}
			if body != `{"data":{"claimed":false}}` && e == nil {
				t.Fatal("malformed response accepted")
			}
		})
	}
}
