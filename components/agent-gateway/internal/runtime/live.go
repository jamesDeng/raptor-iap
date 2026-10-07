package runtime

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"github.com/jamesDeng/raptor-iap/components/agent-gateway/internal/execution"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// NativeLive is deliberately separate from the legacy simulated Runtime.
// A single leased worker calls it serially; no secret is recoverable from the journal.
type NativeLive struct {
	Config     LiveConfig
	Management Management
	Verifier   CheckpointVerifier
	Store      *execution.Store
	Owner      string
	Lease      execution.WorkerLease
	runs       map[string]*nativeRun
}
type nativeRun struct {
	cancel    context.CancelFunc
	start     execution.LiveStart
	transport *SandboxTransport
	sandbox   SandboxRef
	key       ControlKey
	done      chan error
	terminal  *terminalFile
}
type terminalFile struct {
	Phase           string                `json:"phase"`
	Passed          bool                  `json:"passed"`
	Error           string                `json:"error"`
	CheckpointError string                `json:"checkpointError"`
	Result          *execution.LiveResult `json:"result"`
	Checkpoint      struct {
		ArchiveKey  string `json:"archive_key"`
		ChecksumKey string `json:"checksum_key"`
		SHA256      string `json:"sha256"`
		Bytes       int64  `json:"bytes"`
		PiVersion   string `json:"pi_version"`
	} `json:"checkpoint"`
}

