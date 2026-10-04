package backend

import (
	"context"
	"encoding/json"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/adapters"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/catalog"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/db"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/domain"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/requests"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/skills"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/testutil"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"testing"
	"time"
)

type browserGateway struct{}

func (browserGateway) Execution(context.Context, string) (map[string]any, error) {
	return map[string]any{"status": "completed", "appliedSkills": map[string]string{"tag": "skills-v0.1.0"}}, nil
}
func (browserGateway) Progress(ctx context.Context, id string, after int64) ([]requests.GatewayEvent, error) {
	if after > 0 {
		return []requests.GatewayEvent{}, nil
	}
	return []requests.GatewayEvent{{EventID: "fixture-" + id, Sequence: 1, Kind: "tool_result", Summary: "Simulated fixture finished; no infrastructure was changed", Details: json.RawMessage(`{"tool":"read","status":"simulated"}`), EvidenceMode: "simulated"}}, nil
}
func TestBrowserFixtureServer(t *testing.T) {
	if os.Getenv("POC_BROWSER_FIXTURE") != "true" {
		t.Skip("explicit local UI fixture only")
	}
	ctx := context.Background()
	p := testutil.Database(t)
	if e := db.Migrate(ctx, p); e != nil {
		t.Fatal(e)
	}
	h := New(p)
	h.Skills.Source = skills.FixtureSource{Versions: []domain.SkillsVersion{{Tag: "skills-v0.1.0", CommitSHA: strings.Repeat("a", 40)}}}
	h.Requests.Gateway = browserGateway{}
	if _, e := h.Auth.CreateUser(ctx, domain.User{Role: "admin"}, "poc-reviewer", "local-fixture-only-2026", "admin"); e != nil {
		t.Fatal(e)
	}
	h.Catalog.CreateGroup(ctx, "raptor", "Raptor POC")
	h.Catalog.SaveEnvironment(ctx, domain.Environment{Code: "rdev.ali", GroupCode: "raptor", Stage: "dev", Config: map[string]any{"cloud": "aliyun", "region": "ap-southeast-1", "ackClusterId": "synthetic-ack"}}, false)
	app, e := h.Catalog.CreateObject(ctx, catalog.CreateObjectInput{Kind: "application", Name: "test-client"})
	if e != nil {
		t.Fatal(e)
	}
	h.Catalog.CreateObject(ctx, catalog.CreateObjectInput{Kind: "database", Name: "test-postgres"})
	path := t.TempDir() + "/deployments.json"
	fixture := map[string]any{"deployments": []adapters.Deployment{{ResourceID: "synthetic-deployment", Kind: "application", ObjectCode: app.Code, EnvCode: "rdev.ali", ClusterID: "synthetic-ack", Namespace: "poc", Name: "test-client-a", UID: "synthetic-uid-a", State: "Running"}, {ResourceID: "synthetic-deployment-b", Kind: "application", ObjectCode: app.Code, EnvCode: "rdev.ali", ClusterID: "synthetic-ack", Namespace: "poc", Name: "test-client-b", UID: "synthetic-uid-b", State: "Running"}}}
	b, _ := json.Marshal(fixture)
	os.WriteFile(path, b, 0600)
	h.Catalog.Infra = adapters.FixtureInfra{Path: path}
	commands := &adapters.SimulatedCommands{}
	stopCtx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-stopCtx.Done():
				return
			case <-ticker.C:
				if h.Requests.RunDirectPending(stopCtx, commands) != nil {
					return
				}
			}
		}
	}()
	listener, e := net.Listen("tcp", "127.0.0.1:8871")
	if e != nil {
		t.Fatal(e)
	}
	server := &http.Server{Handler: h}
	go server.Serve(listener)
	t.Log("Synthetic browser fixture ready on local backend")
	<-stopCtx.Done()
	server.Close()
}
