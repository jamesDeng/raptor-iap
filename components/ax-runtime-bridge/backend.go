package main

import (
	"context"
	"encoding/json"
	"fmt"
	env "github.com/agent-substrate/env/proto/ateenv/v1alpha"
	"github.com/google/ax/internal/substrate"
	ax "github.com/google/ax/pkg/apis/v1alpha1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/durationpb"
	"io"
	"log"
	"net/http"
	"path"
	"strings"
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
	workspace := ""
	if len(t.Spec.Workspaces) == 1 && t.Spec.Workspaces[0] != nil {
		workspace = t.Spec.Workspaces[0].Name
	}
	ready := false
	for _, condition := range t.GetStatus().GetConditions() {
		if condition.GetType() == "Ready" && condition.GetStatus() == "True" {
			ready = true
		}
	}
	return &TaskView{Name: t.Metadata.Name, Seal: seal, Image: t.Spec.Image, Phase: t.GetStatus().GetPhase(), Workspace: workspace, Ready: ready}, nil
}
func (b *SDKBackend) Get(ctx context.Context, n string) (*TaskView, error) {
	t, e := b.AX.GetTask(ctx, &ax.GetTaskRequest{Atespace: b.Atespace, Name: n})
	if status.Code(e) == codes.NotFound {
		return nil, ErrAbsent
	}
	if e != nil {
		log.Printf("AX backend GetTask failed code=%s", status.Code(e))
		return nil, ErrUnknown
	}
	v, err := view(t)
	if err != nil {
		return nil, err
	}
	// AX can report Running before its resumed guest completes workspace setup.
	// Its Ready condition may stay stale, so ask the guest's workspace probe.
	if !v.Ready && v.Phase == "Running" && b.guestWorkspaceReady(ctx, n) {
		v.Ready = true
	}
	return v, nil
}
func (b *SDKBackend) guestWorkspaceReady(ctx context.Context, n string) bool {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+b.Router+"/readyz?check=workspace", nil)
	if err != nil {
		return false
	}
	req.Header.Set("ate-target-actor", b.Atespace+"/"+n)
	req.Host = n + "." + b.Atespace + ".actors.resources.substrate.ate.dev"
	client := &http.Client{Transport: &http.Transport{Proxy: nil, DisableKeepAlives: true}, Timeout: 2 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}
func (b *SDKBackend) Create(ctx context.Context, n, seal string) (*TaskView, error) {
	t, e := b.AX.CreateTask(ctx, &ax.CreateTaskRequest{Task: &ax.Task{ApiVersion: "ax.dev/v1alpha1", Kind: "Task", Metadata: &ax.ObjectMeta{Name: n, Atespace: b.Atespace}, Spec: &ax.TaskSpec{Image: b.Image, Debug: true, Env: []*ax.EnvVar{{Name: "RAPTOR_BINDING_SHA256", Value: seal}}, Workspaces: []*ax.WorkspaceRef{{Name: "raptor-runtime", Path: "/workspace"}}}}})
	if e != nil {
		log.Printf("AX backend CreateTask failed code=%s", status.Code(e))
		return nil, ErrUnknown
	}
	return view(t)
}

type repositoryWorkspaceRow struct {
	Purpose    string `json:"purpose"`
	URL        string `json:"url"`
	Directory  string `json:"directory"`
	BaseBranch string `json:"baseBranch"`
	BaseCommit string `json:"baseCommit"`
	Path       string `json:"path"`
}

