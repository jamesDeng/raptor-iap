package execution

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestAttemptAccessRejectsBroadenedBindingAndRedirect(t *testing.T) {
	b := AttemptBinding{RequestID: NewID(), AttemptID: NewID(), Operation: "application.question", ObjectKind: "application", ObjectCode: "gateway", EnvCode: "rdev.ali", SkillsCommit: strings.Repeat("a", 40), Model: "gpt-5.6-luna"}
	expires := time.Now().Add(time.Minute)
	mode := "valid"
	calls := 0
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		u, p, ok := r.BasicAuth()
		if !ok || u != "service" || p != "fixture" {
			t.Error("service auth missing")
		}
		if mode == "redirect" {
			w.Header().Set("Location", "https://untrusted.invalid")
			w.WriteHeader(307)
			return
		}
		binding := b
		if mode == "broadened" {
			binding.EnvCode = "other"
		}
		json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"credential": strings.Repeat("q", 32), "expiresAt": expires, "binding": binding, "raptorMcpUrl": "https://raptor.invalid/mcp", "infraMcpUrl": "https://infra.invalid/mcp"}})
	}))
	defer s.Close()
	c := HTTPRaptor{BaseURL: s.URL, Username: "service", Password: "fixture"}
	if _, e := c.Issue(context.Background(), b, strings.Repeat("a", 64), expires); e != nil {
		t.Fatal(e)
	}
	for _, v := range []string{"broadened", "redirect"} {
		mode = v
		before := calls
		if _, e := c.Issue(context.Background(), b, strings.Repeat("a", 64), expires); e == nil {
			t.Fatal("unsafe access accepted")
		}
		if calls != before+1 {
			t.Fatal("mutation retried")
		}
	}
}
