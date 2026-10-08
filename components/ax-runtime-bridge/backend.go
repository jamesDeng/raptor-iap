package main

import (
	"context"
	env "github.com/agent-substrate/env/proto/ateenv/v1alpha"
	"github.com/google/ax/internal/substrate"
	ax "github.com/google/ax/pkg/apis/v1alpha1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/durationpb"
	"io"
	"path"
	"time"
)

type SDKBackend struct {
	AX                      ax.AXClient
	Substrate               *substrate.Client
	Image, Atespace, Router string
	EgressHosts             []string
}

func view(t *ax.Task) (*TaskView, error) {
	if t == nil || t.Metadata == nil || t.Spec == nil {
		return nil, ErrUnknown
	}
	seal := ""
	for _, e := range t.Spec.Env {
		if e.Name == "RAPTOR_BINDING_SHA256" {
			if seal != "" {
				return nil, ErrConflict
			}
			seal = e.Value
		}
	}
	return &TaskView{Name: t.Metadata.Name, Seal: seal, Image: t.Spec.Image, Phase: t.GetStatus().GetPhase()}, nil
}
func (b *SDKBackend) Get(ctx context.Context, n string) (*TaskView, error) {
	t, e := b.AX.GetTask(ctx, &ax.GetTaskRequest{Atespace: b.Atespace, Name: n})
	if status.Code(e) == codes.NotFound {
		return nil, ErrAbsent
	}
	if e != nil {
		return nil, ErrUnknown
	}
	return view(t)
}
func (b *SDKBackend) Create(ctx context.Context, n, seal string) (*TaskView, error) {
	t, e := b.AX.CreateTask(ctx, &ax.CreateTaskRequest{Task: &ax.Task{ApiVersion: "ax.dev/v1alpha1", Kind: "Task", Metadata: &ax.ObjectMeta{Name: n, Atespace: b.Atespace}, Spec: &ax.TaskSpec{Image: b.Image, Debug: true, Env: []*ax.EnvVar{{Name: "RAPTOR_BINDING_SHA256", Value: seal}}, Workspaces: []*ax.WorkspaceRef{{Name: "raptor-runtime", Path: "/workspace"}}}}})
	if e != nil {
		return nil, ErrUnknown
	}
	return view(t)
}
func (b *SDKBackend) Resume(ctx context.Context, n string) (*TaskView, error) {
	if e := b.Substrate.EnsureRuntimeEgress(ctx, b.Atespace, n, b.EgressHosts); e != nil {
		return nil, ErrUnknown
	}
	t, e := b.AX.ResumeTask(ctx, &ax.ResumeTaskRequest{Atespace: b.Atespace, Name: n})
	if e != nil {
		return nil, ErrUnknown
	}
	return view(t)
}
func (b *SDKBackend) Delete(ctx context.Context, n string) error {
	_, e := b.AX.DeleteTask(ctx, &ax.DeleteTaskRequest{Atespace: b.Atespace, Name: n})
	if e == nil || status.Code(e) == codes.NotFound {
		return nil
	}
	return ErrUnknown
}
func (b *SDKBackend) ActorAbsent(ctx context.Context, n string) (bool, error) {
	_, e := b.Substrate.GetActor(ctx, b.Atespace, n)
	if status.Code(e) == codes.NotFound {
		return true, nil
	}
	if e != nil {
		return false, ErrUnknown
	}
	return false, nil
}
func (b *SDKBackend) guest(ctx context.Context, n string) (context.Context, *grpc.ClientConn, env.ProcessServiceClient, error) {
	conn, e := grpc.NewClient(b.Router, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if e != nil {
		return nil, nil, nil, ErrUnknown
	}
	ctx = metadata.AppendToOutgoingContext(ctx, "ate-target-actor", b.Atespace+"/"+n)
	return ctx, conn, env.NewProcessServiceClient(conn), nil
}
func (b *SDKBackend) Run(ctx context.Context, n, phase string) (string, error) {
	ctx, c, p, e := b.guest(ctx, n)
	if e != nil {
		return "", e
	}
	defer c.Close()
	command := []string{"node", "/opt/raptor-harness/runner.mjs", "/tmp/raptor-private/restore-job.json", "/tmp/raptor-private/restore-result.json"}
	timeout := 2 * time.Minute
	switch phase {
	case "prepare":
		command = []string{"sh", "-c", "set -eu; umask 077; test ! -e /workspace/raptor-state; mkdir -p /tmp/raptor-private /tmp/raptor-oss; chmod 700 /tmp/raptor-private; test -f /opt/raptor-harness/runner.mjs"}
	case "restore":
	case "inference":
		command[2] = "/tmp/raptor-private/attempt-job.json"
		command[3] = "/tmp/raptor-private/attempt-result.json"
	default:
		return "", ErrInvalid
	}
	v, e := p.StartProcess(ctx, &env.StartProcessRequest{Command: command, Cwd: "/workspace", Timeout: durationpb.New(timeout)})
	if e != nil {
		return "", ErrUnknown
	}
	return v.GetProcessId(), nil
}
func (b *SDKBackend) Process(ctx context.Context, n, id string) (bool, int, error) {
	ctx, c, p, e := b.guest(ctx, n)
	if e != nil {
		return false, 0, e
	}
	defer c.Close()
	v, e := p.GetProcess(ctx, &env.GetProcessRequest{ProcessId: id})
	if e != nil {
		return false, 0, ErrUnknown
	}
	switch v.GetState() {
	case env.ProcessState_PROCESS_STATE_RUNNING:
		return false, 0, nil
	case env.ProcessState_PROCESS_STATE_EXITED:
		return true, int(v.GetExitCode()), nil
	default:
		return false, 0, ErrUnknown
	}
}
func collect(ctx context.Context, p env.ProcessServiceClient, id string, limit int64) ([]byte, int, error) {
	stream, e := p.StreamProcessOutput(ctx, &env.StreamProcessOutputRequest{ProcessId: id, Follow: true})
	if e != nil {
		return nil, 0, ErrUnknown
	}
	var data []byte
	for {
		v, e := stream.Recv()
		if e == io.EOF {
			return nil, 0, ErrUnknown
		}
		if e != nil {
			return nil, 0, ErrUnknown
		}
		if int64(len(data)+len(v.GetStdout())) > limit {
			return nil, 0, ErrInvalid
		}
		data = append(data, v.GetStdout()...)
		if exit := v.GetExit(); exit != nil {
			return data, int(exit.GetExitCode()), nil
		}
	}
}
func (b *SDKBackend) Read(ctx context.Context, n, file string, limit int64) ([]byte, error) {
	if !safePath(file) {
		return nil, ErrInvalid
	}
	ctx, c, p, e := b.guest(ctx, n)
	if e != nil {
		return nil, e
	}
	defer c.Close()
	v, e := p.StartProcess(ctx, &env.StartProcessRequest{Command: []string{"cat", "--", file}, Timeout: durationpb.New(30 * time.Second)})
	if e != nil {
		return nil, ErrUnknown
	}
	data, code, e := collect(ctx, p, v.GetProcessId(), limit)
	if e != nil {
		return nil, e
	}
	if code != 0 {
		return nil, ErrAbsent
	}
	return data, nil
}
func (b *SDKBackend) Write(ctx context.Context, n, file string, data []byte) error {
	if !safePath(file) {
		return ErrInvalid
	}
	ctx, c, p, e := b.guest(ctx, n)
	if e != nil {
		return e
	}
	defer c.Close()
	// Validated path has no shell metacharacters; bytes and secrets travel only via stdin.
	v, e := p.StartProcess(ctx, &env.StartProcessRequest{Command: []string{"sh", "-c", "set -eu; umask 077; mkdir -p " + path.Dir(file) + "; cat > " + file}, Stdin: true, Timeout: durationpb.New(30 * time.Second)})
	if e != nil {
		return ErrUnknown
	}
	stream, e := p.WriteProcessInput(ctx)
	if e != nil {
		return ErrUnknown
	}
	id := v.GetProcessId()
	for len(data) > 0 {
		size := 65536
		if len(data) < size {
			size = len(data)
		}
		if stream.Send(&env.WriteProcessInputRequest{ProcessId: id, Data: data[:size]}) != nil {
			return ErrUnknown
		}
		data = data[size:]
	}
	if stream.Send(&env.WriteProcessInputRequest{ProcessId: id, Close: true}) != nil {
		return ErrUnknown
	}
	if _, e = stream.CloseAndRecv(); e != nil {
		return ErrUnknown
	}
	_, code, e := collect(ctx, p, id, 1024)
	if e != nil || code != 0 {
		return ErrUnknown
	}
	return nil
}