func repositoryWorkspace(r Request) (*ax.Workspace, error) {
	if r.Binding.Operation != "repository.change" || len(r.Repositories) == 0 || len(r.Repositories) > 32 || !uuid.MatchString(r.Binding.AttemptID) {
		return nil, ErrInvalid
	}
	name := "raptor-repositories-" + r.Binding.AttemptID
	ws := &ax.Workspace{ApiVersion: "ax.dev/v1alpha1", Kind: "Workspace", Metadata: &ax.ObjectMeta{Name: name, Atespace: r.Binding.ClusterID}, Spec: &ax.WorkspaceSpec{}}
	paths := map[string]string{}
	rows := make([]repositoryWorkspaceRow, 0, len(r.Repositories))
	for _, repo := range r.Repositories {
		if !validRepository(repo) {
			return nil, ErrInvalid
		}
		clonePath, ok := paths[repo.URL]
		if !ok {
			clonePath = fmt.Sprintf("repositories/repo-%d", len(paths))
			paths[repo.URL] = clonePath
			// AX starts workspace Git setup before it activates actor egress.
			// The bridge checks out public repositories in the prepare phase instead.
		}
		selected := clonePath
		if repo.Directory != "." {
			selected = path.Join(clonePath, repo.Directory)
		}
		rows = append(rows, repositoryWorkspaceRow{Purpose: repo.Purpose, URL: repo.URL, Directory: repo.Directory, BaseBranch: repo.BaseBranch, BaseCommit: repo.BaseCommit, Path: "/workspace/" + selected})
	}
	data, err := json.Marshal(rows)
	if err != nil {
		return nil, ErrInvalid
	}
	ws.Spec.Files = []*ax.File{{Path: ".raptor-repositories.json", Content: string(data)}}
	return ws, nil
}
func (b *SDKBackend) CreateWithRepositories(ctx context.Context, n, seal string, repos []Repository) (*TaskView, error) {
	if !strings.HasPrefix(n, "raptor-") || !uuid.MatchString(strings.TrimPrefix(n, "raptor-")) {
		return nil, ErrInvalid
	}
	r := Request{Binding: Binding{Operation: "repository.change", AttemptID: strings.TrimPrefix(n, "raptor-"), ClusterID: b.Atespace}, Repositories: repos}
	ws, err := repositoryWorkspace(r)
	if err != nil {
		return nil, err
	}
	current, err := b.AX.GetWorkspace(ctx, &ax.GetWorkspaceRequest{Atespace: b.Atespace, Name: ws.Metadata.Name})
	if status.Code(err) == codes.NotFound {
		_, err = b.AX.UpdateWorkspace(ctx, &ax.UpdateWorkspaceRequest{Workspace: ws})
		if err != nil {
			current, err = b.AX.GetWorkspace(ctx, &ax.GetWorkspaceRequest{Atespace: b.Atespace, Name: ws.Metadata.Name})
			if err != nil || current == nil || !proto.Equal(current.Spec, ws.Spec) {
				return nil, ErrUnknown
			}
		}
	} else if err != nil {
		return nil, ErrUnknown
	} else if current == nil || !proto.Equal(current.Spec, ws.Spec) {
		return nil, ErrConflict
	}
	t, err := b.AX.CreateTask(ctx, &ax.CreateTaskRequest{Task: &ax.Task{ApiVersion: "ax.dev/v1alpha1", Kind: "Task", Metadata: &ax.ObjectMeta{Name: n, Atespace: b.Atespace}, Spec: &ax.TaskSpec{Image: b.Image, Debug: true, Env: []*ax.EnvVar{{Name: "RAPTOR_BINDING_SHA256", Value: seal}}, Workspaces: []*ax.WorkspaceRef{{Name: ws.Metadata.Name, Path: "/workspace"}}}}})
	if err != nil {
		return nil, ErrUnknown
	}
	return view(t)
}
func (b *SDKBackend) DeleteRepositoryWorkspace(ctx context.Context, n string, repos []Repository) error {
	if !strings.HasPrefix(n, "raptor-") || !uuid.MatchString(strings.TrimPrefix(n, "raptor-")) {
		return ErrInvalid
	}
	r := Request{Binding: Binding{Operation: "repository.change", AttemptID: strings.TrimPrefix(n, "raptor-"), ClusterID: b.Atespace}, Repositories: repos}
	expected, err := repositoryWorkspace(r)
	if err != nil {
		return err
	}
	current, err := b.AX.GetWorkspace(ctx, &ax.GetWorkspaceRequest{Atespace: b.Atespace, Name: expected.Metadata.Name})
	if status.Code(err) == codes.NotFound {
		return nil
	}
	if err != nil {
		return ErrUnknown
	}
	if current == nil || !proto.Equal(current.Spec, expected.Spec) {
		return ErrConflict
	}
	_, err = b.AX.DeleteWorkspace(ctx, &ax.DeleteWorkspaceRequest{Atespace: b.Atespace, Name: expected.Metadata.Name})
	if err != nil && status.Code(err) != codes.NotFound {
		return ErrUnknown
	}
	return nil
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
		log.Printf("AX backend GetActor failed code=%s", status.Code(e))
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
	task, e := b.Get(ctx, n)
	if e != nil {
		return "", e
	}
	command, e := commandForPhase(phase, task.Workspace)
	if e != nil {
		return "", e
	}
	ctx, c, p, e := b.guest(ctx, n)
	if e != nil {
		return "", e
	}
	defer c.Close()
	timeout := processTimeout(phase, task.Workspace)
	v, e := p.StartProcess(ctx, &env.StartProcessRequest{Command: command, Cwd: "/workspace", Timeout: durationpb.New(timeout)})
	if e != nil {
		return "", ErrUnknown
	}
	return v.GetProcessId(), nil
}
func processTimeout(phase, workspace string) time.Duration {
	if phase == "prepare" && strings.HasPrefix(workspace, "raptor-repositories-") {
		return 5 * time.Minute // Up to 32 public fetches in eight bounded workers.
	}
	return 2 * time.Minute
}
func commandForPhase(phase, workspace string) ([]string, error) {
	repository := strings.HasPrefix(workspace, "raptor-repositories-")
	switch phase {
	case "prepare":
		command := "set -eu; umask 077; test ! -e /workspace/raptor-state; mkdir -p /tmp/raptor-private /tmp/raptor-oss; chmod 700 /tmp/raptor-private; test -f /opt/raptor-harness/runner.mjs"
		if repository {
			command += "; node /opt/raptor-harness/checkout-repositories.mjs /workspace/.raptor-repositories.json /tmp/raptor-private/repository-error.json; node /opt/raptor-harness/verify-repositories.mjs /workspace/.raptor-repositories.json /tmp/raptor-private/repository-error.json"
		}
		return []string{"sh", "-c", command}, nil
	case "restore":
		return []string{"node", "/opt/raptor-harness/runner.mjs", "/tmp/raptor-private/restore-job.json", "/tmp/raptor-private/restore-result.json"}, nil
	case "inference":
		command := []string{"node", "/opt/raptor-harness/runner.mjs", "/tmp/raptor-private/attempt-job.json", "/tmp/raptor-private/attempt-result.json"}
		if repository {
			command = append([]string{"env", "GIT_CONFIG_GLOBAL=/tmp/raptor-private/gitconfig", "GH_CONFIG_DIR=/tmp/raptor-private/gh"}, command...)
		}
		return command, nil
	default:
		return nil, ErrInvalid
	}
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
