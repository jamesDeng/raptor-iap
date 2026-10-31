package api

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"raptor-iap/infra-api/internal/approval"
	"raptor-iap/infra-api/internal/commands"
	"raptor-iap/infra-api/internal/domain"
	"raptor-iap/infra-api/internal/policy"
	"raptor-iap/infra-api/internal/resources"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type capacityResources struct {
	mu     sync.RWMutex
	proof  resources.Proof
	reads  atomic.Int32
	proofs atomic.Int32
}

func (f *capacityResources) snapshot() resources.Proof {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.proof
}
func (f *capacityResources) setProof(p resources.Proof) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.proof = p
}
func (f *capacityResources) ResolveProof(context.Context, string, string) (resources.Proof, error) {
	f.proofs.Add(1)
	return f.snapshot(), nil
}
func (f *capacityResources) Resolve(_ context.Context, env, kind, code string) (domain.ResolvedResource, error) {
	f.reads.Add(1)
	i := f.snapshot().Identity
	p := domain.ProxyMapping{Code: i.ProxyCode, GroupID: i.GroupID, ServerGroupID: i.ServerGroupID, ListenerID: i.ListenerID, Port: i.Port, TargetDBCode: i.TargetDBCode}
	a := i.Application
	return domain.ResolvedResource{Context: domain.ResourceContext{EnvCode: env, Kind: kind, Code: code}, Environment: domain.Environment{Code: env, AccountID: i.AccountID, Region: i.Region, ClusterID: i.ClusterID, Proxies: []domain.ProxyMapping{p}}, Proxy: &p, PgproxyWorkload: &domain.PgproxyWorkload{Type: "ess", Code: i.ProxyCode, WorkloadKey: "pk", TargetDBCode: i.TargetDBCode}, DatabaseWorkloads: []domain.DatabaseWorkload{{Type: "rds", Code: i.TargetDBCode, ResourceID: i.DatabaseResourceID}}, ApplicationWorkloads: []domain.ApplicationWorkload{{Type: "kubernetes", WorkloadKey: "wk", ClusterID: a.ClusterID, Namespace: a.Namespace, Name: a.Name, UID: a.UID, Target: domain.Target{EnvCode: a.EnvCode, AppCode: a.AppCode, ClusterID: a.ClusterID, Namespace: a.Namespace, Name: a.Name, UID: a.UID}}}}, nil
}

type capacityBackend struct {
	proxy  string
	writes atomic.Int32
}

func (b *capacityBackend) Snapshot(context.Context, domain.Environment, string) ([]policy.ProxySnapshot, error) {
	return []policy.ProxySnapshot{{EnvCode: "dev", ProxyCode: b.proxy, GroupID: "g", Desired: 2, Min: 0, Max: 5, Nodes: []policy.Node{{ID: "i"}}}}, nil
}
func (b *capacityBackend) Scale(context.Context, domain.Environment, string, int) (string, error) {
	b.writes.Add(1)
	return "synthetic-receipt", nil
}
func (b *capacityBackend) Protect(context.Context, domain.Environment, string, []string, bool) error {
	return nil
}
func (b *capacityBackend) Deregister(context.Context, domain.Environment, string, []string) error {
	return nil
}

type capacityMetrics struct{}

func (capacityMetrics) ConnectedClients(context.Context, policy.ProxySnapshot) ([]policy.ClientObservation, error) {
	return []policy.ClientObservation{{InstanceID: "i", ObservedAt: time.Now()}}, nil
}

