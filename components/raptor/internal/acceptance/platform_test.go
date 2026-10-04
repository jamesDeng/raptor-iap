package acceptance

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/adapters"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/backend"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/db"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/domain"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/frontend"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/openapi"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/requests"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/skills"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/testutil"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLocalHTTPPlatformAcceptance(t *testing.T) {
	ctx := context.Background()
	master := testutil.Database(t)
	if e := db.Migrate(ctx, master); e != nil {
		t.Fatal(e)
	}
	cfg := master.Config().Copy()
	cfg.ConnConfig.RuntimeParams["role"] = "raptor_app"
	runtimePool, e := pgxpool.NewWithConfig(ctx, cfg)
	if e != nil {
		t.Fatal(e)
	}
	defer runtimePool.Close()
	h := backend.New(runtimePool)
	h.RegisterService("fixture-service", "fixture-password")
	h.Mux.Handle("POST /webhooks/github", h.GitHub.Handler("fixture-webhook-secret"))
	h.Skills.Source = skills.FixtureSource{Versions: []domain.SkillsVersion{{Tag: "skills-v0.1.0", CommitSHA: strings.Repeat("a", 40)}, {Tag: "skills-v0.2.0", CommitSHA: strings.Repeat("b", 40)}}}
	if _, e = h.Auth.CreateUser(ctx, domain.User{Role: "admin"}, "fixture-user", "fixture-password-2026", "admin"); e != nil {
		t.Fatal(e)
	}
	backendServer := httptest.NewServer(h)
	defer backendServer.Close()
	openHandler, e := openapi.NewHandler(openapi.Client{BaseURL: backendServer.URL, Username: "fixture-service", Password: "fixture-password"})
	if e != nil {
		t.Fatal(e)
	}
	openServer := httptest.NewServer(openHandler)
	defer openServer.Close()
	binary := filepath.Join(t.TempDir(), "gateway")
	build := exec.Command("go", "build", "-o", binary, "./cmd/gateway")
	build.Dir = filepath.Join("..", "..", "..", "agent-gateway")
	if b, e := build.CombinedOutput(); e != nil {
		t.Fatalf("Gateway build failed: %s", b)
	}
	migrate := exec.Command(binary, "-migrate")
	databaseURL, e := url.Parse(os.Getenv("TEST_DATABASE_URL"))
	if e != nil {
		t.Fatal("test database URL invalid")
	}
	databaseURL.Path = "/" + master.Config().ConnConfig.Database
	migrate.Env = append(os.Environ(), "MIGRATION_DATABASE_URL="+databaseURL.String())
	if b, e := migrate.CombinedOutput(); e != nil {
		t.Fatalf("Gateway migration failed: %s", b)
	}
	listener, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	addr := listener.Addr().String()
	listener.Close()
	gatewayURL := "http://" + addr
	gatewayConfig := master.Config().Copy()
	gatewayConfig.ConnConfig.RuntimeParams["role"] = "gateway_app"
	u, e := url.Parse(databaseURL.String())
	if e != nil {
		t.Fatal(e)
	}
	query := u.Query()
	query.Set("role", "gateway_app")
	u.RawQuery = query.Encode()
	probe, e := pgxpool.New(ctx, u.String())
	if e != nil {
		t.Fatal("Gateway test connection invalid")
	}
	defer probe.Close()
	var owner string
	if e = probe.QueryRow(ctx, "SELECT current_user").Scan(&owner); e != nil {
		var failure *pgconn.PgError
		if errors.As(e, &failure) {
			t.Fatalf("Gateway role connection failed: %s %s", failure.Code, failure.Message)
		}
		t.Fatal("Gateway role connection failed")
	}
	if owner != "gateway_app" {
		t.Fatal("Gateway did not use its runtime role")
	}
	var accessible bool
	if e = probe.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM gateway.runtime_slot)").Scan(&accessible); e != nil {
		t.Fatal("Gateway runtime schema unavailable")
	}
	process := exec.Command(binary)
	process.Env = append(os.Environ(), "GATEWAY_DATABASE_URL="+u.String(), "GATEWAY_ADDR="+addr, "SERVICE_USERNAME=fixture-service", "SERVICE_PASSWORD=fixture-password", "GATEWAY_SIMULATION=true", "GATEWAY_SIMULATED_SCENARIO=approval-review", "GATEWAY_CHECKPOINT_DIR="+t.TempDir(), "RAPTOR_OPEN_API_URL="+openServer.URL)
	var logs bytes.Buffer
	process.Stdout = &logs
	process.Stderr = &logs
	if e = process.Start(); e != nil {
		t.Fatal(e)
	}
	defer func() { process.Process.Kill(); process.Wait() }()
	gateway := requests.HTTPGateway{BaseURL: gatewayURL, Username: "fixture-service", Password: "fixture-password"}
	h.Requests.Gateway = gateway
	deadline := time.Now().Add(10 * time.Second)
	for {
		r, _ := http.NewRequest("GET", gatewayURL+"/ready", nil)
		r.SetBasicAuth("fixture-service", "fixture-password")
		response, e := http.DefaultClient.Do(r)
		if e == nil {
			response.Body.Close()
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("Gateway failed to listen")
		}
		time.Sleep(20 * time.Millisecond)
	}
	frontHandler, _ := frontend.NewHandler(backendServer.URL)
	frontServer := httptest.NewServer(frontHandler)
	defer frontServer.Close()
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar}
	csrf := ""
	call := func(method, path string, input any) map[string]any {
		t.Helper()
		b, _ := json.Marshal(input)
		r, _ := http.NewRequest(method, frontServer.URL+"/api/v1"+path, bytes.NewReader(b))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-CSRF-Token", csrf)
		if method == "POST" && path == "/requests" {
			r.Header.Set("Idempotency-Key", domain.NewID())
		}
		response, e := client.Do(r)
		if e != nil {
			t.Fatal(e)
		}
		defer response.Body.Close()
		var v map[string]any
		if json.NewDecoder(response.Body).Decode(&v) != nil {
			t.Fatal("invalid response")
		}
		if response.StatusCode >= 300 {
			t.Fatalf("%s %s rejected: %d", method, path, response.StatusCode)
		}
		data, _ := v["data"].(map[string]any)
		return data
	}
	session := call("POST", "/login", map[string]string{"username": "fixture-user", "password": "fixture-password-2026"})
	csrf = session["csrf"].(string)
	call("POST", "/environment-groups", map[string]string{"code": "fixture", "name": "Synthetic fixture"})
	call("POST", "/environments", domain.Environment{Code: "rdev.ali", GroupCode: "fixture", Stage: "dev", Config: map[string]any{"ackClusterId": "synthetic-ack"}})
	object := call("POST", "/objects", map[string]string{"kind": "database", "name": "fixture-db"})
	code := object["code"].(string)
	if code == "" {
		t.Fatal("object code missing")
	}
	var deployed int
	master.QueryRow(ctx, "SELECT count(*) FROM raptor.requests").Scan(&deployed)
	if deployed != 0 {
		t.Fatal("object creation deployed infrastructure")
	}
	request := call("POST", "/requests", domain.RequestInput{Type: "agent", Object: domain.ObjectRef{Kind: "database", Code: code}, EnvCode: "rdev.ali", Skills: domain.SkillsVersion{Tag: "skills-v0.1.0", CommitSHA: strings.Repeat("a", 40)}, Operations: []domain.Operation{{Name: "database.deploy", Parameters: map[string]any{"engineVersion": "synthetic", "instanceClass": "synthetic", "storageGiB": 20}}}})
	id := request["id"].(string)
	if e = h.Requests.DispatchPending(ctx, gateway); e != nil {
		t.Fatal(e)
	}
	lastAttempt := ""
	wait := func(status string) {
		t.Helper()
		deadline := time.Now().Add(8 * time.Second)
		for {
			v, e := h.Requests.View(ctx, id)
			attempt, _ := v.Execution["attemptId"].(string)
			if e == nil && v.ExecutionAvailable && v.Request.Status == status && attempt != lastAttempt {
				lastAttempt = attempt
				return
			}
			if time.Now().After(deadline) {
				t.Fatalf("request did not reach %s", status)
			}
			time.Sleep(20 * time.Millisecond)
		}
	}
	wait("waiting_approval")
	call("POST", "/requests/"+id+"/skills", map[string]string{"tag": "skills-v0.2.0", "commitSha": strings.Repeat("b", 40), "strategy": "next-pause"})
	if e = h.Requests.DispatchPending(ctx, gateway); e != nil {
		t.Fatal(e)
	}
	serviceClient := openapi.Client{BaseURL: openServer.URL, Username: "fixture-service", Password: "fixture-password"}
	binding := map[string]any{"requestId": id, "actionId": domain.NewID(), "interface": "ess.scale-in", "envCode": "rdev.ali", "target": map[string]string{"groupId": "synthetic-group"}, "parameters": map[string]int{"desiredCapacity": 1}}
	approvalResult, e := serviceClient.Call(ctx, "POST", "/v1/requests/"+id+"/approvals", binding)
	if e != nil {
		t.Fatal(e)
	}
	approval := approvalResult["data"].(map[string]any)
	call("POST", "/requests/"+id+"/approvals/"+approval["approvalId"].(string)+"/decision", map[string]string{"decision": "approve"})
	if e = h.Requests.DispatchPending(ctx, gateway); e != nil {
		t.Fatal(e)
	}
	wait("waiting_review")
	head := strings.Repeat("c", 40)
	if _, e = serviceClient.Call(ctx, "POST", "/v1/requests/"+id+"/pull-requests", map[string]any{"repository": "synthetic/repo", "number": 1, "url": "https://github.com/synthetic/repo/pull/1", "headSha": head}); e != nil {
		t.Fatal(e)
	}
	webhook := func(delivery, event string, input any) {
		t.Helper()
		b, _ := json.Marshal(input)
		mac := hmac.New(sha256.New, []byte("fixture-webhook-secret"))
		mac.Write(b)
		r, _ := http.NewRequest("POST", backendServer.URL+"/webhooks/github", bytes.NewReader(b))
		r.Header.Set("X-Hub-Signature-256", "sha256="+hex.EncodeToString(mac.Sum(nil)))
		r.Header.Set("X-GitHub-Delivery", delivery)
		r.Header.Set("X-GitHub-Event", event)
		resp, e := http.DefaultClient.Do(r)
		if e != nil {
			t.Fatal(e)
		}
		resp.Body.Close()
		if resp.StatusCode != 202 {
			t.Fatal("webhook not durably accepted")
		}
	}
	webhook("feedback", "pull_request_review", map[string]any{"action": "submitted", "repository": map[string]string{"full_name": "synthetic/repo"}, "pull_request": map[string]any{"number": 1, "head": map[string]string{"sha": head}}, "review": map[string]string{"state": "changes_requested", "commit_id": head, "body": "Synthetic submitted feedback"}})
	h.Requests.DispatchPending(ctx, gateway)
	wait("waiting_review")
	webhook("merge", "pull_request", map[string]any{"action": "closed", "repository": map[string]string{"full_name": "synthetic/repo"}, "pull_request": map[string]any{"number": 1, "merged": true, "head": map[string]string{"sha": head}}})
	h.Requests.DispatchPending(ctx, gateway)
	wait("completed")
	final, e := h.Requests.View(ctx, id)
	if e != nil {
		t.Fatal(e)
	}
	applied, _ := final.Execution["appliedSkills"].(map[string]any)
	if applied["tag"] != "skills-v0.2.0" {
		t.Fatal("selected skills not applied after restore")
	}
	timeline, e := h.Requests.Timeline(ctx, id, 0)
	if e != nil || len(timeline.Events) == 0 {
		t.Fatal("no durable progress")
	}
	for _, event := range timeline.Events {
		if event.EvidenceMode != "simulated" {
			t.Fatal("acceptance presented non-simulated evidence")
		}
	}
	restarted := backend.New(runtimePool)
	history, e := restarted.Requests.Timeline(ctx, id, 0)
	if e != nil || len(history.Events) == 0 || !history.SyncUnavailable {
		t.Fatal("saved progress lost on independent backend restart")
	}
	denied := call("POST", "/requests", domain.RequestInput{Type: "agent", Object: domain.ObjectRef{Kind: "database", Code: code}, EnvCode: "rdev.ali", Skills: domain.SkillsVersion{Tag: "skills-v0.1.0", CommitSHA: strings.Repeat("a", 40)}, Operations: []domain.Operation{{Name: "database.deploy", Parameters: map[string]any{"engineVersion": "synthetic", "instanceClass": "synthetic", "storageGiB": 20}}}})
	id = denied["id"].(string)
	lastAttempt = ""
	h.Requests.DispatchPending(ctx, gateway)
	wait("waiting_approval")
	binding["requestId"] = id
	binding["actionId"] = domain.NewID()
	approvalResult, e = serviceClient.Call(ctx, "POST", "/v1/requests/"+id+"/approvals", binding)
	if e != nil {
		t.Fatal(e)
	}
	approval = approvalResult["data"].(map[string]any)
	call("POST", "/requests/"+id+"/approvals/"+approval["approvalId"].(string)+"/decision", map[string]string{"decision": "deny", "next": "block"})
	h.Requests.DispatchPending(ctx, gateway)
	deadline = time.Now().Add(8 * time.Second)
	for {
		view, e := h.Requests.View(ctx, id)
		if e == nil && view.Execution["status"] == "blocked" && view.Execution["cleanup"] == "confirmed" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("denial did not stop the simulated runtime")
		}
		time.Sleep(20 * time.Millisecond)
	}
	app := call("POST", "/objects", map[string]string{"kind": "application", "name": "synthetic-client"})
	appCode := app["code"].(string)
	targets := []domain.RestartTarget{{AppCode: appCode, EnvCode: "rdev.ali", ClusterID: "synthetic-ack", Namespace: "poc", Name: "a", UID: "uid-a"}, {AppCode: appCode, EnvCode: "rdev.ali", ClusterID: "synthetic-ack", Namespace: "poc", Name: "b", UID: "uid-b"}}
	deployments := []adapters.Deployment{}
	for _, target := range targets {
		deployments = append(deployments, adapters.Deployment{ResourceID: target.UID, Kind: "application", EnvCode: target.EnvCode, ObjectCode: target.AppCode, ClusterID: target.ClusterID, Namespace: target.Namespace, Name: target.Name, UID: target.UID, State: "Running"})
	}
	fixturePath := filepath.Join(t.TempDir(), "deployments.json")
	fixtureBody, _ := json.Marshal(map[string]any{"deployments": deployments})
	os.WriteFile(fixturePath, fixtureBody, 0600)
	h.Catalog.Infra = adapters.FixtureInfra{Path: fixturePath}
	direct := call("POST", "/requests", domain.RequestInput{Type: "direct", Operation: "application.restart", Targets: targets})
	directID := direct["id"].(string)
	if e = h.Requests.RunRestartBatch(ctx, directID, &partialCommands{}); e != nil {
		t.Fatal(e)
	}
	items, e := h.Requests.RestartItems(ctx, directID)
	if e != nil || len(items) != 2 {
		t.Fatal("missing restart target receipt")
	}
	counts := map[string]int{}
	for _, item := range items {
		counts[item.State]++
		var details map[string]any
		json.Unmarshal(item.Details, &details)
		if details["evidenceMode"] != "simulated" {
			t.Fatal("restart evidence not simulated")
		}
	}
	if counts["succeeded"] != 1 || counts["failed"] != 1 {
		t.Fatal("partial restart result lost")
	}
	t.Log("Local HTTP acceptance: catalog, dispatch, approval/denial, signed review/merge, skills restore, saved history and direct partial restart; all execution simulated")
}

type partialCommands struct{ simulated adapters.SimulatedCommands }

func (c *partialCommands) GetDeploymentStatus(ctx context.Context, target domain.RestartTarget) (adapters.DeploymentStatus, error) {
	return c.simulated.GetDeploymentStatus(ctx, target)
}
func (c *partialCommands) RestartDeployment(ctx context.Context, id string, target domain.RestartTarget) error {
	if target.Name == "b" {
		return adapters.ErrRejected
	}
	return c.simulated.RestartDeployment(ctx, id, target)
}
