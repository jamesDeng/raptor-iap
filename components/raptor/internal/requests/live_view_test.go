package requests

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/domain"
	"testing"
	"time"
)

type liveFixture struct {
	execution map[string]any
	events    []GatewayEvent
	offline   bool
}

func (f *liveFixture) Execution(context.Context, string) (map[string]any, error) {
	if f.offline {
		return nil, errors.New("offline")
	}
	return f.execution, nil
}
func (f *liveFixture) Progress(context.Context, string, int64) ([]GatewayEvent, error) {
	return f.events, nil
}
func TestLiveEventRetainsSourceIdentityAndTime(t *testing.T) {
	s, in, u := setup(t)
	ctx := context.Background()
	r, e := s.Create(ctx, u, "events", in)
	if e != nil {
		t.Fatal(e)
	}
	var event GatewayEvent
	json.Unmarshal([]byte(`{"eventId":"live-1","requestId":"`+r.ID+`","attemptId":"attempt-1","sequence":1,"kind":"tool_result","summary":"Read live state","details":{"tool":"deployment_status_get"},"evidenceMode":"live","occurredAt":"2026-10-07T01:00:00Z"}`), &event)
	s.Gateway = &liveFixture{events: []GatewayEvent{event}}
	timeline, e := s.Timeline(ctx, r.ID, 0)
	if e != nil || len(timeline.Events) != 1 {
		t.Fatal("live event rejected", e)
	}
	b, _ := json.Marshal(timeline.Events[0])
	var wire map[string]any
	json.Unmarshal(b, &wire)
	if wire["attemptId"] != "attempt-1" || !timeline.Events[0].OccurredAt.Equal(time.Date(2026, 10, 7, 1, 0, 0, 0, time.UTC)) {
		t.Fatal("source identity/time lost", string(b))
	}
	again, e := s.Timeline(ctx, r.ID, 0)
	if e != nil || len(again.Events) != 1 {
		t.Fatal("duplicate imported", e)
	}
}
func TestGatewayPublicViewCachedOnOutage(t *testing.T) {
	s, in, u := setup(t)
	ctx := context.Background()
	r, e := s.Create(ctx, u, "cache", in)
	if e != nil {
		t.Fatal(e)
	}
	f := &liveFixture{execution: map[string]any{"requestId": r.ID, "attemptId": "attempt", "status": "running", "runtimeMode": "live", "stage": "checkpointing", "result": map[string]any{"requestId": r.ID, "attemptId": "attempt", "answer": "Point-in-time observation", "actualModel": "gpt-5.6-luna"}, "checkpoint": map[string]any{"path": "private-path"}}}
	s.Gateway = f
	v, e := s.View(ctx, r.ID)
	if e != nil {
		t.Fatal(e)
	}
	if _, ok := v.Execution["checkpoint"]; ok {
		t.Fatal("private checkpoint returned")
	}
	f.offline = true
	v, e = s.View(ctx, r.ID)
	if e != nil || v.ExecutionAvailable || v.Execution["result"] == nil {
		t.Fatal("saved result lost", e)
	}
}
func TestForeignGatewayExecutionCannotUpdateRequest(t *testing.T) {
	s, in, u := setup(t)
	ctx := context.Background()
	r, e := s.Create(ctx, u, "foreign", in)
	if e != nil {
		t.Fatal(e)
	}
	s.Gateway = &liveFixture{execution: map[string]any{"requestId": "foreign", "status": "completed", "runtimeMode": "live"}}
	v, e := s.View(ctx, r.ID)
	if e != nil {
		t.Fatal(e)
	}
	if v.ExecutionAvailable || v.Request.Status == "completed" {
		t.Fatal("foreign execution trusted")
	}
}
func TestConflictingDuplicateEventRejected(t *testing.T) {
	s, in, u := setup(t)
	ctx := context.Background()
	r, e := s.Create(ctx, u, "dup", in)
	if e != nil {
		t.Fatal(e)
	}
	f := &liveFixture{events: []GatewayEvent{{EventID: "e", RequestID: r.ID, AttemptID: "a", Sequence: 1, Kind: "progress", Summary: "first", EvidenceMode: "live", OccurredAt: time.Now().UTC()}}}
	s.Gateway = f
	if _, e = s.Timeline(ctx, r.ID, 0); e != nil {
		t.Fatal(e)
	}
	f.events[0].Summary = "different"
	if _, e = s.Timeline(ctx, r.ID, 0); e == nil {
		t.Fatal("conflicting duplicate accepted")
	}
}
func TestQuestionCompletionNeedsEvidenceAndCleanup(t *testing.T) {
	r := domain.Request{ID: "request", Definition: questionInput(t, "health?", "gpt-5.6-luna")}
	raw := map[string]any{"requestId": "request", "attemptId": "attempt", "status": "completed", "runtimeMode": "live", "result": map[string]any{"requestId": "request", "attemptId": "attempt", "answer": "healthy", "actualModel": "gpt-5.6-luna", "selectedModel": "gpt-5.6-luna"}}
	if _, e := publicExecution(raw, r); e == nil {
		t.Fatal("answer-only success accepted")
	}
}
func TestCancelledLiveQuestionNeedsConfirmedCleanup(t *testing.T) {
	r := domain.Request{ID: "request", Definition: questionInput(t, "health?", "gpt-5.6-luna")}
	raw := map[string]any{"requestId": "request", "attemptId": "attempt", "status": "cancelled", "runtimeMode": "live", "cleanupStatus": "unknown"}
	if _, e := publicExecution(raw, r); e == nil {
		t.Fatal("unconfirmed cancellation treated as terminal")
	}
}
func TestLiveQuestionSuccessCarriesBoundEvidence(t *testing.T) {
	r := domain.Request{ID: "request", Definition: questionInput(t, "health?", "gpt-5.6-luna")}
	identity := map[string]any{"envCode": "rdev.ali", "appCode": "existing", "clusterId": "cluster", "namespace": "ns", "name": "app", "uid": "uid"}
	evidence := []any{map[string]any{"server": "raptor", "tool": "request_get", "evidenceMode": "live", "observedAt": "2026-10-07T01:00:00Z", "identity": map[string]any{"envCode": "rdev.ali", "objectCode": "existing"}}}
	for _, tool := range []string{"cloud_identity_get", "deployments_list", "deployment_status_get"} {
		evidence = append(evidence, map[string]any{"server": "infra", "tool": tool, "observedAt": "2026-10-07T01:00:00Z", "evidenceMode": "live", "identity": identity, "state": map[string]any{"readyReplicas": 0}})
	}
	raw := map[string]any{"requestId": "request", "attemptId": "attempt", "status": "completed", "runtimeMode": "live", "checkpointStatus": "verified", "cleanupStatus": "confirmed", "result": map[string]any{"requestId": "request", "attemptId": "attempt", "answer": "The deployment is unhealthy at the observation time.", "actualModel": "gpt-5.6-luna", "selectedModel": "gpt-5.6-luna", "evidence": evidence}}
	if _, e := publicExecution(raw, r); e != nil {
		t.Fatal("grounded unhealthy observation rejected", e)
	}
	identity["uid"] = ""
	if _, e := publicExecution(raw, r); e == nil {
		t.Fatal("missing UID accepted")
	}
}
func TestEventSourceOrder(t *testing.T) {
	s, in, u := setup(t)
	ctx := context.Background()
	r, e := s.Create(ctx, u, "ordered", in)
	if e != nil {
		t.Fatal(e)
	}
	stamp := time.Now().UTC()
	s.Gateway = &liveFixture{events: []GatewayEvent{{RequestID: r.ID, AttemptID: "a", EventID: "second", Sequence: 2, Kind: "progress", Summary: "second", EvidenceMode: "live", OccurredAt: stamp}, {RequestID: r.ID, AttemptID: "a", EventID: "first", Sequence: 1, Kind: "progress", Summary: "first", EvidenceMode: "live", OccurredAt: stamp}}}
	v, e := s.Timeline(ctx, r.ID, 0)
	if e != nil || len(v.Events) != 2 {
		t.Fatal(e)
	}
	if v.Events[0].Summary != "first" {
		t.Fatal("source order lost")
	}
}
func TestStaleExecutionCannotEraseTerminalAnswer(t *testing.T) {
	s, in, u := setup(t)
	ctx := context.Background()
	r, e := s.Create(ctx, u, "stale", in)
	if e != nil {
		t.Fatal(e)
	}
	f := &liveFixture{execution: map[string]any{"requestId": r.ID, "attemptId": "a", "status": "completed", "runtimeMode": "simulated", "updatedAt": "2026-10-07T02:00:00Z", "result": map[string]any{"requestId": r.ID, "attemptId": "a", "answer": "saved answer", "actualModel": "fixture"}}}
	s.Gateway = f
	if _, e = s.View(ctx, r.ID); e != nil {
		t.Fatal(e)
	}
	f.execution = map[string]any{"requestId": r.ID, "attemptId": "a", "status": "running", "runtimeMode": "simulated", "updatedAt": "2026-10-07T01:00:00Z"}
	v, e := s.View(ctx, r.ID)
	if e != nil {
		t.Fatal(e)
	}
	if v.Request.Status != "completed" || v.Execution["result"] == nil {
		t.Fatal("stale snapshot erased terminal answer")
	}
}
func TestInvalidEventBatchIsAtomic(t *testing.T) {
	s, in, u := setup(t)
	ctx := context.Background()
	r, e := s.Create(ctx, u, "atomic", in)
	if e != nil {
		t.Fatal(e)
	}
	s.Gateway = &liveFixture{events: []GatewayEvent{{EventID: "first", Sequence: 1, Kind: "progress", Summary: "first", EvidenceMode: "simulated"}, {EventID: "invalid", Sequence: 2, Kind: "progress", Summary: "bad", EvidenceMode: "unknown"}}}
	if _, e = s.Timeline(ctx, r.ID, 0); e == nil {
		t.Fatal("invalid batch accepted")
	}
	var count int
	s.Pool.QueryRow(ctx, "SELECT count(*) FROM raptor.events WHERE request_id=$1", r.ID).Scan(&count)
	if count != 0 {
		t.Fatal("partially imported batch advanced cursor")
	}
}
func TestLiveCompletionRejectsSimulatedContext(t *testing.T) {
	r := domain.Request{ID: "request", Definition: questionInput(t, "health?", "gpt-5.6-luna")}
	var raw map[string]any
	json.Unmarshal([]byte(`{"requestId":"request","attemptId":"a","status":"completed","runtimeMode":"live","checkpointStatus":"verified","cleanupStatus":"confirmed","result":{"requestId":"request","attemptId":"a","answer":"healthy","selectedModel":"gpt-5.6-luna","actualModel":"gpt-5.6-luna","evidence":[{"server":"raptor","tool":"request_get","evidenceMode":"simulated"},{"server":"infra","tool":"cloud_identity_get","observedAt":"2026-10-07T01:00:00Z","evidenceMode":"live"},{"server":"infra","tool":"deployments_list","observedAt":"2026-10-07T01:00:00Z","evidenceMode":"live"},{"server":"infra","tool":"deployment_status_get","observedAt":"2026-10-07T01:00:00Z","evidenceMode":"live","identity":{"envCode":"rdev.ali","appCode":"existing","clusterId":"c","namespace":"ns","name":"app","uid":"uid"}}]}}`), &raw)
	if _, e := publicExecution(raw, r); e == nil {
		t.Fatal("simulated context accepted for live success")
	}
}
