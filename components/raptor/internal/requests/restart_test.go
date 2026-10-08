package requests

import (
	"context"
	"encoding/json"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/adapters"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/domain"
	"sync"
	"testing"
	"time"
)

type fakeRestart struct {
	mu      sync.Mutex
	calls   map[string]int
	started int
	barrier chan struct{}
	failure string
	unknown bool
	mode    string
}
type staleRestart struct {
	fakeRestart
	wrongUID     bool
	lookupFails  bool
	zeroObserved bool
}

func (f *staleRestart) GetDeploymentStatus(ctx context.Context, target domain.RestartTarget) (adapters.DeploymentStatus, error) {
	if f.lookupFails {
		return adapters.DeploymentStatus{}, adapters.ErrRejected
	}
	if f.zeroObserved {
		f.mu.Lock()
		generation := int64(1 + f.calls[target.Name])
		f.mu.Unlock()
		return adapters.DeploymentStatus{UID: target.UID, Generation: generation, ObservedGeneration: generation, DesiredReplicas: 1, EvidenceMode: "live"}, nil
	}
	uid := target.UID
	if f.wrongUID {
		uid = "other-uid"
	}
	return adapters.DeploymentStatus{UID: uid, Generation: 1, ObservedGeneration: 1, DesiredReplicas: 1, AvailableReplicas: 1, Replicas: 1, UpdatedReplicas: 1, ReadyReplicas: 1}, nil
}
func TestRestartIdentityLookupAndDeadline(t *testing.T) {
	for _, mode := range []string{"identity", "lookup", "deadline"} {
		t.Run(mode, func(t *testing.T) {
			s, id := restartRequest(t)
			s.Pool.Exec(context.Background(), "DELETE FROM raptor.targets WHERE request_id=$1 AND target->>'name'='b'", id)
			f := &staleRestart{fakeRestart: fakeRestart{calls: map[string]int{}}, wrongUID: mode == "identity", lookupFails: mode == "lookup"}
			tick := 0
			s.RestartNow = func() time.Time { tick++; return time.Unix(int64(tick)*600, 0) }
			if e := s.RunRestartBatch(context.Background(), id, f); e != nil {
				t.Fatal(e)
			}
			items, e := s.RestartItems(context.Background(), id)
			if e != nil || len(items) != 1 {
				t.Fatal(e)
			}
			if mode == "deadline" {
				if items[0].State != "unknown" {
					t.Fatal("stale ready rollout counted as success")
				}
			} else if f.started != 0 {
				t.Fatal("restart sent after invalid lookup")
			}
		})
	}
}

