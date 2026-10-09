package bridge

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/jamesDeng/raptor-iap/components/t3code-bridge/internal/config"
	"github.com/jamesDeng/raptor-iap/components/t3code-bridge/internal/raptor"
	"github.com/jamesDeng/raptor-iap/components/t3code-bridge/internal/state"
	"testing"
	"time"
)

type fakeClient struct {
	keys      []string
	payloads  []string
	creates   int
	cancelled bool
	lost      bool
	view      raptor.RequestView
	events    raptor.Timeline
	onCreate  func()
}

func (f *fakeClient) Login(context.Context) (string, error) { return "u", nil }
func (f *fakeClient) Create(_ context.Context, k string, b json.RawMessage) (raptor.Request, error) {
	f.creates++
	f.keys = append(f.keys, k)
	f.payloads = append(f.payloads, string(b))
	if f.onCreate != nil {
		f.onCreate()
	}
	if f.lost && f.creates == 1 {
		return raptor.Request{}, raptor.ErrUnavailable
	}
	var d raptor.Definition
	json.Unmarshal(b, &d)
	return raptor.Request{ID: "req", CreatorID: "u", Definition: d}, nil
}
func (f *fakeClient) View(context.Context, string) (raptor.RequestView, error) { return f.view, nil }
func (f *fakeClient) Events(context.Context, string, int64) (raptor.Timeline, error) {
	return f.events, nil
}
func (f *fakeClient) Cancel(context.Context, string) error { f.cancelled = true; return nil }
func fixture(t *testing.T) (*Runner, *fakeClient, *state.Store, string) {
	t.Helper()
	s, e := state.Open(t.TempDir() + "/state")
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { s.Close() })
	c := config.Config{Origin: "https://raptor.example", ApplicationCode: "app", EnvironmentCode: "dev", Model: "gpt-5.6-luna"}
	f := &fakeClient{}
	r := New(f, s, c)
	r.pollInterval = time.Millisecond
	r.deadline = time.Second
	r.cancelDeadline = time.Second
	id, e := r.NewSession(context.Background())
	if e != nil {
		t.Fatal(e)
	}
	return r, f, s, id
}
func goodView() raptor.RequestView {
	d := raptor.Definition{Type: "agent", Model: "gpt-5.6-luna", Object: raptor.ObjectRef{Kind: "application", Code: "app"}, EnvCode: "dev", Operations: []raptor.Operation{{Name: "application.question", Parameters: map[string]string{"question": "health?"}}}}
	return raptor.RequestView{Request: raptor.Request{ID: "req", CreatorID: "u", Definition: d, Status: "completed"}, ExecutionAvailable: true, Execution: &raptor.Execution{RequestID: "req", AttemptID: "attempt", Status: "completed", RuntimeMode: "live", UpdatedAt: time.Now(), CheckpointStatus: "verified", CleanupStatus: &raptor.Cleanup{SandboxAbsent: true, KeyAbsent: true, AccessRevoked: true}, Result: &raptor.Result{RequestID: "req", AttemptID: "attempt", SelectedModel: "gpt-5.6-luna", ActualModel: "gpt-5.6-luna", Answer: "healthy", GeneratedAt: time.Now(), Usage: []raptor.Usage{{Input: 1, Output: 1, Total: 2}}, Evidence: []raptor.Evidence{{Server: "raptor", Tool: "request_get", EvidenceMode: "catalog", ObservedAt: time.Now(), Identity: raptor.Identity{RequestID: "req", EnvCode: "dev", ObjectCode: "app"}, State: json.RawMessage(`{}`)}, {Server: "infra", Tool: "cloud_identity_get", EvidenceMode: "live", ObservedAt: time.Now(), Identity: raptor.Identity{EnvCode: "dev"}, State: json.RawMessage(`{"accountMatches":true}`)}, {Server: "infra", Tool: "deployments_list", EvidenceMode: "live", ObservedAt: time.Now(), Identity: raptor.Identity{EnvCode: "dev", AppCode: "app"}, State: json.RawMessage(`{}`)}, {Server: "infra", Tool: "deployment_status_get", EvidenceMode: "live", ObservedAt: time.Now(), Identity: raptor.Identity{EnvCode: "dev", AppCode: "app"}, State: json.RawMessage(`{}`)}}}}}
}
func TestAcceptedPostLostResponseReusesExactKeyAndBytes(t *testing.T) {
	r, f, _, id := fixture(t)
	f.lost = true
	f.view = goodView()
	_, e := r.Prompt(context.Background(), id, "health?", func(Update) error { return nil })
	if e == nil {
		t.Fatal("uncertain POST completed")
	}
	if e = r.LoadSession(context.Background(), id, func(Update) error { return nil }); e != nil {
		t.Fatal(e)
	}
	if f.creates != 2 || f.keys[0] != f.keys[1] || f.payloads[0] != f.payloads[1] {
		t.Fatal(f.keys, f.payloads)
	}
}
func TestReloadObservesWithoutResubmitting(t *testing.T) {
	r, f, _, id := fixture(t)
	f.view = goodView()
	o, e := r.Prompt(context.Background(), id, "health?", func(Update) error { return nil })
	if e != nil || o.Status != "completed" {
		t.Fatal(o, e)
	}
	if e = r.LoadSession(context.Background(), id, func(Update) error { return nil }); e != nil || f.creates != 1 {
		t.Fatal(f.creates, e)
	}
}
func TestAnswerRequiresVerifiedCheckpointAndCleanup(t *testing.T) {
	for _, mutate := range []func(*raptor.RequestView){func(v *raptor.RequestView) { v.Execution.CheckpointStatus = "failed" }, func(v *raptor.RequestView) { v.Execution.CleanupStatus.AccessRevoked = false }, func(v *raptor.RequestView) { v.Execution.RecoveryNeeded = true }, func(v *raptor.RequestView) { v.Execution.Result = nil }} {
		r, f, _, id := fixture(t)
		f.view = goodView()
		mutate(&f.view)
		if _, e := r.Prompt(context.Background(), id, "health?", func(Update) error { return nil }); e == nil {
			t.Fatal("unsafe success")
		}
	}
}
func TestUnavailableOrStaleViewCannotComplete(t *testing.T) {
	r, f, s, id := fixture(t)
	f.view = goodView()
	f.view.ExecutionAvailable = false
	if _, e := r.Prompt(context.Background(), id, "health?", func(Update) error { return nil }); e == nil {
		t.Fatal("stale success")
	}
	v, _ := s.Load(id, r.binding)
	if v.Terminal {
		t.Fatal("unknown made terminal")
	}
}
func TestWrongAttemptModelAndSelectorsRejected(t *testing.T) {
	for _, m := range []func(*raptor.RequestView){func(v *raptor.RequestView) { v.Execution.Result.AttemptID = "wrong" }, func(v *raptor.RequestView) { v.Execution.Result.ActualModel = "other" }, func(v *raptor.RequestView) { v.Request.Definition.EnvCode = "prod" }, func(v *raptor.RequestView) { v.Request.ID = "wrong" }} {
		r, f, _, id := fixture(t)
		f.view = goodView()
		m(&f.view)
		if _, e := r.Prompt(context.Background(), id, "health?", func(Update) error { return nil }); e == nil {
			t.Fatal("bad binding accepted")
		}
	}
}
func TestCursorOrderingAndPaging(t *testing.T) {
	r, f, _, id := fixture(t)
	f.view = goodView()
	f.events.Events = []raptor.Event{{RequestID: "req", AttemptID: "attempt", Sequence: 2, Summary: "second"}, {RequestID: "req", AttemptID: "attempt", Sequence: 1, Summary: "first"}}
	if _, e := r.Prompt(context.Background(), id, "health?", func(Update) error { return nil }); e == nil {
		t.Fatal("regression accepted")
	}
}
func TestConcurrentPromptRejected(t *testing.T) {
	r, f, _, id := fixture(t)
	f.view = goodView()
	f.onCreate = func() {
		_, e := r.Prompt(context.Background(), id, "another", func(Update) error { return nil })
		if !errors.Is(e, ErrBusy) {
			t.Error(e)
		}
	}
	if _, e := r.Prompt(context.Background(), id, "health?", func(Update) error { return nil }); e != nil {
		t.Fatal(e)
	}
}
func TestCancelBeforeSubmissionResponse(t *testing.T) {
	r, f, _, id := fixture(t)
	f.view = goodView()
	f.view.Request.Status = "cancelled"
	f.view.Execution.Status = "cancelled"
	f.view.Execution.Result = nil
	f.onCreate = func() {
		if e := r.Cancel(context.Background(), id); e != nil {
			t.Error(e)
		}
	}
	o, e := r.Prompt(context.Background(), id, "health?", func(Update) error { return nil })
	if e != nil || o.Status != "cancelled" || !f.cancelled {
		t.Fatal(o, e)
	}
}
func TestCancelCompletionRace(t *testing.T) {
	r, f, _, id := fixture(t)
	f.view = goodView()
	f.onCreate = func() { r.Cancel(context.Background(), id) }
	o, e := r.Prompt(context.Background(), id, "health?", func(Update) error { return nil })
	if e != nil || o.Status != "completed" {
		t.Fatal(o, e)
	}
}
func TestDeadlineRetainsUnresolvedRequest(t *testing.T) {
	r, f, s, id := fixture(t)
	f.view = goodView()
	f.view.ExecutionAvailable = false
	r.Prompt(context.Background(), id, "health?", func(Update) error { return nil })
	v, _ := s.Load(id, r.binding)
	if v.RequestID != "req" || v.Terminal {
		t.Fatal(v)
	}
	if _, e := r.Prompt(context.Background(), id, "another", func(Update) error { return nil }); e == nil {
		t.Fatal("overlap")
	}
}

func TestPromptAdmissionPreservesImmediateCancel(t *testing.T) {
	r, f, st, id := fixture(t)
	f.view = goodView()
	f.view.Execution.Status = "cancelled"
	_, err := r.PromptReady(context.Background(), id, "health?", func(Update) error { return nil }, func() {
		if e := r.Cancel(context.Background(), id); e != nil {
			t.Fatal(e)
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	saved, err := st.Load(id, r.binding)
	if err != nil || !saved.CancelIntent || !f.cancelled {
		t.Fatal("immediate cancel lost", err)
	}
}
func TestCancelledLifecycleReplaysAfterReload(t *testing.T) {
	r, f, _, id := fixture(t)
	f.view = goodView()
	f.view.Execution.Status = "cancelled"
	if _, e := r.Prompt(context.Background(), id, "health?", func(Update) error { return nil }); e != nil {
		t.Fatal(e)
	}
	found := false
	if e := r.LoadSession(context.Background(), id, func(u Update) error {
		if u.Kind == "progress" && u.Text == "cancelled" {
			found = true
		}
		return nil
	}); e != nil {
		t.Fatal(e)
	}
	if !found {
		t.Fatal("cancelled lifecycle missing on reload")
	}
}