func decodeTerminal(raw []byte, b execution.AttemptBinding, secrets []string) (terminalFile, error) {
	var out terminalFile
	if len(raw) > 65536 {
		return out, ErrRuntimeUnavailable
	}
	for _, secret := range secrets {
		if secret != "" && bytes.Contains(raw, []byte(secret)) {
			return out, ErrRuntimeUnavailable
		}
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(&out) != nil {
		return out, ErrRuntimeUnavailable
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		return out, ErrRuntimeUnavailable
	}
	if out.Passed && (out.Result == nil || execution.ValidateLiveResult(*out.Result, b, secrets...) != nil) {
		return out, ErrRuntimeUnavailable
	}
	allowed := map[string]bool{"NeedsSignIn": true, "Cancelled": true, "Timeout": true, "TurnLimit": true, "McpUnavailable": true, "ToolFailed": true, "MissingEvidence": true, "InvalidEvidence": true, "InvalidResult": true, "UnexpectedToolCatalog": true, "InvalidProgress": true, "ModelFailed": true, "RunnerFailed": true, "CheckpointFailed": true}
	if !out.Passed && !allowed[out.Error] {
		return out, ErrRuntimeUnavailable
	}
	if out.Result != nil && execution.ValidateLiveResult(*out.Result, b, secrets...) != nil {
		return out, ErrRuntimeUnavailable
	}
	return out, nil
}
func decodeProgress(raw []byte, cursor int64) ([]execution.RuntimeEvent, error) {
	if len(raw) > 65536 {
		return nil, ErrRuntimeUnavailable
	}
	rows := bytes.Split(raw, []byte("\n"))
	out := []execution.RuntimeEvent{}
	var last int64
	for _, row := range rows[:len(rows)-1] {
		var e execution.RuntimeEvent
		d := json.NewDecoder(bytes.NewReader(row))
		d.DisallowUnknownFields()
		if d.Decode(&e) != nil || e.RuntimeSequence != last+1 {
			return nil, ErrRuntimeUnavailable
		}
		var extra any
		if d.Decode(&extra) != io.EOF {
			return nil, ErrRuntimeUnavailable
		}
		last = e.RuntimeSequence
		if last > cursor {
			out = append(out, e)
		}
	}
	if last < cursor {
		return nil, ErrRuntimeUnavailable
	}
	return out, nil
}
func (n *NativeLive) fence(ctx context.Context, attempt string) error {
	if n.Lease == nil || !n.Lease.Valid(ctx) {
		return execution.ErrUnavailable
	}
	// The transaction provides the durable fence immediately before every side effect.
	record, e := n.Store.RecoveryRecord(ctx, attempt)
	if e != nil {
		return e
	}
	request, owner, e := n.Store.ActiveOwner(ctx)
	if e != nil || owner != n.Owner || request != record.Binding.RequestID || record.Binding.AttemptID != attempt {
		return execution.ErrUnavailable
	}
	return nil
}
func (n *NativeLive) intent(ctx context.Context, b execution.AttemptBinding, kind, name string) error {
	if e := n.fence(ctx, b.AttemptID); e != nil {
		return e
	}
	return n.Store.RecordIntent(ctx, b.AttemptID, n.Owner, execution.RuntimeIntent{Kind: kind, Name: name})
}
func (n *NativeLive) resource(ctx context.Context, b execution.AttemptBinding, kind, id string) error {
	return n.Store.RecordResource(ctx, b.AttemptID, n.Owner, execution.RuntimeResource{Kind: kind, ID: id})
}
func (n *NativeLive) Start(ctx context.Context, in execution.LiveStart) (execution.RuntimeHandle, error) {
	h := execution.RuntimeHandle{RequestID: in.Binding.RequestID}
	b := in.Binding
	if n.Config.Validate() != nil || in.Access.Binding != b || in.Access.RaptorMcpURL != n.Config.RaptorMcpURL || in.Access.InfraMcpURL != n.Config.InfraMcpURL || len(in.Access.Credential) < 32 || !in.Deadline.After(time.Now()) {
		return h, ErrConfiguration
	}
	if _, e := n.Verifier.VerifyCheckpoint(ctx, in.Checkpoint); e != nil {
		return h, e
	}
	if n.runs == nil {
		n.runs = map[string]*nativeRun{}
	}
	if n.runs[b.RequestID] != nil {
		return h, ErrConfiguration
	}
	attemptCtx, cancelAttempt := context.WithDeadline(ctx, in.Deadline)
	ctx = attemptCtx
	r := &nativeRun{start: in, cancel: cancelAttempt}
	n.runs[b.RequestID] = r
	if e := n.intent(ctx, b, "key", "raptor-"+b.AttemptID); e != nil {
		return h, e
	}
	key, e := n.managementFor(b).CreateKey(ctx, "raptor-"+b.AttemptID, in.Deadline)
	if e != nil {
		return h, e
	}
	r.key = key
	if e = n.resource(ctx, b, "key", key.ID); e != nil {
		return h, e
	}
	r.transport, e = NewSandboxTransport("https://api.ap-southeast-1.e2b.fc.aliyuncs.com", "ap-southeast-1.e2b.fc.aliyuncs.com", key.value, nil)
	if e != nil {
		return h, e
	}
	r.transport.before = func(call context.Context) error { return n.fence(call, b.AttemptID) }
	if e = n.intent(ctx, b, "sandbox", b.AttemptID); e != nil {
		return h, e
	}
	ttl := int(time.Until(in.Deadline).Seconds())
	if ttl < 1 || ttl > 900 {
		return h, ErrConfiguration
	}
	r.sandbox, e = r.transport.Create(ctx, CreateSpec{Template: n.Config.TemplateID, AttemptID: b.AttemptID, VolumeName: n.Config.VolumeName, ExecutionRole: n.Config.ExecutionRoleARN, TTL: ttl})
	if e != nil {
		return h, e
	}
	h.ID = r.sandbox.ID
	if e = n.resource(ctx, b, "sandbox", h.ID); e != nil {
		return h, e
	}
	if e = n.prepare(ctx, r); e != nil {
		return h, e
	}
	cancel, e := n.Store.LiveCancelled(ctx, b.RequestID)
	if e != nil || cancel {
		return h, execution.ErrInvalid
	}
	if e = n.intent(ctx, b, "command", b.AttemptID); e != nil {
		return h, e
	}
	command, e := r.transport.StartCommand(ctx, r.sandbox, CommandSpec{Executable: "/bin/sh", Args: []string{"-c", "umask 077; exec node /tmp/raptor-harness/runner.mjs /tmp/raptor-private/attempt-job.json /tmp/raptor-private/attempt-result.json"}, Tag: b.AttemptID, Deadline: time.Minute * 3})
	if e != nil {
		return h, e
	}
	if e = n.resource(ctx, b, "command", strconv.FormatUint(uint64(command.PID), 10)); e != nil {
		command.Close()
		return h, e
	}
	r.done = make(chan error, 1)
	go func() { r.done <- command.Wait(); close(r.done) }()
	return h, nil
}

const prepareCommand = "umask 077; test \"$(node --version)\" = v22.23.3 && python3 --version >/dev/null && test ! -e /tmp/raptor-state && mkdir -p /tmp/raptor-harness /tmp/raptor-private && chmod 700 /tmp/raptor-harness /tmp/raptor-private"

var harnessFiles = []string{"package.json", "package-lock.json", "archive.py", "application-context.mjs", "app-question.mjs", "checkpoint.mjs", "pi-adapter.mjs", "runner.mjs", "progress.mjs", "live-contract.mjs", "live-runner.mjs", "live-question.mjs", "mcp-runtime.mjs"}

func (n *NativeLive) prepare(ctx context.Context, r *nativeRun) error {
	b := r.start.Binding
	// Fixed preparation runs under a separately journaled command generation.
	if e := n.intent(ctx, b, "prepare", b.AttemptID); e != nil {
		return e
	}
	cmd, e := r.transport.StartCommand(ctx, r.sandbox, CommandSpec{Executable: "/bin/sh", Args: []string{"-c", prepareCommand}, Tag: "prepare-" + b.AttemptID, Deadline: 30 * time.Second})
	if e != nil {
		return e
	}
	if e = n.resource(ctx, b, "prepare", strconv.FormatUint(uint64(cmd.PID), 10)); e != nil {
		cmd.Close()
		return e
	}
	if e = cmd.Wait(); e != nil {
		return e
	}
	for _, file := range harnessFiles {
		p := filepath.Join(n.Config.HarnessDir, file)
		info, e := os.Lstat(p)
		if e != nil || !info.Mode().IsRegular() || info.Size() > 2*1024*1024 {
			return ErrConfiguration
		}
		raw, e := os.ReadFile(p)
		if e != nil {
			return ErrConfiguration
		}
		if e = n.fence(ctx, b.AttemptID); e != nil {
			return e
		}
		if e = r.transport.WriteFile(ctx, r.sandbox, "/tmp/raptor-harness/"+file, raw); e != nil {
			return e
		}
	}
	cp := r.start.Checkpoint
	reference := map[string]any{"archive_key": cp.ArchiveKey, "checksum_key": cp.ChecksumKey, "sha256": cp.SHA256, "bytes": cp.Bytes, "pi_version": cp.PiVersion}
	restore, _ := json.Marshal(map[string]any{"phase": "restore", "reference": reference, "state_root": "/tmp/raptor-state", "mount_root": "/mnt/oss", "prefix": n.Config.BucketPrefix})
	if e = r.transport.WriteFile(ctx, r.sandbox, "/tmp/raptor-restore.json", restore); e != nil {
		return e
	}
	if e = n.intent(ctx, b, "restore", b.AttemptID); e != nil {
		return e
	}
	cmd, e = r.transport.StartCommand(ctx, r.sandbox, CommandSpec{Executable: "/bin/sh", Args: []string{"-c", "umask 077; cd /tmp/raptor-harness && npm ci --ignore-scripts --no-audit --no-fund >/dev/null 2>&1 && node runner.mjs /tmp/raptor-restore.json /tmp/raptor-restored.json"}, Tag: "restore-" + b.AttemptID, Deadline: time.Minute * 3})
	if e != nil {
		return e
	}
	if e = n.resource(ctx, b, "restore", strconv.FormatUint(uint64(cmd.PID), 10)); e != nil {
		cmd.Close()
		return e
	}
	if e = cmd.Wait(); e != nil {
		return e
	}
	job, e := liveJob(n.Config, r.start)
	if e != nil {
		return e
	}
	if e = n.fence(ctx, b.AttemptID); e != nil {
		return e
	}
	return r.transport.WriteFile(ctx, r.sandbox, "/tmp/raptor-private/attempt-job.json", job)
}
func (n *NativeLive) Poll(ctx context.Context, h execution.RuntimeHandle, cursor int64) (execution.LiveObservation, error) {
	out := execution.LiveObservation{}
	r := n.runs[h.RequestID]
	if r == nil || r.sandbox.ID != h.ID {
		return out, ErrRuntimeUnavailable
	}
	if e := n.fence(ctx, r.start.Binding.AttemptID); e != nil {
		return out, e
	}
	raw, e := r.transport.ReadFile(ctx, r.sandbox, "/tmp/raptor-private/attempt-progress.jsonl", 65536)
	if e != nil && e != ErrFileNotFound {
		return out, e
	}
	if e == nil {
		out.Events, e = decodeProgress(raw, cursor)
		if e != nil {
			return out, e
		}
	}
	// Wait must finish the Connect stream before a result can authorize checkpointing.
	select {
	case waitErr := <-r.done:
		raw, e = r.transport.ReadFile(ctx, r.sandbox, "/tmp/raptor-private/attempt-result.json", 65536)
		if e != nil {
			return out, e
		}
		terminal, e := decodeTerminal(raw, r.start.Binding, append(n.Config.RedactionValues(), r.key.value, r.start.Access.Credential))
		if e != nil {
			return out, e
		}
		// Runner exit 1 is expected for explicit failure; a passed result requires clean exit.
		if waitErr != nil && terminal.Passed {
			return out, ErrCommandUnconfirmed
		}
		r.terminal = &terminal
		out.Terminal = true
		out.Result = terminal.Result
		out.FailureCode = terminal.Error
		return out, nil
	default:
		return out, nil
	}
}
func (n *NativeLive) Checkpoint(ctx context.Context, h execution.RuntimeHandle) (execution.VerifiedCheckpoint, error) {
	r := n.runs[h.RequestID]
	if r == nil || r.terminal == nil {
		return execution.VerifiedCheckpoint{}, ErrCheckpointVerification
	}
	if e := n.intent(ctx, r.start.Binding, "checkpoint", r.start.Binding.AttemptID); e != nil {
		return execution.VerifiedCheckpoint{}, e
	}
	c := r.terminal.Checkpoint
	return n.Verifier.VerifyCheckpoint(ctx, execution.VerifiedCheckpoint{ArchiveKey: c.ArchiveKey, ChecksumKey: c.ChecksumKey, SHA256: c.SHA256, Bytes: c.Bytes, PiVersion: c.PiVersion})
}
func (n *NativeLive) Cancel(ctx context.Context, h execution.RuntimeHandle) error {
	r := n.runs[h.RequestID]
	if r == nil || r.transport == nil || r.sandbox.ID == "" {
		return ErrRuntimeUnavailable
	}
	if e := n.fence(ctx, r.start.Binding.AttemptID); e != nil {
		return e
	}
	return r.transport.WriteFile(ctx, r.sandbox, "/tmp/raptor-private/attempt-cancel", []byte("cancel"))
}
func (n *NativeLive) Stop(ctx context.Context, h execution.RuntimeHandle) (execution.LiveCleanup, error) {
	r := n.runs[h.RequestID]
	if r == nil {
		id, owner, e := n.Store.ActiveOwner(ctx)
		if e != nil || owner != n.Owner || id != h.RequestID {
			return execution.LiveCleanup{}, ErrTerminationUnconfirmed
		}
		x, e := n.Store.Get(ctx, id)
		if e != nil {
			return execution.LiveCleanup{}, e
		}
		record, e := n.Store.RecoveryRecord(ctx, x.AttemptID)
		if e != nil {
			return execution.LiveCleanup{}, e
		}
		return n.cleanup(ctx, record, nil)
	}
	record, e := n.Store.RecoveryRecord(ctx, r.start.Binding.AttemptID)
	if e != nil {
		return execution.LiveCleanup{}, e
	}
	// The same recovery path handles ambiguous create responses without replacement.
	out, err := n.cleanup(ctx, record, r.transport)
	if out.SandboxAbsent && out.KeyAbsent {
		if r.cancel != nil {
			r.cancel()
		}
		r.key = ControlKey{}
		r.start.Access = execution.AgentAccess{}
		delete(n.runs, h.RequestID)
	}
	return out, err
}
func (n *NativeLive) cleanup(ctx context.Context, record execution.RecoveryRecord, t *SandboxTransport) (execution.LiveCleanup, error) {
	out := execution.LiveCleanup{}
	b := record.Binding
	sandboxIntent, keyIntent := false, false
	for _, i := range record.Intents {
		sandboxIntent = sandboxIntent || i.Kind == "sandbox"
		keyIntent = keyIntent || i.Kind == "key" || strings.HasPrefix(i.Kind, "reconcile_key:")
	}
	if !sandboxIntent {
		out.SandboxAbsent = true
	} else {
		if t == nil {
			return out, ErrTerminationUnconfirmed
		}
		if e := n.fence(ctx, b.AttemptID); e != nil {
			return out, e
		}
		t.before = func(call context.Context) error { return n.fence(call, b.AttemptID) }
		refs, e := t.InventorySandboxes(ctx, b.AttemptID)
		if e != nil {
			return out, e
		}

		// An empty inventory cannot resolve a create whose response was lost.
		for _, intent := range record.Intents {
			if intent.Kind == "sandbox" && intent.ResourceID == "" {
				if len(refs) != 1 {
					return out, ErrTerminationUnconfirmed
				}
				if e = n.resource(ctx, b, "sandbox", refs[0].ID); e != nil {
					return out, e
				}
			}
		}
		found := map[string]bool{}
		for _, ref := range refs {
			found[ref.ID] = true
		}
		for _, intent := range record.Intents {
			if intent.Kind == "sandbox" && intent.ResourceID != "" && !found[intent.ResourceID] {
				state, err := t.Inspect(ctx, SandboxRef{ID: intent.ResourceID})
				if err != nil || !state.Absent {
					return out, ErrTerminationUnconfirmed
				}
			}
		}
		if e = n.intent(ctx, b, "terminate", b.AttemptID); e != nil {
			return out, e
		}
		for _, ref := range refs {
			if e = n.fence(ctx, b.AttemptID); e != nil {
				return out, e
			}
			if e = t.Terminate(ctx, ref); e != nil {
				return out, e
			}
		}
		refs, e = t.InventorySandboxes(ctx, b.AttemptID)
		if e != nil || len(refs) != 0 {
			return out, ErrTerminationUnconfirmed
		}
		out.SandboxAbsent = true
	}
	if !keyIntent {
		out.KeyAbsent = true
		return out, nil
	}
	if e := n.intent(ctx, b, "revoke_key", b.AttemptID); e != nil {
		return out, e
	}
	keys, e := n.managementFor(b).InventoryKeys(ctx)
	if e != nil {
		return out, e
	}
	names := map[string]bool{}
	ids := map[string]bool{}
	for _, i := range record.Intents {
		if i.Kind == "key" || strings.HasPrefix(i.Kind, "reconcile_key:") {
			names[i.Name] = true
			if i.ResourceID != "" {
				ids[i.ResourceID] = true
			}
		}
	}
	for _, intent := range record.Intents {
		if (intent.Kind == "key" || strings.HasPrefix(intent.Kind, "reconcile_key:")) && intent.ResourceID == "" {
			matches := []KeyInfo{}
			for _, k := range keys {
				if k.Name == intent.Name {
					matches = append(matches, k)
				}
			}
			if len(matches) != 1 {
				return out, ErrKeyCleanupUnconfirmed
			}
			if e = n.resource(ctx, b, intent.Kind, matches[0].ID); e != nil {
				return out, e
			}
		}
	}
	for _, k := range keys {
		if names[k.Name] || ids[k.ID] {
			if e = n.fence(ctx, b.AttemptID); e != nil {
				return out, e
			}
			if e = n.managementFor(b).RevokeKey(ctx, k.ID); e != nil {
				return out, e
			}
		}
	}
	keys, e = n.managementFor(b).InventoryKeys(ctx)
	if e != nil {
		return out, e
	}
	for _, k := range keys {
		if names[k.Name] || ids[k.ID] {
			return out, ErrKeyCleanupUnconfirmed
		}
	}
	out.KeyAbsent = true
	return out, nil
}
func (n *NativeLive) Reconcile(ctx context.Context, record execution.RecoveryRecord) (execution.RecoveredRun, error) {
	out := execution.RecoveredRun{Result: record.Result, Checkpoint: record.Checkpoint}
	b := record.Binding
	sandboxIntent := false
	for _, i := range record.Intents {
		sandboxIntent = sandboxIntent || i.Kind == "sandbox"
	}
	var t *SandboxTransport
	if sandboxIntent {
		generation := execution.NewID()
		kind := "reconcile_key:" + generation
		name := "reconcile-" + generation
		if e := n.intent(ctx, b, kind, name); e != nil {
			return out, e
		}
		key, e := n.managementFor(b).CreateKey(ctx, name, time.Now().Add(120*time.Second))
		if e != nil {
			return out, e
		}
		if e = n.resource(ctx, b, kind, key.ID); e != nil {
			return out, e
		}
		t, e = NewSandboxTransport("https://api.ap-southeast-1.e2b.fc.aliyuncs.com", "ap-southeast-1.e2b.fc.aliyuncs.com", key.value, nil)
		if e != nil {
			return out, e
		}
		t.before = func(call context.Context) error { return n.fence(call, b.AttemptID) }
		refs, e := t.InventorySandboxes(ctx, b.AttemptID)
		if e != nil {
			return out, e
		}
		if len(refs) > 1 {
			return out, ErrRuntimeUnavailable
		}
		if len(refs) == 1 {
			ref, e := t.Connect(ctx, refs[0])
			if e == nil {
				raw, e := t.ReadFile(ctx, ref, "/tmp/raptor-private/attempt-result.json", 65536)
				if e == nil {
					terminal, e := decodeTerminal(raw, b, []string{key.value})
					if e == nil {
						out.Result = terminal.Result
						c := terminal.Checkpoint
						verified, err := n.Verifier.VerifyCheckpoint(ctx, execution.VerifiedCheckpoint{ArchiveKey: c.ArchiveKey, ChecksumKey: c.ChecksumKey, SHA256: c.SHA256, Bytes: c.Bytes, PiVersion: c.PiVersion})
						if err == nil {
							out.Checkpoint = verified
						}
					}
				}
			}
		}
	}
	// Termination precedes final completion; inference is never launched on recovery.
	record, e := n.Store.RecoveryRecord(ctx, b.AttemptID)
	if e != nil {
		return out, e
	}
	out.Cleanup, e = n.cleanup(ctx, record, t)
	if e == nil && out.Checkpoint.ArchiveKey != "" {
		out.Checkpoint, e = n.Verifier.VerifyCheckpoint(ctx, out.Checkpoint)
	}
	return out, e
}

func (n *NativeLive) managementFor(b execution.AttemptBinding) Management {
	m := n.Management
	m.before = func(ctx context.Context) error { return n.fence(ctx, b.AttemptID) }
	return m
}

func liveJob(config LiveConfig, in execution.LiveStart) ([]byte, error) {
	token := "Bearer " + in.Access.Credential
	return json.Marshal(map[string]any{"phase": "live-inference", "state_root": "/tmp/raptor-state", "private_root": "/tmp/raptor-private", "mount_root": "/mnt/oss", "prefix": config.BucketPrefix, "generation": in.Binding.AttemptID, "request": map[string]any{"binding": in.Binding, "question": in.Question}, "limits": map[string]any{"model_seconds": 90, "max_turns": 3, "max_output_tokens": 1024}, "mcp": map[string]any{"raptor": map[string]any{"url": config.RaptorMcpURL, "headers": map[string]string{"Authorization": token}}, "infra": map[string]any{"url": config.InfraMcpURL, "headers": map[string]string{"X-Infra-Authorization": "Basic " + base64.StdEncoding.EncodeToString([]byte(config.InfraUsername+":"+config.InfraPassword))}}}})
}
