package runtime

import (
	"context"
	"github.com/jamesDeng/raptor-iap/components/agent-gateway/internal/execution"
	"io"
	"strconv"
	"time"
)

type CompatibilityReport struct {
	Passed        bool   `json:"passed"`
	ModelCalls    int    `json:"modelCalls"`
	SandboxAbsent bool   `json:"sandboxAbsent"`
	KeyAbsent     bool   `json:"keyAbsent"`
	Failure       string `json:"failure,omitempty"`
}

// Compatibility performs no inference and consumes only an explicitly claimed
// compatibility request. All mutating operations use the live attempt journal.
func (n *NativeLive) Compatibility(ctx context.Context, b execution.AttemptBinding, checkpoint execution.VerifiedCheckpoint, deadline time.Time) (report CompatibilityReport) {
	report.Failure = "CompatibilityFailed"
	r := &nativeRun{start: execution.LiveStart{Binding: b, Checkpoint: checkpoint, Deadline: deadline}}
	if n.runs == nil {
		n.runs = map[string]*nativeRun{}
	}
	n.runs[b.RequestID] = r
	defer func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
		defer cancel()
		cleanup, e := n.Stop(cleanupCtx, execution.RuntimeHandle{RequestID: b.RequestID, ID: r.sandbox.ID})
		report.SandboxAbsent = cleanup.SandboxAbsent
		report.KeyAbsent = cleanup.KeyAbsent
		if e != nil || !cleanup.KeyAbsent || !cleanup.SandboxAbsent {
			report.Passed = false
			report.Failure = "CleanupUnconfirmed"
		}
		if report.Passed {
			report.Failure = ""
		}
	}()
	if n.Config.Validate() != nil {
		return
	}
	if _, e := n.Verifier.VerifyCheckpoint(ctx, checkpoint); e != nil {
		return
	}
	if n.intent(ctx, b, "key", "raptor-"+b.AttemptID) != nil {
		return
	}
	key, e := n.Management.CreateKey(ctx, "raptor-"+b.AttemptID, deadline)
	if e != nil {
		return
	}
	r.key = key
	if n.resource(ctx, b, "key", key.ID) != nil {
		return
	}
	r.transport, e = NewSandboxTransport("https://api.ap-southeast-1.e2b.fc.aliyuncs.com", "ap-southeast-1.e2b.fc.aliyuncs.com", key.value, nil)
	if e != nil {
		return
	}
	if n.intent(ctx, b, "sandbox", b.AttemptID) != nil {
		return
	}
	ttl := int(time.Until(deadline).Seconds())
	r.sandbox, e = r.transport.Create(ctx, CreateSpec{Template: n.Config.TemplateID, AttemptID: b.AttemptID, VolumeName: n.Config.VolumeName, ExecutionRole: n.Config.ExecutionRoleARN, TTL: ttl})
	if e != nil {
		return
	}
	if n.resource(ctx, b, "sandbox", r.sandbox.ID) != nil {
		return
	}
	r.sandbox, e = r.transport.Connect(ctx, r.sandbox)
	if e != nil {
		return
	}
	if n.fence(ctx, b.AttemptID) != nil {
		return
	}
	if r.transport.WriteFile(ctx, r.sandbox, "/tmp/raptor-compat", []byte("raptor-compatibility")) != nil {
		return
	}
	raw, e := r.transport.ReadFile(ctx, r.sandbox, "/tmp/raptor-compat", 64)
	if e != nil || string(raw) != "raptor-compatibility" {
		return
	}
	// The probe uses an attempt UUID, never user/model text, in its fixed script.
	probe := "/mnt/oss/lifecycle/compat-" + b.AttemptID
	if n.intent(ctx, b, "command", b.AttemptID) != nil {
		return
	}
	cmd, e := r.transport.StartCommand(ctx, r.sandbox, CommandSpec{Executable: "/bin/sh", Args: []string{"-c", "umask 077; test \"$(node --version)\" = v22.23.3 && python3 --version >/dev/null && mkdir -p /mnt/oss/lifecycle && printf raptor-compatibility > " + probe}, Tag: b.AttemptID, Deadline: 30 * time.Second})
	if e != nil {
		return
	}
	if n.resource(ctx, b, "command", strconv.FormatUint(uint64(cmd.PID), 10)) != nil {
		cmd.Close()
		return
	}
	if cmd.Wait() != nil {
		return
	}
	objectKey := n.Config.BucketPrefix + "/lifecycle/compat-" + b.AttemptID
	algorithm, e := n.Verifier.Objects.BucketEncryption(ctx)
	if e != nil || algorithm != "AES256" {
		return
	}
	meta, e := n.Verifier.Objects.Head(ctx, objectKey)
	if e != nil || meta.Bytes != int64(len("raptor-compatibility")) || meta.Encryption != "AES256" {
		return
	}
	object, e := n.Verifier.Objects.Read(ctx, objectKey)
	if e != nil {
		return
	}
	data, e := io.ReadAll(io.LimitReader(object, 21))
	object.Close()
	if e != nil || string(data) != "raptor-compatibility" {
		return
	}
	if n.intent(ctx, b, "probe_cleanup", b.AttemptID) != nil {
		return
	}
	cmd, e = r.transport.StartCommand(ctx, r.sandbox, CommandSpec{Executable: "/bin/sh", Args: []string{"-c", "rm -- " + probe}, Tag: "cleanup-" + b.AttemptID, Deadline: 30 * time.Second})
	if e != nil {
		return
	}
	if n.resource(ctx, b, "probe_cleanup", strconv.FormatUint(uint64(cmd.PID), 10)) != nil {
		cmd.Close()
		return
	}
	if cmd.Wait() != nil {
		return
	}
	report.Passed = true
	return
}
