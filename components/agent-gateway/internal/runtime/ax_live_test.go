package runtime

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/jamesDeng/raptor-iap/components/agent-gateway/internal/execution"
	"strings"
	"testing"
	"time"
)

type fixtureBridge struct {
	calls             []AXRequest
	files             map[string][]byte
	failEnsure        bool
	inferenceTerminal bool
	exitCode          int
}

func (f *fixtureBridge) Call(_ context.Context, r AXRequest) (AXResponse, error) {
	f.calls = append(f.calls, r)
	switch r.Action {
	case "ensure":
		if f.failEnsure {
			return AXResponse{}, ErrRuntimeUnavailable
		}
		return AXResponse{Name: "raptor-" + r.Binding.AttemptID}, nil
	case "start":
		return AXResponse{ProcessID: "proc-" + r.Phase}, nil
	case "poll":
		return AXResponse{Terminal: r.Phase != "inference" || f.inferenceTerminal, ExitCode: f.exitCode}, nil
	case "write":
		f.files[r.Path] = r.Data
	case "read":
		v, ok := f.files[r.Path]
		if !ok {
			return AXResponse{}, ErrFileNotFound
		}
		return AXResponse{Data: v}, nil
	case "remove":
		return AXResponse{Absent: true}, nil
	}
	return AXResponse{}, nil
}
func axFixture(t *testing.T) (*AXLive, *fixtureBridge, execution.LiveStart) {
	n, r := nativeJournal(t)
	n.Config = LiveConfig{InfraUsername: "infra", InfraPassword: "private-password", AccountID: "1360282071200743", Region: "ap-southeast-1", TeamID: "team", TemplateID: "template", Bucket: "bucket", BucketPrefix: "auth", VolumeName: "auth", VolumeID: "volume", ExecutionRoleARN: "acs:ram::1360282071200743:role/runtime", HarnessDir: "/opt/harness", RaptorMcpURL: "https://raptor.example/mcp", InfraMcpURL: "https://infra.example/mcp"}
	sum := sha256.Sum256([]byte("archive"))
	sha := hex.EncodeToString(sum[:])
	n.Verifier = CheckpointVerifier{Objects: fixtureObjects{archive: "archive", checksum: sha, encryption: "AES256", size: 7}, Prefix: "auth"}
	b := r.Binding
	in := execution.LiveStart{Binding: b, Checkpoint: execution.VerifiedCheckpoint{ArchiveKey: "auth/checkpoint.tgz", ChecksumKey: "auth/checkpoint.sha256", SHA256: sha, Bytes: 7, PiVersion: "0.99.2"}, Deadline: time.Now().Add(time.Minute), Access: execution.AgentAccess{Binding: b, Credential: strings.Repeat("c", 40), RaptorMcpURL: n.Config.RaptorMcpURL, InfraMcpURL: n.Config.InfraMcpURL}}
	f := &fixtureBridge{files: map[string][]byte{}}
	return &AXLive{NativeLive: *n, Bridge: f}, f, in
}
func TestAXStagesRelativeCheckpointAndPrivateAccess(t *testing.T) {
	a, f, in := axFixture(t)
	h, e := a.Start(context.Background(), in)
	if e != nil {
		t.Fatal(e)
	}
	if h.ID != "raptor-"+in.Binding.AttemptID {
		t.Fatal("wrong task")
	}
	if string(f.files["/tmp/raptor-oss/checkpoint.tgz"]) != "archive" {
		t.Fatal("OSS prefix duplicated in mount path")
	}
	if _, ok := f.files["/tmp/raptor-oss/auth/checkpoint.tgz"]; ok {
		t.Fatal("prefix duplicated")
	}
	var job map[string]any
	if json.Unmarshal(f.files["/tmp/raptor-private/attempt-job.json"], &job) != nil || job["state_root"] != "/workspace/raptor-state" || job["mount_root"] != "/tmp/raptor-oss" {
		t.Fatal("durable/private paths wrong")
	}
	for _, call := range f.calls {
		if strings.HasPrefix(call.Path, "/workspace") {
			t.Fatal("private MCP access staged durably")
		}
	}
	if _, e = a.Stop(context.Background(), h); e != nil {
		t.Fatal(e)
	}
}
func TestAXCreateFailureDoesNotReplay(t *testing.T) {
	a, f, in := axFixture(t)
	f.failEnsure = true
	if _, e := a.Start(context.Background(), in); e == nil {
		t.Fatal("ambiguous create accepted")
	}
	if _, e := a.Start(context.Background(), in); e == nil {
		t.Fatal("create replayed")
	}
	count := 0
	for _, c := range f.calls {
		if c.Action == "ensure" {
			count++
		}
	}
	if count != 1 {
		t.Fatal("duplicate actor create")
	}
}
func TestAXRecoveryNeverStartsOrResumesInference(t *testing.T) {
	a, f, in := axFixture(t)
	if e := a.intent(context.Background(), in.Binding, "sandbox", "raptor-"+in.Binding.AttemptID); e != nil {
		t.Fatal(e)
	}
	if e := a.intent(context.Background(), in.Binding, "command", in.Binding.AttemptID); e != nil {
		t.Fatal(e)
	}
	r, e := a.Store.RecoveryRecord(context.Background(), in.Binding.AttemptID)
	if e != nil {
		t.Fatal(e)
	}
	out, e := a.Reconcile(context.Background(), r)
	if e != nil || !out.Cleanup.SandboxAbsent {
		t.Fatal(out, e)
	}
	for _, c := range f.calls {
		if c.Action == "start" || c.Action == "ensure" {
			t.Fatal("recovery replayed execution")
		}
	}
}

