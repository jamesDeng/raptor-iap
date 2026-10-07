package approval

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// Only an explicit true on a well-formed authenticated Raptor response allows
// a scale-in check. Provider target/parameters are constructed by Infra API.
func TestApprovalCheckExactBindingAndFailClosed(t *testing.T) {
	for _, body := range []string{`{"data":{"allowed":true,"reason":"approved"}}`, `{"data":{"allowed":false,"reason":"not approved"}}`, `{"data":{}}`, `{"data":{"allowed":true,"allowed":false}}`, `{"data":{"allowed":true}} {}`} {
		transport := roundTrip(func(r *http.Request) (*http.Response, error) {
			user, pass, ok := r.BasicAuth()
			if !ok || user != "service" || pass != "private" || r.URL.Path != "/v1/approval-check" || r.URL.Host != "raptor.internal" {
				t.Fatal("incorrect private service call")
			}
			var binding map[string]any
			json.NewDecoder(r.Body).Decode(&binding)
			if binding["interface"] != "ess.scale-in" || binding["actionId"] != "action" || binding["target"].(map[string]any)["groupId"] != "g" || binding["parameters"].(map[string]any)["desiredCapacity"] != float64(2) {
				t.Fatal(binding)
			}
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: http.Header{}}, nil
		})
		c, e := New("https://raptor.internal", "service", "private", &http.Client{Transport: transport})
		if e != nil {
			t.Fatal(e)
		}
		allowed, e := c.Check(context.Background(), Binding{RequestID: "r", ActionID: "action", EnvCode: "dev", ProxyCode: "p", GroupID: "g", DesiredCapacity: 2})
		if body == `{"data":{"allowed":true,"reason":"approved"}}` {
			if !allowed || e != nil {
				t.Fatal(allowed, e)
			}
		} else if allowed {
			t.Fatal("invalid approval allowed")
		}
	}
}
