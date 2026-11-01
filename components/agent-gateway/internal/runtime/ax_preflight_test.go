package runtime

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jamesDeng/raptor-iap/components/agent-gateway/internal/execution"
)

type preflightModel struct {
	calls   int
	before  func()
	failure error
}

func (m *preflightModel) Lease(context.Context, execution.AttemptBinding) (ModelLease, error) {
	m.calls++
	if m.before != nil {
		m.before()
	}
	return ModelLease{Credential: json.RawMessage(`{"token":"fixture-model"}`), Generation: 1, ExpiresAt: time.Now().Add(time.Minute)}, m.failure
}
func (*preflightModel) Refresh(context.Context, execution.AttemptBinding, int64, json.RawMessage) (int64, error) {
	return 0, ErrRuntimeUnavailable
}
func (*preflightModel) Valid(context.Context, execution.AttemptBinding) error { return nil }

func qualificationSkills(t *testing.T) (string, execution.SkillsVersion) {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	git := func(args ...string) string {
		t.Helper()
		raw, e := exec.Command("git", append([]string{"-C", root}, args...)...).CombinedOutput()
		if e != nil {
			t.Fatal(e, string(raw))
		}
		return strings.TrimSpace(string(raw))
	}
	git("init")
	git("config", "user.name", "fixture")
	git("config", "user.email", "fixture@example.invalid")
	if err = os.MkdirAll(filepath.Join(root, "agent-skills/pgcat-replacement"), 0700); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(root, "agent-skills/pgcat-replacement/SKILL.md"), []byte("qualification fixture\nreplacement-contract: direct-v2\n"), 0600); err != nil {
		t.Fatal(err)
	}
	git("add", ".")
	git("commit", "-m", "fixture")
	sha := git("rev-parse", "HEAD")
	git("tag", "skills-v0.4.0")
	return root, execution.SkillsVersion{Tag: "skills-v0.4.0", CommitSHA: sha}
}
func qualificationMaterial(in execution.LiveStart) ObservationMaterial {
	s := in.Access.ReplacementScope
	raw, _ := json.Marshal(map[string]any{"version": 1, "requestId": in.Binding.RequestID, "definitionSha256": in.Binding.DefinitionSHA256, "envCode": s.EnvCode, "accountId": s.AccountID, "region": s.Region, "groupId": s.GroupID, "serverGroupId": s.ServerGroupID, "listenerId": s.ListenerID, "dbInstanceId": s.DatabaseResourceID, "expiresAt": time.Now().Add(800 * time.Second), "application": s.Application, "private": map[string]string{"kubeconfig": "/tmp/raptor-private/kubeconfig", "password": "private-preflight-source"}})
	return ObservationMaterial{Config: raw, Kubeconfig: observerKubeFixture("system:serviceaccount:raptor-test:client-observer"), Secrets: []string{"private-preflight-source"}}
}
func axQualificationFixture(t *testing.T) (*AXLive, *fixtureBridge, execution.LiveStart, *preflightModel) {
	t.Helper()
	a, f, in := axFixture(t)
	var checkpointError error
	in.Checkpoint, checkpointError = a.Verifier.VerifyCheckpoint(context.Background(), in.Checkpoint)
	if checkpointError != nil {
		t.Fatal(checkpointError)
	}
	a.Config.SkillsRepository, in.Skills = qualificationSkills(t)
	b := in.Binding
	b.Operation = "db-proxy.replace-nodes"
	b.ExecutionMode = "qualification"
	b.ObjectKind = "db-proxy"
	b.ObjectCode = "proxy"
	b.ClusterID = "cluster"
	b.SkillsCommit = in.Skills.CommitSHA
	b.DefinitionSHA256 = strings.Repeat("b", 64)
	b.ProviderID = "codex"
	b.ConnectionVersion = 3
	in.Binding = b
	in.Access.Binding = b
	in.Access.Credential = "raptor_at_" + strings.Repeat("a", 43)
	in.Access.ExpiresAt = in.Deadline
	s := *proxyStart().Access.ReplacementScope
	in.Access.ReplacementScope = &s
	raw, _ := json.Marshal(b)
	if _, e := a.Store.Pool.Exec(context.Background(), "UPDATE gateway.attempts SET binding=$2,definition_sha256=$3 WHERE id=$1", b.AttemptID, raw, b.DefinitionSHA256); e != nil {
		t.Fatal(e)
	}
	a.Observe = func(_ context.Context, in execution.LiveStart) (ObservationMaterial, error) {
		return qualificationMaterial(in), nil
	}
	m := &preflightModel{}
	a.ModelCredentials = m
	return a, f, in, m
}
func TestAXQualificationPreflightBeforeModelAndRuntime(t *testing.T) {
	for _, mode := range []string{"failure", "absent", "cancel", "lease", "invalid-checkpoint"} {
		t.Run(mode, func(t *testing.T) {
			a, f, in, model := axQualificationFixture(t)
			reads := 0
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			a.Observations, _ = NewObservationProxy("https://api.rdev.raptor-iap.top"+observationRoute, func(ctx context.Context, got execution.LiveStart, m ObservationMaterial, tool string, args json.RawMessage) (json.RawMessage, error) {
				reads++
				if model.calls != 0 || len(a.axRuns) != 0 || len(f.calls) != 0 || !bytes.Contains(m.Config, []byte("private-preflight-source")) {
					t.Fatal("preflight ordering/private entry")
				}
				if mode == "cancel" {
					cancel()
				}
				if mode == "lease" {
					a.Owner = "foreign"
				}
				if mode == "failure" {
					return nil, errors.New("private-preflight-source")
				}
				return []byte(`{"nodes":[]}`), nil
			})
			if mode == "absent" {
				a.Observations = nil
			}
			if mode == "invalid-checkpoint" {
				in.Checkpoint.SHA256 = strings.Repeat("0", 64)
			}
			var logs bytes.Buffer
			previous := log.Writer()
			log.SetOutput(&logs)
			defer log.SetOutput(previous)
			h, err := a.Start(ctx, in)
			if err == nil || model.calls != 0 || len(f.calls) != 0 || len(a.axRuns) != 0 || h.RequestID != in.Binding.RequestID || h.ID != "" {
				t.Fatalf("preflight failure reached model/runtime or lost handle: err=%v leases=%d calls=%d runs=%d handle=%+v", err, model.calls, len(f.calls), len(a.axRuns), h)
			}
			if mode != "invalid-checkpoint" && (!strings.Contains(logs.String(), "category=failed") || strings.Contains(logs.String(), "private-preflight-source")) {
				t.Fatal("missing/unsafe failure receipt", logs.String())
			}
			want := 1
			if mode == "absent" || mode == "invalid-checkpoint" {
				want = 0
			}
			if reads != want {
				t.Fatal("missing actual preflight", reads, want)
			}
			// Recovery cleanup works even when Start never registered an AX run.
			a.Owner = "owner"
			out, e := a.Stop(context.Background(), h)
			if e != nil || !out.SandboxAbsent || !out.KeyAbsent {
				t.Fatal("early cleanup failed", out, e)
			}
			if a.Observations != nil && len(a.Observations.entries) != 0 {
				t.Fatal("registered observation retained")
			}
		})
	}
}
func TestAXQualificationPreflightSuccessAndResumeDiscardEvidence(t *testing.T) {
	for _, resume := range []bool{false, true} {
		t.Run(map[bool]string{false: "initial", true: "resume"}[resume], func(t *testing.T) {
			a, f, in, model := axQualificationFixture(t)
			reads := 0
			if resume {
				in.SessionCheckpoint = &execution.SessionCheckpoint{RequestID: in.Binding.RequestID, DefinitionSHA256: in.Binding.DefinitionSHA256, SkillsCommit: in.Binding.SkillsCommit, Model: in.Binding.Model, SessionID: "session", SessionFile: "session.jsonl", SessionSHA256: strings.Repeat("d", 64), Archive: in.Checkpoint}
			}
			a.Observations, _ = NewObservationProxy("https://api.rdev.raptor-iap.top"+observationRoute, func(_ context.Context, got execution.LiveStart, m ObservationMaterial, tool string, args json.RawMessage) (json.RawMessage, error) {
				reads++
				if model.calls != 0 || len(a.axRuns) != 0 || len(f.calls) != 0 || tool != "metrics_read" || string(args) != `{"kind":"pgcatClients"}` {
					t.Fatal("pre-model fixed preflight order")
				}
				return []byte(`{"marker":"preflight-only-evidence"}`), nil
			})
			model.before = func() {
				if reads != 1 {
					t.Fatal("model leased before successful preflight")
				}
			}
			var logs bytes.Buffer
			previous := log.Writer()
			log.SetOutput(&logs)
			defer log.SetOutput(previous)
			h, err := a.Start(context.Background(), in)
			if err != nil {
				t.Fatal(err)
			}
			if reads != 1 || model.calls != 1 {
				t.Fatal("preflight not repeated for each Start", reads, model.calls)
			}
			for _, call := range f.calls {
				if bytes.Contains(call.Data, []byte("preflight-only-evidence")) {
					t.Fatal("preflight bytes injected into runtime")
				}
			}
			if !strings.Contains(logs.String(), "category=started") || !strings.Contains(logs.String(), "category=succeeded") || strings.Contains(logs.String(), "preflight-only-evidence") || strings.Contains(logs.String(), "private-preflight-source") {
				t.Fatal("missing/unsafe operator receipt", logs.String())
			}
			if _, err = a.Stop(context.Background(), h); err != nil {
				t.Fatal(err)
			}
		})
	}
}

