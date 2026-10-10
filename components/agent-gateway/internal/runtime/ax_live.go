package runtime

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/jamesDeng/raptor-iap/components/agent-gateway/internal/execution"
	"strings"
	"time"
)

type ObjectWriter interface {
	Put(context.Context, string, []byte) error
}
type AXLive struct {
	NativeLive
	Bridge AXCaller
	axRuns map[string]*axRun
}
type axRun struct {
	start                 execution.LiveStart
	terminal              *terminalFile
	modelLease            ModelLease
	modelRefreshProcessed bool
	modelRevoked          bool
}

func (a *AXLive) call(ctx context.Context, r AXRequest) (AXResponse, error) {
	if a.Bridge == nil {
		return AXResponse{}, ErrConfiguration
	}
	if e := a.fence(ctx, r.Binding.AttemptID); e != nil {
		return AXResponse{}, e
	}
	return a.Bridge.Call(ctx, r)
}
func (a *AXLive) request(in execution.LiveStart, action string) AXRequest {
	return AXRequest{Action: action, Binding: in.Binding, Deadline: in.Deadline}
}
func (a *AXLive) write(ctx context.Context, in execution.LiveStart, path string, data []byte) error {
	r := a.request(in, "write")
	r.Path = path
	r.Data = data
	_, e := a.call(ctx, r)
	return e
}
func (a *AXLive) read(ctx context.Context, in execution.LiveStart, path string, limit int64) ([]byte, error) {
	r := a.request(in, "read")
	r.Path = path
	r.Limit = limit
	v, e := a.call(ctx, r)
	return v.Data, e
}
func (a *AXLive) phase(ctx context.Context, in execution.LiveStart, phase, kind string, wait bool) error {
	if e := a.intent(ctx, in.Binding, kind, in.Binding.AttemptID); e != nil {
		return e
	}
	r := a.request(in, "start")
	r.Phase = phase
	v, e := a.call(ctx, r)
	if e != nil || v.ProcessID == "" {
		return ErrCommandUnconfirmed
	}
	if e = a.resource(ctx, in.Binding, kind, v.ProcessID); e != nil {
		return e
	}
	if !wait {
		return nil
	}
	r.Action = "poll"
	for {
		v, e = a.call(ctx, r)
		if e != nil {
			return e
		}
		if v.Terminal {
			if v.ExitCode != 0 {
				return ErrCommandUnconfirmed
			}
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(200 * time.Millisecond):
		}
	}
}
func (a *AXLive) Start(ctx context.Context, in execution.LiveStart) (execution.RuntimeHandle, error) {
	h := execution.RuntimeHandle{RequestID: in.Binding.RequestID}
	if a.Config.Validate() != nil || in.Access.Binding != in.Binding || in.Access.RaptorMcpURL != a.Config.RaptorMcpURL || in.Access.InfraMcpURL != a.Config.InfraMcpURL || len(in.Access.Credential) < 32 || !in.Deadline.After(time.Now()) {
		return h, ErrConfiguration
	}
	cp, e := a.Verifier.VerifyCheckpoint(ctx, in.Checkpoint)
	if e != nil {
		return h, e
	}
	var modelLease ModelLease
	if in.Binding.ProviderID != "" {
		if a.ModelCredentials == nil {
			return h, ErrConfiguration
		}
		if e = a.fence(ctx, in.Binding.AttemptID); e != nil {
			return h, e
		}
		modelLease, e = a.ModelCredentials.Lease(ctx, in.Binding)
		if e != nil {
			return h, e
		}
	}
	if a.axRuns == nil {
		a.axRuns = map[string]*axRun{}
	}
	if a.axRuns[h.RequestID] != nil {
		return h, ErrConfiguration
	}
	a.axRuns[h.RequestID] = &axRun{start: in, modelLease: modelLease}
	ctx, cancel := context.WithDeadline(ctx, in.Deadline)
	defer cancel()
	if e = a.intent(ctx, in.Binding, "sandbox", "raptor-"+in.Binding.AttemptID); e != nil {
		return h, e
	}
	out, e := a.call(ctx, a.request(in, "ensure"))
	if e != nil {
		return h, e
	}
	h.ID = out.Name
	if h.ID != "raptor-"+in.Binding.AttemptID {
		return h, ErrRuntimeUnavailable
	}
	if e = a.resource(ctx, in.Binding, "sandbox", h.ID); e != nil {
		return h, e
	}
	if e = a.phase(ctx, in, "prepare", "prepare", true); e != nil {
		return h, e
	}
	for key, size := range map[string]int64{cp.ArchiveKey: cp.Bytes, cp.ChecksumKey: 64} {
		reader, e := a.Verifier.Objects.Read(ctx, key)
		if e != nil {
			return h, ErrCheckpointVerification
		}
		raw, e := bounded(reader, size)
		reader.Close()
		if e != nil || int64(len(raw)) != size {
			return h, ErrCheckpointVerification
		}
		if e = a.write(ctx, in, "/tmp/raptor-oss/"+strings.TrimPrefix(key, a.Config.BucketPrefix+"/"), raw); e != nil {
			return h, e
		}
	}
	reference := map[string]any{"archive_key": cp.ArchiveKey, "checksum_key": cp.ChecksumKey, "sha256": cp.SHA256, "bytes": cp.Bytes, "pi_version": cp.PiVersion}
	raw, _ := json.Marshal(map[string]any{"phase": "restore", "reference": reference, "state_root": "/workspace/raptor-state", "mount_root": "/tmp/raptor-oss", "prefix": a.Config.BucketPrefix, "skip_auth_check": in.Binding.ProviderID == "codex"})
	if e = a.write(ctx, in, "/tmp/raptor-private/restore-job.json", raw); e != nil {
		return h, e
	}
	if e = a.phase(ctx, in, "restore", "restore", true); e != nil {
		return h, e
	}
	if in.Binding.ProviderID == "codex" {
		auth, marshalError := json.Marshal(map[string]json.RawMessage{"openai-codex": modelLease.Credential})
		if marshalError != nil {
			return h, ErrConfiguration
		}
		if e = a.write(ctx, in, "/tmp/raptor-private/model-credential.json", auth); e != nil {
			return h, e
		}
	}
	job, e := liveJob(a.Config, in)
	if e != nil {
		return h, e
	}
	var value map[string]any
	if json.Unmarshal(job, &value) != nil {
		return h, ErrConfiguration
	}
	value["state_root"] = "/workspace/raptor-state"
	value["mount_root"] = "/tmp/raptor-oss"
	job, _ = json.Marshal(value)
	if e = a.write(ctx, in, "/tmp/raptor-private/attempt-job.json", job); e != nil {
		return h, e
	}
	cancelled, e := a.Store.LiveCancelled(ctx, in.Binding.RequestID)
	if e != nil || cancelled {
		return h, execution.ErrInvalid
	}
	return h, a.phase(ctx, in, "inference", "command", false)
}
func (a *AXLive) Poll(ctx context.Context, h execution.RuntimeHandle, cursor int64) (execution.LiveObservation, error) {
	var out execution.LiveObservation
	r := a.axRuns[h.RequestID]
	if r == nil || h.ID != "raptor-"+r.start.Binding.AttemptID {
		return out, ErrRuntimeUnavailable
	}
	if r.start.Binding.ProviderID == "codex" && !r.modelRevoked && a.ModelCredentials.Valid(ctx, r.start.Binding) != nil {
		r.modelRevoked = true
		if e := a.Cancel(ctx, h); e != nil {
			return out, e
		}
	}
	if r.start.Binding.ProviderID == "codex" && !r.modelRefreshProcessed {
		update, readError := a.read(ctx, r.start, "/tmp/raptor-private/model-refresh.json", 65536)
		if readError != nil && !errors.Is(readError, ErrFileNotFound) {
			return out, readError
		}
		if readError == nil {
			r.modelRefreshProcessed = true
			var body struct {
				Credential json.RawMessage `json:"credential"`
			}
			ack := map[string]any{"status": "failed"}
			if json.Unmarshal(update, &body) == nil && len(body.Credential) > 0 && a.ModelCredentials != nil {
				generation, refreshError := a.ModelCredentials.Refresh(ctx, r.start.Binding, r.modelLease.Generation, body.Credential)
				if refreshError == nil {
					ack = map[string]any{"status": "published", "generation": generation}
				}
			}
			ackBytes, _ := json.Marshal(ack)
			if e := a.write(ctx, r.start, "/tmp/raptor-private/model-refresh-ack.json", ackBytes); e != nil {
				return out, e
			}
		}
	}
	raw, e := a.read(ctx, r.start, "/tmp/raptor-private/attempt-progress.jsonl", 65536)
	if e != nil && !errors.Is(e, ErrFileNotFound) {
		return out, e
	}
	if e == nil {
		out.Events, e = decodeProgress(raw, cursor, append(append(a.Config.RedactionValues(), r.start.Access.Credential), modelSecretValues(r.modelLease)...)...)
		if e != nil {
			return out, e
		}
	}
	out.Turns, e = observeConversation(ctx, out.Events, r.start.Binding, func(c context.Context, p string, l int64) ([]byte, error) { return a.read(c, r.start, p, l) }, append(append(a.Config.RedactionValues(), r.start.Access.Credential), modelSecretValues(r.modelLease)...))
	if e != nil {
		return out, e
	}
	req := a.request(r.start, "poll")
	req.Phase = "inference"
	v, e := a.call(ctx, req)
	if e != nil {
		return out, e
	}
	if !v.Terminal {
		return out, nil
	}
	raw, e = a.read(ctx, r.start, "/tmp/raptor-private/attempt-result.json", 65536)
	if e != nil {
		return out, e
	}
	terminal, e := decodeTerminal(raw, r.start.Binding, append(append(a.Config.RedactionValues(), r.start.Access.Credential), modelSecretValues(r.modelLease)...))
	if e != nil || terminal.Passed && v.ExitCode != 0 {
		return out, ErrCommandUnconfirmed
	}
	r.terminal = &terminal
	out.Terminal = true
	out.Result = terminal.Result
	out.FailureCode = terminal.Error
	return out, nil
}
func (a *AXLive) publish(ctx context.Context, in execution.LiveStart, t *terminalFile) (execution.VerifiedCheckpoint, error) {
	bad := execution.VerifiedCheckpoint{}
	if t == nil {
		return bad, ErrCheckpointVerification
	}
	c := t.Checkpoint
	expected := a.Config.BucketPrefix + "/lifecycle/" + in.Binding.AttemptID
	if c.ArchiveKey != expected+".tgz" || c.ChecksumKey != expected+".sha256" || c.Bytes < 1 || c.Bytes > 16*1024*1024 || c.PiVersion != "0.99.2" || len(c.SHA256) != 64 {
		return bad, ErrCheckpointVerification
	}
	writer, ok := a.Verifier.Objects.(ObjectWriter)
	if !ok {
		return bad, ErrCheckpointVerification
	}
	if e := a.intent(ctx, in.Binding, "checkpoint", in.Binding.AttemptID); e != nil {
		return bad, e
	}
	exports := map[string][]byte{}
	for key, size := range map[string]int64{c.ArchiveKey: c.Bytes, c.ChecksumKey: 64} {
		raw, e := a.read(ctx, in, "/tmp/raptor-oss/"+strings.TrimPrefix(key, a.Config.BucketPrefix+"/"), size)
		if e != nil || int64(len(raw)) != size {
			return bad, ErrCheckpointVerification
		}
		exports[key] = raw
	}
	hash := sha256.Sum256(exports[c.ArchiveKey])
	if hex.EncodeToString(hash[:]) != c.SHA256 || string(exports[c.ChecksumKey]) != c.SHA256 {
		return bad, ErrCheckpointVerification
	}
	for key, size := range map[string]int64{c.ArchiveKey: c.Bytes, c.ChecksumKey: 64} {
		raw := exports[key]
		if int64(len(raw)) != size {
			return bad, ErrCheckpointVerification
		}
		if e := a.fence(ctx, in.Binding.AttemptID); e != nil {
			return bad, e
		}
		if e := writer.Put(ctx, key, raw); e != nil {
			return bad, e
		}
	}
	return a.Verifier.VerifyCheckpoint(ctx, execution.VerifiedCheckpoint{ArchiveKey: c.ArchiveKey, ChecksumKey: c.ChecksumKey, SHA256: c.SHA256, Bytes: c.Bytes, PiVersion: c.PiVersion})
}
func (a *AXLive) Checkpoint(ctx context.Context, h execution.RuntimeHandle) (execution.VerifiedCheckpoint, error) {
	r := a.axRuns[h.RequestID]
	if r == nil {
		return execution.VerifiedCheckpoint{}, ErrCheckpointVerification
	}
	return a.publish(ctx, r.start, r.terminal)
}
func (a *AXLive) Cancel(ctx context.Context, h execution.RuntimeHandle) error {
	r := a.axRuns[h.RequestID]
	if r == nil {
		return ErrRuntimeUnavailable
	}
	return a.write(ctx, r.start, "/tmp/raptor-private/attempt-cancel", []byte("cancel"))
}
func (a *AXLive) cleanup(ctx context.Context, record execution.RecoveryRecord) (execution.LiveCleanup, error) {
	out := execution.LiveCleanup{}
	has := false
	for _, v := range record.Intents {
		has = has || v.Kind == "sandbox"
	}
	if !has {
		return execution.LiveCleanup{SandboxAbsent: true, KeyAbsent: true}, nil
	}
	if e := a.intent(ctx, record.Binding, "terminate", record.Binding.AttemptID); e != nil {
		return out, e
	}
	v, e := a.call(ctx, AXRequest{Action: "remove", Binding: record.Binding})
	if e != nil || !v.Absent {
		return out, ErrTerminationUnconfirmed
	}
	delete(a.axRuns, record.Binding.RequestID)
	return execution.LiveCleanup{SandboxAbsent: true, KeyAbsent: true}, nil
}
func (a *AXLive) Stop(ctx context.Context, h execution.RuntimeHandle) (execution.LiveCleanup, error) {
	x, e := a.Store.Get(ctx, h.RequestID)
	if e != nil {
		return execution.LiveCleanup{}, e
	}
	record, e := a.Store.RecoveryRecord(ctx, x.AttemptID)
	if e != nil {
		return execution.LiveCleanup{}, e
	}
	return a.cleanup(ctx, record)
}
func (a *AXLive) Reconcile(ctx context.Context, record execution.RecoveryRecord) (execution.RecoveredRun, error) {
	out := execution.RecoveredRun{Result: record.Result, Checkpoint: record.Checkpoint}
	in := execution.LiveStart{Binding: record.Binding, Deadline: record.Deadline}
	hasCommand := false
	for _, v := range record.Intents {
		hasCommand = hasCommand || v.Kind == "command"
	}
	if hasCommand {
		req := a.request(in, "poll")
		req.Phase = "inference"
		v, e := a.call(ctx, req)
		if e == nil && v.Terminal {
			raw, e := a.read(ctx, in, "/tmp/raptor-private/attempt-result.json", 65536)
			if e == nil {
				t, e := decodeTerminal(raw, in.Binding, a.Config.RedactionValues())
				if e == nil && (!t.Passed || v.ExitCode == 0) {
					out.Result = t.Result
					if cp, e := a.publish(ctx, in, &t); e == nil {
						out.Checkpoint = cp
					}
				}
			}
		}
	}
	var e error
	out.Cleanup, e = a.cleanup(ctx, record)
	if e == nil && out.Checkpoint.ArchiveKey != "" {
		out.Checkpoint, e = a.Verifier.VerifyCheckpoint(ctx, out.Checkpoint)
	}
	return out, e
}

var _ execution.LiveRuntime = (*AXLive)(nil)
