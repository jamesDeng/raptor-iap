package runtime

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jamesDeng/raptor-iap/components/agent-gateway/internal/execution"
)

func TestModelCredentialClientBindsPrivateRouteAndRejectsRedirect(t *testing.T) {
	secret := `{"type":"oauth","access":"private-access","refresh":"private-refresh","expires":2000000000000}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u, p, ok := r.BasicAuth()
		if !ok || u != "service" || p != "password" {
			t.Error("service auth missing")
			http.Error(w, "denied", 401)
			return
		}
		if r.URL.Path != "/internal/v1/model-credentials/lease" {
			t.Errorf("unexpected path %s", r.URL.Path)
			return
		}
		var b struct {
			ProviderID        string `json:"providerId"`
			ModelID           string `json:"modelId"`
			ConnectionVersion int64  `json:"connectionVersion"`
		}
		if json.NewDecoder(r.Body).Decode(&b) != nil || b.ProviderID != "codex" || b.ModelID != "gpt-6-sol" || b.ConnectionVersion != 2 {
			t.Error("binding lost")
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"data":{"credential":` + secret + `,"generation":3,"expiresAt":"2033-05-18T03:33:20Z"}}`))
	}))
	defer server.Close()
	client := HTTPModelCredentials{BaseURL: server.URL, Username: "service", Password: "password"}
	b := execution.AttemptBinding{RequestID: "r", AttemptID: "a", ProviderID: "codex", Model: "gpt-6-sol", ConnectionVersion: 2}
	lease, err := client.Lease(context.Background(), b)
	if err != nil || lease.Generation != 3 || !strings.Contains(string(lease.Credential), "private-refresh") {
		t.Fatalf("lease failed: %v", err)
	}
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, server.URL, 302) }))
	defer redirect.Close()
	client.BaseURL = redirect.URL
	if _, err = client.Lease(context.Background(), b); err == nil || strings.Contains(err.Error(), "private") {
		t.Fatal("redirect or secret-bearing error accepted")
	}
}