type preflightLease struct{ valid bool }

func (l *preflightLease) Valid(context.Context) bool { return l.valid }

type preflightAccess struct {
	config          LiveConfig
	scope           execution.ReplacementScope
	issued, revoked int
}

func (a *preflightAccess) Issue(_ context.Context, b execution.AttemptBinding, _ string, until time.Time) (execution.AgentAccess, error) {
	a.issued++
	return execution.AgentAccess{Binding: b, Credential: "raptor_at_" + strings.Repeat("a", 43), RaptorMcpURL: a.config.RaptorMcpURL, InfraMcpURL: a.config.InfraMcpURL, ReplacementScope: &a.scope, ExpiresAt: until.Add(-time.Second)}, nil
}
func (a *preflightAccess) Revoke(ctx context.Context, _ execution.AttemptBinding) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	a.revoked++
	return nil
}

type preflightRaptor struct{ input execution.ExecutionInput }

func (r preflightRaptor) Context(context.Context, string) (execution.ExecutionInput, error) {
	return r.input, nil
}
func workerQualificationInput(t *testing.T, id string, skills execution.SkillsVersion) execution.ExecutionInput {
	t.Helper()
	definition, err := json.Marshal(map[string]any{"type": "agent", "model": "gpt-6-luna", "providerId": "codex", "connectionVersion": 3, "object": map[string]string{"kind": "db-proxy", "code": "proxy"}, "envCode": "rdev.ali", "operations": []any{map[string]any{"name": "db-proxy.replace-nodes", "parameters": map[string]string{"mode": "qualification"}}}, "skills": skills})
	if err != nil {
		t.Fatal(err)
	}
	var canonical any
	if err = json.Unmarshal(definition, &canonical); err != nil {
		t.Fatal(err)
	}
	definition, err = json.Marshal(canonical)
	if err != nil {
		t.Fatal(err)
	}
	return execution.ExecutionInput{RequestID: id, Skills: skills, Definition: definition, DefinitionSHA256: fmt.Sprintf("%x", sha256.Sum256(definition)), Environment: json.RawMessage(`{"code":"rdev.ali","config":{"ackClusterId":"cluster"}}`)}
}
func TestAXQualificationWorkerPreflightFailureCleansIssuedAccessAndSlot(t *testing.T) {
	for _, mode := range []string{"failure", "cancel-signal", "context-cancel", "lease-loss"} {
		t.Run(mode, func(t *testing.T) {
			a, f, seed, model := axQualificationFixture(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			// Finish the fixture's seed claim without claiming any new checkpoint was made.
			if e := a.Store.FinalizeLive(ctx, seed.Binding.AttemptID, a.Owner, execution.LiveOutcome{Status: "failed", FailureCode: "Interrupted", SandboxAbsent: true, KeyAbsent: true, AccessRevoked: true}); e != nil {
				t.Fatal(e)
			}
			id := execution.NewID()
			if _, e := a.Store.Receive(ctx, id); e != nil {
				t.Fatal(e)
			}
			access := &preflightAccess{config: a.Config, scope: *seed.Access.ReplacementScope}
			lease := &preflightLease{valid: true}
			a.Lease = lease
			providers := &Providers{Default: "ax", Backends: map[string]execution.LiveRuntime{"ax": a}, Store: a.Store, Owner: a.Owner, Lease: lease}
			w := &execution.LiveWorker{Store: a.Store, Owner: a.Owner, Lease: lease, Runtime: providers, Access: access, Raptor: preflightRaptor{workerQualificationInput(t, id, seed.Skills)}, Bootstrap: seed.Checkpoint, QualificationEnabled: true}
			reads := 0
			a.Observations, _ = NewObservationProxy("https://api.rdev.raptor-iap.top"+observationRoute, func(_ context.Context, got execution.LiveStart, _ ObservationMaterial, _ string, _ json.RawMessage) (json.RawMessage, error) {
				reads++
				if model.calls != 0 || len(f.calls) != 0 || len(a.axRuns) != 0 {
					t.Fatal("worker preflight after model/runtime")
				}
				switch mode {
				case "failure":
					return nil, errors.New("private-preflight-source")
				case "cancel-signal":
					if e := a.Store.DeliverSignal(context.Background(), execution.Signal{ID: execution.NewID(), RequestID: id, Kind: "cancel", Payload: json.RawMessage(`{}`)}); e != nil {
						t.Fatal(e)
					}
				case "context-cancel":
					cancel()
				case "lease-loss":
					lease.valid = false
				}
				return []byte(`{"nodes":[]}`), nil
			})
			e := w.RunNext(ctx)
			if mode == "context-cancel" || mode == "lease-loss" {
				if e == nil {
					t.Fatal("lost execution authority unexpectedly completed")
				}
				// The existing recovery owner completes cleanup after the lost context/lease.
				lease.valid = true
				w.Owner = "recovery-owner"
				a.Owner = w.Owner
				providers.Owner = w.Owner
				if e = w.Reconcile(context.Background()); e != nil {
					t.Fatal("recovery cleanup", e)
				}
			} else if e != nil {
				t.Fatal(e)
			}
			got, e := a.Store.Get(context.Background(), id)
			if e != nil {
				t.Fatal(e)
			}
			if reads != 1 || access.issued != 1 || access.revoked != 1 || model.calls != 0 || len(f.calls) != 0 || len(a.axRuns) != 0 || len(a.Observations.entries) != 0 || got.RecoveryNeeded || (got.Status != "failed" && got.Status != "cancelled") {
				t.Fatalf("early failure not cleaned: reads=%d issued=%d revoked=%d model=%d bridge=%d entries=%d status=%s recovery=%t", reads, access.issued, access.revoked, model.calls, len(f.calls), len(a.Observations.entries), got.Status, got.RecoveryNeeded)
			}
			if mode == "cancel-signal" && got.Status != "cancelled" {
				t.Fatal("accepted cancellation lost")
			}
			var checkpointStatus string
			if e = a.Store.Pool.QueryRow(context.Background(), "SELECT checkpoint_status FROM gateway.executions WHERE request_id=$1", id).Scan(&checkpointStatus); e != nil || checkpointStatus == "verified" {
				t.Fatal("fabricated verified checkpoint", checkpointStatus, e)
			}
			active, owner, e := a.Store.ActiveOwner(context.Background())
			if e != nil || active != "" || owner != "" {
				t.Fatal("runtime slot retained", active, owner, e)
			}
		})
	}
}