type publishedObjects struct {
	fixtureObjects
	puts int
}

func (p *publishedObjects) Put(context.Context, string, []byte) error { p.puts++; return nil }
func TestAXInvalidExportNeverPublishes(t *testing.T) {
	a, f, in := axFixture(t)
	objects := &publishedObjects{fixtureObjects: a.Verifier.Objects.(fixtureObjects)}
	a.Verifier.Objects = objects
	var terminal terminalFile
	key := "auth/lifecycle/" + in.Binding.AttemptID
	terminal.Checkpoint.ArchiveKey = key + ".tgz"
	terminal.Checkpoint.ChecksumKey = key + ".sha256"
	terminal.Checkpoint.SHA256 = objects.checksum
	terminal.Checkpoint.Bytes = 7
	terminal.Checkpoint.PiVersion = "0.99.2"
	f.files["/tmp/raptor-oss/lifecycle/"+in.Binding.AttemptID+".tgz"] = []byte("archive")
	f.files["/tmp/raptor-oss/lifecycle/"+in.Binding.AttemptID+".sha256"] = []byte(strings.Repeat("0", 64))
	if _, e := a.publish(context.Background(), in, &terminal); e == nil || objects.puts != 0 {
		t.Fatal("invalid guest export reached OSS")
	}
}
func TestAXNewRequestRestoresAuthOnly(t *testing.T) {
	a, f, in := axFixture(t)
	if _, e := a.Start(context.Background(), in); e != nil {
		t.Fatal(e)
	}
	var restore map[string]any
	json.Unmarshal(f.files["/tmp/raptor-private/restore-job.json"], &restore)
	if restore["bootstrap_only"] != true {
		t.Fatal("new AX request would inherit bootstrap history")
	}
}
func TestAXPauseRequiresCleanExitAndIsNotTerminalCompletion(t *testing.T) {
	for _, exit := range []int{0, 1} {
		t.Run(string(rune('0'+exit)), func(t *testing.T) {
			a, f, in := axFixture(t)
			b := in.Binding
			b.Operation = "db-proxy.replace-nodes"
			b.ObjectKind = "db-proxy"
			b.ClusterID = "cluster"
			b.DefinitionSHA256 = strings.Repeat("a", 64)
			in.Binding = b
			in.Access.Binding = b
			raw, _ := json.Marshal(b)
			a.Store.Pool.Exec(context.Background(), "UPDATE gateway.attempts SET binding=$2,definition_sha256=$3 WHERE id=$1", b.AttemptID, raw, b.DefinitionSHA256)
			a.axRuns = map[string]*axRun{b.RequestID: {start: in}}
			f.inferenceTerminal = true
			f.exitCode = exit
			payload := map[string]any{"phase": "live-inference", "passed": false, "paused": map[string]any{"kind": "approval", "approvalId": "33333333-3333-4333-8333-333333333333", "actionId": "scale-three", "bindingDigest": strings.Repeat("b", 64), "requestId": b.RequestID, "definitionSha256": b.DefinitionSHA256, "startedAt": time.Now().UTC()}, "sessionReference": map[string]any{"version": 1, "requestId": b.RequestID, "definitionSha256": b.DefinitionSHA256, "skillsCommit": b.SkillsCommit, "model": b.Model, "sessionId": "session", "file": "session.jsonl", "sha256": strings.Repeat("d", 64)}}
			f.files["/tmp/raptor-private/attempt-result.json"], _ = json.Marshal(payload)
			out, e := a.Poll(context.Background(), execution.RuntimeHandle{RequestID: b.RequestID, ID: "raptor-" + b.AttemptID}, 0)
			if exit != 0 {
				if e == nil {
					t.Fatal("unconfirmed paused exit accepted")
				}
				return
			}
			if e != nil || out.Terminal || out.Paused == nil || out.Result != nil {
				t.Fatal("AX pause misclassified", out, e)
			}
		})
	}
}
