package main

import (
	"context"
	"encoding/json"
	"errors"
	ax "github.com/google/ax/pkg/apis/v1alpha1"
	"strings"
	"testing"
	"time"
)

type fakeBackend struct {
	task                   *TaskView
	creates, runs, deletes int
	createError            error
	visibleOnError         bool
	runError               error
}
type delayedReadyBackend struct {
	fakeBackend
	readyPolls int
}

func (f *delayedReadyBackend) Get(ctx context.Context, name string) (*TaskView, error) {
	task, err := f.fakeBackend.Get(ctx, name)
	if err == nil && task.Phase == "Running" {
		f.readyPolls++
		task.Ready = f.readyPolls >= 3
	}
	return task, err
}

func TestEnsureWaitsForAXTaskReady(t *testing.T) {
	f := &delayedReadyBackend{}
	s := service(t, &f.fakeBackend)
	s.Backend = f
	if _, err := s.Ensure(context.Background(), request()); err != nil {
		t.Fatal(err)
	}
	if f.readyPolls < 3 || !f.task.Ready {
		t.Fatalf("returned before AX task became ready: polls=%d", f.readyPolls)
	}
}

func TestTaskViewUsesAXReadyCondition(t *testing.T) {
	task := &ax.Task{Metadata: &ax.ObjectMeta{Name: "task"}, Spec: &ax.TaskSpec{Image: "image"}, Status: &ax.TaskStatus{Phase: "Running", Conditions: []*ax.Condition{{Type: "Ready", Status: "False"}}}}
	first, err := view(task)
	if err != nil || first.Ready {
		t.Fatalf("unready task accepted: %+v %v", first, err)
	}
	task.Status.Conditions[0].Status = "True"
	second, err := view(task)
	if err != nil || !second.Ready {
		t.Fatalf("ready task rejected: %+v %v", second, err)
	}
}

type fakeRepositoryBackend struct {
	fakeBackend
	repositoryCreates int
	workspaceDeletes  int
	received          []Repository
}

func (f *fakeRepositoryBackend) DeleteRepositoryWorkspace(context.Context, string, []Repository) error {
	f.workspaceDeletes++
	return nil
}

func (f *fakeRepositoryBackend) CreateWithRepositories(_ context.Context, n, seal string, repos []Repository) (*TaskView, error) {
	f.repositoryCreates++
	f.received = append([]Repository(nil), repos...)
	v, err := f.fakeBackend.Create(context.Background(), n, seal)
	v.Workspace = "raptor-repositories-" + strings.TrimPrefix(n, "raptor-")
	return v, err
}
func TestRepositoryChangeUsesDedicatedCreate(t *testing.T) {
	f := &fakeRepositoryBackend{}
	s := service(t, &f.fakeBackend)
	s.Backend = f
	r := repositoryRequest()
	if _, err := s.Ensure(context.Background(), r); err != nil {
		t.Fatal(err)
	}
	if f.repositoryCreates != 1 || len(f.received) != 1 || f.received[0] != r.Repositories[0] {
		t.Fatal("repository snapshot not delivered")
	}
	if _, err := s.Ensure(context.Background(), r); err != nil || f.repositoryCreates != 1 {
		t.Fatalf("idempotent ensure: %v", err)
	}
	changed := r
	changed.Repositories = append([]Repository(nil), r.Repositories...)
	changed.Repositories[0].BaseCommit = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	if _, err := s.Ensure(context.Background(), changed); err == nil {
		t.Fatal("changed snapshot adopted existing task")
	}
}
func TestRepositoryChangeAcceptsSelectedEnvironmentWithoutBroadeningQuestion(t *testing.T) {
	r := repositoryRequest()
	r.Binding.EnvCode = "prod.ali"
	r.Repositories[0].EnvironmentCode = "prod.ali"
	if err := r.validate(true); err != nil {
		t.Fatal("selected environment rejected", err)
	}
	r.Binding.Operation = "application.question"
	r.Repositories = nil
	if err := r.validate(true); err == nil {
		t.Fatal("question admitted outside existing environment")
	}
}
func TestRepositoryTaskWrongWorkspaceCannotBeAdopted(t *testing.T) {
	f := &fakeRepositoryBackend{}
	s := service(t, &f.fakeBackend)
	s.Backend = f
	r := repositoryRequest()
	if _, err := s.Ensure(context.Background(), r); err != nil {
		t.Fatal(err)
	}
	f.task.Workspace = "raptor-runtime"
	if _, err := s.Ensure(context.Background(), r); err == nil {
		t.Fatal("foreign workspace accepted")
	}
}
func TestRepositoryRemoveDeletesWorkspaceAfterTask(t *testing.T) {
	f := &fakeRepositoryBackend{}
	s := service(t, &f.fakeBackend)
	s.Backend = f
	r := repositoryRequest()
	if _, err := s.Ensure(context.Background(), r); err != nil {
		t.Fatal(err)
	}
	if out, err := s.Remove(context.Background(), r); err != nil || !out.Absent || f.workspaceDeletes != 1 || f.deletes != 1 {
		t.Fatalf("cleanup: %+v %v workspaceDeletes=%d", out, err, f.workspaceDeletes)
	}
}