// A nested proof invocation when both slots are occupied must fail this test.
// Real Raptor services/DB/HTTPInfra, actual Infra handlers/inspector/approval
// clients and Runtime run across TLS; only provider reads/writes are synthetic.
func TestResourceBoundedPrivateHTTPGraph(t *testing.T) {
	binary := os.Getenv("RAPTOR_GRAPH_PEER_BINARY")
	if binary == "" {
		t.Skip("requires compiled Raptor backend peer and TEST_DATABASE_URL")
	}
	var handler http.Handler
	handlerReady := make(chan struct{})
	slots := make(chan struct{}, 2)
	var refused atomic.Int32
	var barrierMu sync.Mutex
	var barrier chan struct{}
	var entered int
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case slots <- struct{}{}:
			defer func() { <-slots }()
		default:
			refused.Add(1)
			http.Error(w, "synthetic FC invocation capacity", 429)
			return
		}
		proof := r.Header.Get("Accept") == "application/vnd.raptor.resolution-proof+json" || r.URL.Path == "/v1/resource-resolution-proof"
		if !proof {
			barrierMu.Lock()
			b := barrier
			if b != nil {
				entered++
				if entered == 2 {
					close(b)
					barrier = nil
				}
			}
			barrierMu.Unlock()
			if b != nil {
				select {
				case <-b:
				case <-r.Context().Done():
					return
				}
			}
		}
		<-handlerReady
		handler.ServeHTTP(w, r)
	}))
	defer server.Close()
	dir := t.TempDir()
	ready := filepath.Join(dir, "ready.json")
	peer := exec.Command(binary, "-test.run=^TestResourceCapacityPeer$", "-test.timeout=60s")
	peer.Env = append(os.Environ(), "RAPTOR_GRAPH_READY="+ready, "RAPTOR_GRAPH_INFRA="+server.URL)
	stdin, e := peer.StdinPipe()
	if e != nil {
		t.Fatal(e)
	}
	logFile, e := os.Create(filepath.Join(dir, "peer.log"))
	if e != nil {
		t.Fatal(e)
	}
	defer logFile.Close()
	peer.Stdout = logFile
	peer.Stderr = logFile
	if e = peer.Start(); e != nil {
		t.Fatal(e)
	}
	peerFinished := make(chan error, 1)
	go func() { peerFinished <- peer.Wait(); close(peerFinished) }()
	t.Cleanup(func() {
		stdin.Close()
		if e := <-peerFinished; e != nil {
			raw, _ := os.ReadFile(logFile.Name())
			t.Errorf("peer cleanup: %v %s", e, raw)
		}
	})
	var info struct {
		Origin, Credential, RequestID, QualificationCredential string
		Proof                                                  resources.Proof
		Actions                                                []string
	}
	deadline := time.Now().Add(20 * time.Second)
	for {
		select {
		case e := <-peerFinished:
			raw, _ := os.ReadFile(logFile.Name())
			t.Fatalf("peer startup exited: %v %s", e, raw)
		default:
		}
		raw, err := os.ReadFile(ready)
		if err == nil && json.Unmarshal(raw, &info) == nil {
			break
		}
		if time.Now().After(deadline) {
			raw, _ := os.ReadFile(logFile.Name())
			t.Fatalf("peer startup: %s", raw)
		}
		time.Sleep(10 * time.Millisecond)
	}
	client := &http.Client{Timeout: 10 * time.Second, Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}} // loopback test certificates only
	resolver := &capacityResources{proof: info.Proof}
	inspector, e := NewResourceAgentInspector(info.Origin, "fixture-inspector", "fixture-inspector-password", client, resolver.ResolveProof)
	if e != nil {
		t.Fatal(e)
	}
	approvals, e := approval.NewResource(info.Origin, "fixture-controller", "fixture-controller-password", client, resolver.ResolveProof)
	if e != nil {
		t.Fatal(e)
	}
	backend := &capacityBackend{proxy: info.Proof.Identity.ProxyCode}
	runtime := commands.Runtime{Backend: backend, Approval: approvals, Claims: approvals, Metrics: capacityMetrics{}, EvidenceMode: "synthetic"}
	handler, e = NewResourceServer(&capacityReader{}, resolver, "user", "secret", "X-Infra-Authorization", runtime, NewServiceAuthorizer("user", "/v1/db-proxy-scale"), inspector)
	if e != nil {
		t.Fatal(e)
	}
	close(handlerReady)
	agent := "Basic " + base64.StdEncoding.EncodeToString([]byte("agent:"+info.Credential))
	invoke := func(method, path string, body any, auth string) (int, string) {
		raw, _ := json.Marshal(body)
		req, _ := http.NewRequest(method, server.URL+path, bytes.NewReader(raw))
		req.Header.Set("X-Infra-Authorization", auth)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json, text/event-stream")
		resp, err := client.Do(req)
		if err != nil {
			return 0, err.Error()
		}
		defer resp.Body.Close()
		data, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, string(data)
	}
	batch := func(t *testing.T, method, path, auth string, bodies []any) {
		barrierMu.Lock()
		barrier = make(chan struct{})
		entered = 0
		barrierMu.Unlock()
		var wg sync.WaitGroup
		for _, body := range bodies {
			wg.Add(1)
			go func(body any) {
				defer wg.Done()
				status, data := invoke(method, path, body, auth)
				if status != 200 || strings.Contains(data, `"isError":true`) {
					t.Errorf("valid saturated %s failed: %d %s", path, status, data)
				}
			}(body)
		}
		wg.Wait()
	}
	t.Run("concurrent-agent-reads", func(t *testing.T) {
		batch(t, "GET", "/v1/deployments?envCode=dev&kind=db-proxy&code="+backend.proxy, agent, []any{nil, nil})
	})
	// Ordinary related-resource reads must also perform the complete proof,
	// even when the selected workload itself has not changed.
	for _, path := range []string{
		"/v1/deployments?envCode=dev&kind=application&code=" + info.Proof.Identity.Application.AppCode,
		"/v1/deployments?envCode=dev&kind=database&code=" + info.Proof.Identity.TargetDBCode,
		"/v1/deployment-status?envCode=dev&appCode=" + info.Proof.Identity.Application.AppCode + "&workloadKey=wk",
	} {
		t.Run(path, func(t *testing.T) {
			before := resolver.proofs.Load()
			batch(t, "GET", path, agent, []any{nil, nil})
			if resolver.proofs.Load()-before != 2 {
				t.Fatal("ordinary reads lacked full fresh proof")
			}
		})
	}
	for _, tool := range []string{"proxy_observation_get", "proxy_pgcat_metrics_get"} {
		t.Run(tool, func(t *testing.T) {
			args := map[string]any{"requestId": info.RequestID, "envCode": "dev", "proxyCode": backend.proxy}
			if tool == "proxy_observation_get" {
				args["kind"] = "fleet"
			} else {
				args["series"] = "active"
			}
			body := map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/call", "params": map[string]any{"name": tool, "arguments": args}}
			before := resolver.proofs.Load()
			batch(t, "POST", "/mcp", agent, []any{body, body})
			if resolver.proofs.Load()-before != 4 {
				t.Fatal("proxy reads lacked fresh full proof and runtime identity check")
			}
		})
	}
	t.Run("concurrent-mcp-discovery", func(t *testing.T) {
		before := resolver.proofs.Load()
		body := map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/list"}
		batch(t, "POST", "/mcp", agent, []any{body, body})
		if resolver.proofs.Load()-before != 2 {
			t.Fatal("discovery reused authorization")
		}
	})
	// Service calls isolate the newly introduced approval callback from token
	// introspection, proving Check AND Claim need a local fresh proof path.
	t.Run("concurrent-approval-commands", func(t *testing.T) {
		bodies := []any{}
		for _, action := range info.Actions[:2] {
			bodies = append(bodies, map[string]any{"requestId": info.RequestID, "actionId": action, "envCode": "dev", "proxyCode": backend.proxy, "desiredCapacity": 1})
		}
		batch(t, "POST", "/v1/db-proxy-scale", valid(), bodies)
	})
	t.Run("concurrent-agent-approval-commands", func(t *testing.T) {
		bodies := []any{}
		for _, action := range info.Actions[2:] {
			bodies = append(bodies, map[string]any{"requestId": info.RequestID, "actionId": action, "envCode": "dev", "proxyCode": backend.proxy, "desiredCapacity": 1})
		}
		batch(t, "POST", "/v1/db-proxy-scale", agent, bodies)
	})
	t.Run("concurrent-qualification-reads", func(t *testing.T) {
		auth := "Basic " + base64.StdEncoding.EncodeToString([]byte("agent:"+info.QualificationCredential))
		before := resolver.proofs.Load()
		batch(t, "GET", "/v1/deployments?envCode=dev&kind=database&code="+info.Proof.Identity.TargetDBCode, auth, []any{nil, nil})
		if resolver.proofs.Load()-before != 2 {
			t.Fatal("qualification lacked fresh proof")
		}
	})
	if refused.Load() != 0 || backend.writes.Load() != 4 {
		t.Errorf("nested-slot refusals=%d provider submissions=%d; want 0 and 4", refused.Load(), backend.writes.Load())
	}
	beforeReads, beforeProofs := resolver.reads.Load(), resolver.proofs.Load()
	if status, _ := invoke("GET", "/v1/deployments?envCode=dev&kind=db-proxy&code=foreign", nil, agent); status == 200 {
		t.Error("foreign selector authorized")
	}
	if resolver.reads.Load() != beforeReads || resolver.proofs.Load() != beforeProofs {
		t.Error("foreign selector reached provider")
	}
	for _, action := range info.Actions {
		status, _ := invoke("POST", "/v1/db-proxy-scale", map[string]any{"requestId": info.RequestID, "actionId": action, "envCode": "dev", "proxyCode": backend.proxy, "desiredCapacity": 1}, valid())
		if status == 200 {
			t.Error("replayed approval submitted")
		}
	}
	if backend.writes.Load() != 4 {
		t.Errorf("replay provider submissions=%d", backend.writes.Load())
	}
	for _, field := range []string{"listener", "database", "uid"} {
		t.Run("fresh-provider-drift/"+field, func(t *testing.T) {
			changed := info.Proof
			switch field {
			case "listener":
				changed.Identity.ListenerID = "changed"
			case "database":
				changed.Identity.DatabaseResourceID = "changed"
			case "uid":
				changed.Identity.Application.UID = "changed"
			}
			changed.ResolutionDigest, _ = resources.Digest(changed.Identity)
			resolver.setProof(changed)
			defer resolver.setProof(info.Proof)
			before := resolver.reads.Load()
			status, body := invoke("GET", "/v1/deployments?envCode=dev&kind=database&code="+info.Proof.Identity.TargetDBCode, nil, agent)
			if status != 403 || resolver.reads.Load() != before {
				t.Fatalf("changed complete proof reached selected read: %d %s", status, body)
			}
		})
	}
	t.Logf("bounded graph: invocation capacity=2, nested-slot refusals=%d, provider submissions=%d, local fresh proofs=%d", refused.Load(), backend.writes.Load(), resolver.proofs.Load())
}

// Only external provider behavior is synthetic; each status uses its selected UID.
type capacityReader struct{ observedReader }

func (*capacityReader) Status(_ context.Context, _ domain.Environment, target domain.Target) (domain.Status, error) {
	return domain.Status{UID: target.UID, ReadyReplicas: 1, EvidenceMode: "synthetic", ObservedAt: time.Now()}, nil
}
func (*capacityReader) ObserveProxy(_ context.Context, _ domain.Environment, target domain.ProxyReadTarget, kind string) (domain.ProxyObservation, error) {
	return domain.ProxyObservation{Fleet: &domain.FleetObservation{GroupID: target.GroupID, ObservedAt: time.Now(), Instances: []domain.FleetNode{}}}, nil
}
func (*capacityReader) ObservePgcatSeries(_ context.Context, _ domain.Environment, target domain.ProxyReadTarget, series string) (domain.PgcatSeriesObservation, error) {
	return domain.PgcatSeriesObservation{Series: series, ObservedAt: time.Now(), Rows: []domain.PrometheusSample{}}, nil
}
