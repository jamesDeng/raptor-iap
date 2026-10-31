package backend

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/adapters"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/agentaccess"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/approvals"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/domain"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/openapi"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"
)

// TestResourceCapacityPeer is a test-only peer for Infra's cross-module graph.
// The parent owns stdin and closes it to join this process and drop its database.
func TestResourceCapacityPeer(t *testing.T) {
	path, origin := os.Getenv("RAPTOR_GRAPH_READY"), os.Getenv("RAPTOR_GRAPH_INFRA")
	if path == "" || origin == "" {
		t.Skip("cross-module capacity peer")
	}
	h, r, f, token := resourceRuntimeFixture(t)
	actions := []string{domain.NewID(), domain.NewID(), domain.NewID(), domain.NewID()}
	for _, action := range actions {
		a, e := h.Approvals.Request(context.Background(), approvals.ApprovalInput{RequestID: r.ID, ActionID: action, Interface: "db-proxy.scale-in", EnvCode: "dev", Target: map[string]any{"proxyCode": r.Definition.Object.Code}, Parameters: map[string]any{"desiredCapacity": 1}})
		if e != nil {
			t.Fatal(e)
		}
		if _, e = h.Approvals.Decide(context.Background(), domain.User{ID: domain.NewID(), Role: "user"}, a.ID, approvals.DecisionInput{Decision: "approve"}); e != nil {
			t.Fatal(e)
		}
	}
	qualification := r.Definition
	qualification.ConnectionVersion = 0
	qualification.Operations = []domain.Operation{{Name: "db-proxy.replace-nodes", Parameters: map[string]any{"mode": "qualification"}}}
	q, e := h.Requests.Create(context.Background(), domain.User{ID: domain.NewID()}, "qualification", qualification)
	if e != nil {
		t.Fatal(e)
	}
	hash, _ := agentaccess.Fingerprint(q.Definition)
	qualificationToken := issued(t, h, q, hash, domain.NewID(), time.Now().Add(time.Minute))
	env, e := h.Catalog.GetEnvironment(context.Background(), "dev")
	if e != nil {
		t.Fatal(e)
	}
	env.Config["infraApiUrl"] = origin
	if e = h.Catalog.SaveEnvironment(context.Background(), env, true); e != nil {
		t.Fatal(e)
	}
	actual := New(h.Requests.Pool)
	actual.Catalog.Infra = adapters.HTTPInfra{Username: "user", Password: "secret", AuthHeader: "X-Infra-Authorization", ResolveEnvironment: actual.Catalog.GetEnvironment, Client: &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}}} // loopback test certificates only
	actual.RegisterService("fixture-controller", "fixture-controller-password")
	backend := httptest.NewServer(actual.Mux)
	defer backend.Close()
	proxy, e := openapi.NewHandler(openapi.Client{BaseURL: backend.URL, Username: "fixture-controller", Password: "fixture-controller-password"})
	if e != nil {
		t.Fatal(e)
	}
	server := httptest.NewTLSServer(proxy)
	defer server.Close()
	raw, e := json.Marshal(map[string]any{"origin": server.URL, "credential": token, "requestId": r.ID, "proof": f.proof, "actions": actions, "qualificationCredential": qualificationToken})
	if e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(path, raw, 0600); e != nil {
		t.Fatal(e)
	}
	_, _ = io.Copy(io.Discard, os.Stdin)
}
