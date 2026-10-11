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

func TestResourceDecisionReadsFreshControlWithoutApprovalGrant(t *testing.T) {
	state := ""
	status := "running"
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/requests/request/control" {
			t.Error("resource wait fetched approval")
			http.Error(w, "unexpected", 500)
			return
		}
		calls++
		json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"requestId": "request", "status": status, "controlState": state}})
	}))
	defer server.Close()
	client := HTTPRaptor{BaseURL: server.URL, Username: "test", Password: "test"}
	wait := LiveWait{Kind: "resource", ActionID: "capacity-ready", BindingDigest: strings.Repeat("a", 64), StartedAt: time.Now(), WakeAfterSeconds: 60}
	check := func(want string) {
		t.Helper()
		got, err := client.ResolveLiveDecision(context.Background(), "request", wait)
		if err != nil || got.Next != want {
			t.Fatalf("got %+v %v want %s", got, err, want)
		}
	}
	check("waiting")
	wait.StartedAt = time.Now().Add(-61 * time.Second)
	check("resume")
	state = "blocked"
	check("block")
	state = "cancel_requested"
	check("cancel")
	if calls != 4 {
		t.Fatalf("control reads %d", calls)
	}
}
