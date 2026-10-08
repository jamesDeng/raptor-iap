package main

import (
	"context"
	"errors"
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

func TestNeverReceivedCreateCanBeTombstoned(t *testing.T){f:=&fakeBackend{};s:=service(t,f);r:=request();v,e:=s.Remove(context.Background(),r);if e!=nil||!v.Absent{t.Fatal("absence without create marker cannot be confirmed",e)};if _,e=s.Ensure(context.Background(),r);e==nil{t.Fatal("late create passed cleanup tombstone")};if f.creates!=0{t.Fatal("late create replayed")}}

func TestBridgeUpgradeKeepsAttemptImageForCleanup(t *testing.T){f:=&fakeBackend{};s:=service(t,f);r:=request();if _,e:=s.Ensure(context.Background(),r);e!=nil{t.Fatal(e)};s.Image="new-default-image";v,e:=s.Remove(context.Background(),r);if e!=nil||!v.Absent{t.Fatal("bridge image update broke owned cleanup",e)}}