func TestRepositoryWorkspaceSpecClonesEachURLOnce(t *testing.T) {
	r := repositoryRequest()
	r.Repositories = append(r.Repositories, Repository{EnvironmentCode: "rdev.ali", Purpose: "kubernetes", URL: r.Repositories[0].URL, Directory: "kubernetes", BaseBranch: "main", BaseCommit: r.Repositories[0].BaseCommit})
	r.Repositories = append(r.Repositories, Repository{EnvironmentCode: "rdev.ali", Purpose: "kubernetes", URL: "https://github.com/example/apps.git", Directory: ".", BaseBranch: "release", BaseCommit: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"})
	w, err := repositoryWorkspace(r)
	if err != nil || w.Metadata.Name != "raptor-repositories-"+r.Binding.AttemptID || len(w.Spec.Git) != 0 || len(w.Spec.Files) != 1 {
		t.Fatalf("workspace: %+v %v", w, err)
	}
	var metadata []map[string]any
	if json.Unmarshal([]byte(w.Spec.Files[0].Content), &metadata) != nil || len(metadata) != 3 || metadata[1]["path"] != "/workspace/repositories/repo-0/kubernetes" {
		t.Fatalf("workspace path metadata: %+v", metadata)
	}
}
func TestRepositoryWorkspaceAllowsMultipleRowsPerPurpose(t *testing.T) {
	r := repositoryRequest()
	for i, url := range []string{"https://github.com/example/apps.git", "https://github.com/example/third.git"} {
		r.Repositories = append(r.Repositories, Repository{EnvironmentCode: "rdev.ali", Purpose: "kubernetes", URL: url, Directory: ".", BaseBranch: "main", BaseCommit: strings.Repeat(string(rune('b'+i)), 40)})
	}
	if w, err := repositoryWorkspace(r); err != nil || len(w.Spec.Git) != 0 {
		t.Fatalf("three unique public repositories rejected: %v", err)
	}
}
func TestRepositoryInferenceUsesPrivateGitConfigPaths(t *testing.T) {
	if got := processTimeout("prepare", "raptor-repositories-bbbbbbbb-bbbb-4bbb-bbbb-bbbbbbbbbbbb"); got != 5*time.Minute {
		t.Fatalf("repository prepare timeout = %v", got)
	}
	if got := processTimeout("prepare", "raptor-runtime"); got != 2*time.Minute {
		t.Fatalf("question prepare timeout = %v", got)
	}
	prepare, err := commandForPhase("prepare", "raptor-repositories-bbbbbbbb-bbbb-4bbb-bbbb-bbbbbbbbbbbb")
	if err != nil || !strings.Contains(strings.Join(prepare, " "), "checkout-repositories.mjs") {
		t.Fatalf("repository checkout must happen after the AX guest is ready: %v %v", prepare, err)
	}
	cmd, err := commandForPhase("inference", "raptor-repositories-bbbbbbbb-bbbb-4bbb-bbbb-bbbbbbbbbbbb")
	if err != nil || len(cmd) < 5 || cmd[0] != "env" || !strings.Contains(strings.Join(cmd, " "), "GIT_CONFIG_GLOBAL=/tmp/raptor-private/gitconfig") || !strings.Contains(strings.Join(cmd, " "), "GH_CONFIG_DIR=/tmp/raptor-private/gh") {
		t.Fatalf("repository command: %v %v", cmd, err)
	}
	question, err := commandForPhase("inference", "raptor-runtime")
	if err != nil || question[0] != "node" {
		t.Fatalf("question command changed: %v %v", question, err)
	}
}

func (f *fakeBackend) Get(context.Context, string) (*TaskView, error) {
	if f.task == nil {
		return nil, ErrAbsent
	}
	return f.task, nil
}
func (f *fakeBackend) Create(_ context.Context, n, seal string) (*TaskView, error) {
	f.creates++
	v := &TaskView{Name: n, Seal: seal, Image: "fixed", Phase: "Suspended"}
	if f.createError == nil || f.visibleOnError {
		f.task = v
	}
	return v, f.createError
}
func (f *fakeBackend) Resume(context.Context, string) (*TaskView, error) {
	f.task.Phase = "Running"
	f.task.Ready = true
	return f.task, nil
}
func (f *fakeBackend) Delete(context.Context, string) error              { f.deletes++; f.task = nil; return nil }
func (f *fakeBackend) ActorAbsent(context.Context, string) (bool, error) { return f.task == nil, nil }
func (f *fakeBackend) Run(context.Context, string, string) (string, error) {
	f.runs++
	return "process-id", f.runError
}
func (f *fakeBackend) Process(context.Context, string, string) (bool, int, error) {
	return true, 0, nil
}
func (f *fakeBackend) Read(context.Context, string, string, int64) ([]byte, error) {
	return []byte("fixture"), nil
}
func (f *fakeBackend) Write(context.Context, string, string, []byte) error { return nil }

func request() Request {
	return Request{Binding: Binding{RequestID: "aaaaaaaa-aaaa-4aaa-aaaa-aaaaaaaaaaaa", AttemptID: "bbbbbbbb-bbbb-4bbb-bbbb-bbbbbbbbbbbb", Operation: "application.question", ObjectKind: "application", ObjectCode: "app", EnvCode: "rdev.ali", SkillsCommit: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Model: "gpt-5.6-luna", DefinitionSHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", ClusterID: "platform-ack"}, Deadline: time.Now().Add(time.Minute)}
}
func repositoryRequest() Request {
	r := request()
	r.Binding.Operation = "repository.change"
	r.Repositories = []Repository{{EnvironmentCode: "rdev.ali", Purpose: "terraform", URL: "https://github.com/example/infra.git", Directory: ".", BaseBranch: "main", BaseCommit: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}}
	return r
}
func TestRepositoryChangeRequiresBoundSnapshotAndDedicatedBackend(t *testing.T) {
	s := service(t, &fakeBackend{})
	r := repositoryRequest()
	if _, err := s.Ensure(context.Background(), r); err == nil {
		t.Fatal("question-only backend accepted repository change")
	}
	r = request()
	r.Repositories = repositoryRequest().Repositories
	if _, err := s.Ensure(context.Background(), r); err == nil {
		t.Fatal("question path accepted repositories")
	}
	r = repositoryRequest()
	r.Repositories[0].URL = "https://github.com/example/infra.git?token=secret"
	if _, err := s.Ensure(context.Background(), r); err == nil {
		t.Fatal("credential-bearing URL accepted")
	}
}
func service(t *testing.T, f *fakeBackend) *Service {
	t.Helper()
	return &Service{Backend: f, StateDir: t.TempDir(), Image: "fixed"}
}
func TestRejectForeignBindingAndExpiredMutation(t *testing.T) {
	f := &fakeBackend{}
	s := service(t, f)
	r := request()
	r.Binding.EnvCode = "prod"
	if _, e := s.Ensure(context.Background(), r); e == nil {
		t.Fatal("wrong env accepted")
	}
	r = request()
	r.Deadline = time.Now().Add(-time.Second)
	if _, e := s.Ensure(context.Background(), r); e == nil {
		t.Fatal("expired create accepted")
	}
	if f.creates != 0 {
		t.Fatal("invalid request mutated backend")
	}
}
func TestCreateTimeoutAfterSuccessAdoptsExactOwnedTask(t *testing.T) {
	f := &fakeBackend{createError: errors.New("timeout"), visibleOnError: true}
	s := service(t, f)
	r := request()
	if _, e := s.Ensure(context.Background(), r); e != nil {
		t.Fatal(e)
	}
	if f.creates != 1 {
		t.Fatal("duplicate create")
	}
}
func TestInvisibleCreateTimeoutNeverRetriesOrClaimsCleanup(t *testing.T) {
	f := &fakeBackend{createError: errors.New("timeout")}
	s := service(t, f)
	r := request()
	if _, e := s.Ensure(context.Background(), r); e == nil {
		t.Fatal("unknown create accepted")
	}
	if _, e := s.Ensure(context.Background(), r); e == nil {
		t.Fatal("ambiguous create retried")
	}
	if _, e := s.Remove(context.Background(), r); e == nil {
		t.Fatal("unknown create falsely cleaned")
	}
	if f.creates != 1 || f.deletes != 0 {
		t.Fatal("ambiguous create replayed")
	}
}
func TestConflictingTaskIsNeverAdoptedOrDeleted(t *testing.T) {
	f := &fakeBackend{task: &TaskView{Seal: "foreign", Image: "fixed"}}
	s := service(t, f)
	r := request()
	if _, e := s.Ensure(context.Background(), r); e == nil {
		t.Fatal("foreign task adopted")
	}
	if _, e := s.Remove(context.Background(), r); e == nil {
		t.Fatal("foreign task removed")
	}
	if f.creates != 0 || f.deletes != 0 {
		t.Fatal("foreign ownership mutated")
	}
}
func TestAmbiguousProcessStartNeverReplaysInference(t *testing.T) {
	f := &fakeBackend{runError: errors.New("timeout")}
	s := service(t, f)
	r := request()
	if _, e := s.Ensure(context.Background(), r); e != nil {
		t.Fatal(e)
	}
	r.Phase = "prepare"
	f.runError = nil
	if _, e := s.Start(context.Background(), r); e != nil {
		t.Fatal(e)
	}
	r.Phase = "restore"
	if _, e := s.Start(context.Background(), r); e != nil {
		t.Fatal(e)
	}
	f.runs = 0
	f.runError = errors.New("timeout")
	r.Phase = "inference"
	if _, e := s.Start(context.Background(), r); e == nil {
		t.Fatal("unknown command accepted")
	}
	if _, e := s.Start(context.Background(), r); e == nil {
		t.Fatal("unknown command replayed")
	}
	if f.runs != 1 {
		t.Fatal("inference ran twice")
	}
}
func TestPrivateFileBoundary(t *testing.T) {
	for _, p := range []string{"/etc/passwd", "/tmp/raptor-private/../secret", "/tmp/raptor-private/a;cat", "/workspace/raptor-state/auth.json"} {
		if safePath(p) {
			t.Fatalf("unsafe path %q allowed", p)
		}
	}
	if !safePath("/tmp/raptor-private/attempt-job.json") {
		t.Fatal("private staging path rejected")
	}
}

func TestCodexBindingAndRefreshAckBoundary(t *testing.T) {
	f := &fakeBackend{}
	s := service(t, f)
	r := request()
	r.Binding.ProviderID = "codex"
	r.Binding.ConnectionVersion = 2
	r.Binding.Model = "gpt-6-sol"
	if _, e := s.Ensure(context.Background(), r); e != nil {
		t.Fatal(e)
	}
	r.Phase = "prepare"
	if _, e := s.Start(context.Background(), r); e != nil {
		t.Fatal(e)
	}
	r.Phase = "restore"
	if _, e := s.Start(context.Background(), r); e != nil {
		t.Fatal(e)
	}
	r.Phase = "inference"
	if _, e := s.Start(context.Background(), r); e != nil {
		t.Fatal(e)
	}
	r.Action = "write"
	r.Path = "/tmp/raptor-private/model-refresh-ack.json"
	r.Data = []byte(`{"status":"published","generation":2}`)
	if _, e := s.File(context.Background(), r); e != nil {
		t.Fatalf("refresh acknowledgement denied: %v", e)
	}
	r.Path = "/tmp/raptor-private/model-credential.json"
	if _, e := s.File(context.Background(), r); e == nil {
		t.Fatal("credential overwrite during inference accepted")
	}
	r.Binding.ConnectionVersion = 0
	if _, e := s.File(context.Background(), r); e == nil {
		t.Fatal("unbound model accepted")
	}
}

func TestInferenceRequiresSuccessfulRestore(t *testing.T) {
	f := &fakeBackend{}
	s := service(t, f)
	r := request()
	if _, e := s.Ensure(context.Background(), r); e != nil {
		t.Fatal(e)
	}
	r.Phase = "inference"
	if _, e := s.Start(context.Background(), r); e == nil {
		t.Fatal("inference started without restore")
	}
	if f.runs != 0 {
		t.Fatal("unprepared runtime executed")
	}
}

func TestNeverReceivedCreateCanBeTombstoned(t *testing.T) {
	f := &fakeBackend{}
	s := service(t, f)
	r := request()
	v, e := s.Remove(context.Background(), r)
	if e != nil || !v.Absent {
		t.Fatal("absence without create marker cannot be confirmed", e)
	}
	if _, e = s.Ensure(context.Background(), r); e == nil {
		t.Fatal("late create passed cleanup tombstone")
	}
	if f.creates != 0 {
		t.Fatal("late create replayed")
	}
}

func TestBridgeUpgradeKeepsAttemptImageForCleanup(t *testing.T) {
	f := &fakeBackend{}
	s := service(t, f)
	r := request()
	if _, e := s.Ensure(context.Background(), r); e != nil {
		t.Fatal(e)
	}
	s.Image = "new-default-image"
	v, e := s.Remove(context.Background(), r)
	if e != nil || !v.Absent {
		t.Fatal("bridge image update broke owned cleanup", e)
	}
}