func (f *fakeRestart) RestartDeployment(ctx context.Context, id string, t domain.RestartTarget) error {
	f.mu.Lock()
	f.calls[t.Name]++
	f.started++
	if f.started == 2 && f.barrier != nil {
		close(f.barrier)
	}
	f.mu.Unlock()
	if f.barrier != nil {
		select {
		case <-f.barrier:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	if t.Name == f.failure {
		if f.unknown {
			return adapters.ErrSubmissionUnknown
		}
		return adapters.ErrRejected
	}
	return nil
}
func (f *fakeRestart) GetDeploymentStatus(ctx context.Context, t domain.RestartTarget) (adapters.DeploymentStatus, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	generation := int64(1 + f.calls[t.Name])
	return adapters.DeploymentStatus{EvidenceMode: f.mode, UID: t.UID, Generation: generation, ObservedGeneration: generation, DesiredReplicas: 1, AvailableReplicas: 1, Replicas: 1, UpdatedReplicas: 1, ReadyReplicas: 1}, nil
}
func restartRequest(t *testing.T) (*Service, string) {
	s, _, u := setup(t)
	id := domain.NewID()
	targets := []domain.RestartTarget{{AppCode: "app", EnvCode: "adev", ClusterID: "cluster", Namespace: "ns", Name: "a", UID: "uid-a"}, {AppCode: "app", EnvCode: "adev", ClusterID: "cluster", Namespace: "ns", Name: "b", UID: "uid-b"}}
	ctx := context.Background()
	s.Pool.Exec(ctx, `INSERT INTO raptor.requests(id,creator_id,idempotency_key,input_hash,definition,schema_hash) VALUES($1,$2,'restart','hash','{"type":"direct"}','schema')`, id, u.ID)
	for _, target := range targets {
		if e := s.InsertRestartTarget(ctx, id, target); e != nil {
			t.Fatal(e)
		}
	}
	return s, id
}
func TestRestartsStartInParallel(t *testing.T) {
	s, id := restartRequest(t)
	f := &fakeRestart{calls: map[string]int{}, barrier: make(chan struct{})}
	if e := s.RunRestartBatch(context.Background(), id, f); e != nil {
		t.Fatal(e)
	}
	if f.started != 2 {
		t.Fatal("not parallel")
	}
}
func TestPartialFailureContinuesAndRetryOnlyFailedTargets(t *testing.T) {
	s, id := restartRequest(t)
	f := &fakeRestart{calls: map[string]int{}, failure: "b"}
	s.RunRestartBatch(context.Background(), id, f)
	f.failure = ""
	s.RunRestartBatch(context.Background(), id, f)
	if f.calls["a"] != 1 || f.calls["b"] != 2 {
		t.Fatal("successful target restarted again")
	}
}
func TestUnknownSubmissionNotRepeated(t *testing.T) {
	s, id := restartRequest(t)
	f := &fakeRestart{calls: map[string]int{}, failure: "b", unknown: true}
	s.RunRestartBatch(context.Background(), id, f)
	s.RunRestartBatch(context.Background(), id, f)
	if f.calls["b"] != 1 {
		t.Fatal("unknown submission repeated")
	}
}

func TestBackendRestartDoesNotAbandonOrReplayMutation(t *testing.T) {
	s, id := restartRequest(t)
	ctx := context.Background()
	s.Pool.Exec(ctx, "UPDATE raptor.requests SET status='running' WHERE id=$1", id)
	s.Pool.Exec(ctx, "UPDATE raptor.targets SET state=CASE WHEN target->>'name'='a' THEN 'submitting' ELSE 'observing' END WHERE request_id=$1", id)
	restarted := *s
	client := &fakeRestart{calls: map[string]int{}}
	if e := restarted.RunDirectPending(ctx, client); e != nil {
		t.Fatal(e)
	}
	items, e := s.RestartItems(ctx, id)
	if e != nil {
		t.Fatal(e)
	}
	for _, v := range items {
		if v.State != "unknown" {
			t.Fatal("interrupted mutation left silently active", v.State)
		}
	}
	request, _ := s.Get(ctx, id)
	if request.Status == "running" {
		t.Fatal("interrupted batch stuck running")
	}
	if client.started != 0 {
		t.Fatal("mutation replayed during recovery")
	}
}

func TestLiveUnknownRestartKeepsEvidenceAndDoesNotReplay(t *testing.T) {
	s, id := restartRequest(t)
	f := &fakeRestart{calls: map[string]int{}, failure: "b", unknown: true, mode: "live"}
	if e := s.RunRestartBatch(context.Background(), id, f); e != nil {
		t.Fatal(e)
	}
	items, e := s.RestartItems(context.Background(), id)
	if e != nil {
		t.Fatal(e)
	}
	for _, item := range items {
		var details map[string]any
		if json.Unmarshal(item.Details, &details) != nil || details["evidenceMode"] != "live" {
			t.Fatalf("lost live evidence: %s", item.Details)
		}
	}
	s.RunRestartBatch(context.Background(), id, f)
	if f.calls["b"] != 1 {
		t.Fatal("ambiguous live mutation replayed")
	}
}

func TestZeroObservedReplicasCannotCompleteDesiredDeployment(t *testing.T) {
	s, id := restartRequest(t)
	f := &staleRestart{fakeRestart: fakeRestart{calls: map[string]int{}}, zeroObserved: true}
	tick := 0
	s.RestartNow = func() time.Time { tick++; return time.Unix(int64(tick)*600, 0) }
	if e := s.RunRestartBatch(context.Background(), id, f); e != nil {
		t.Fatal(e)
	}
	items, e := s.RestartItems(context.Background(), id)
	if e != nil {
		t.Fatal(e)
	}
	for _, item := range items {
		if item.State != "unknown" {
			t.Fatal("zero observed replicas reported success", item.State)
		}
	}
}
