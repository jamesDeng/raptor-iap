package requests

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/catalog"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/domain"
	"testing"
)

func TestActionsPersistSignal(t *testing.T) {
	s, in, u := setup(t)
	ctx := context.Background()
	r, e := s.Create(ctx, u, "request", in)
	if e != nil {
		t.Fatal(e)
	}
	if e = s.Action(ctx, r.ID, "continue", ""); e == nil {
		t.Fatal("continue without guidance")
	}
	if e = s.Action(ctx, r.ID, "block", ""); e != nil {
		t.Fatal(e)
	}
	var n int
	s.Pool.QueryRow(ctx, "SELECT count(*) FROM raptor.outbox WHERE topic='signal'").Scan(&n)
	if n != 1 {
		t.Fatal("action signal missing")
	}
}
func TestDirectContinueRetriesFailedTargetsLocally(t *testing.T) {
	s, id := restartRequest(t)
	ctx := context.Background()
	client := &fakeRestart{calls: map[string]int{}, failure: "b"}
	s.RunRestartBatch(ctx, id, client)
	if e := s.Action(ctx, id, "continue", "Retry failed target"); e != nil {
		t.Fatal(e)
	}
	r, _ := s.Get(ctx, id)
	if r.Status != "queued" {
		t.Fatal("direct retry did not queue local runner")
	}
	var signals int
	s.Pool.QueryRow(ctx, "SELECT count(*) FROM raptor.outbox WHERE entity_id=$1 AND topic='signal'", id).Scan(&signals)
	if signals != 0 {
		t.Fatal("direct restart sent to agent Gateway")
	}
}

func TestQualificationContinueBindsTerminalAttempt(t *testing.T) {
	s, _, u := setup(t)
	ctx := context.Background()
	o, e := s.Catalog.CreateObject(ctx, catalog.CreateObjectInput{Kind: "db-proxy", Name: "Retry proxy"})
	if e != nil {
		t.Fatal(e)
	}
	s.ModelPolicy = &qualificationModel{enabled: true}
	in := domain.RequestInput{Type: "agent", Object: domain.ObjectRef{Kind: "db-proxy", Code: o.Code}, EnvCode: "adev", Model: "gpt-6-luna", ProviderID: "codex", Operations: []domain.Operation{{Name: "db-proxy.replace-nodes", Parameters: map[string]any{"mode": "qualification"}}}}
	r, e := s.Create(ctx, u, "qualification-retry", in)
	if e != nil {
		t.Fatal(e)
	}
	if e = s.Action(ctx, r.ID, "continue", "retry"); !errors.Is(e, domain.ErrConflict) {
		t.Fatal("missing terminal view accepted", e)
	}
	attempt := domain.NewID()
	view, _ := json.Marshal(map[string]any{"attemptId": attempt, "status": "failed"})
	if _, e = s.Pool.Exec(ctx, "INSERT INTO raptor.execution_views(request_id,public_view,synced_at) VALUES($1,$2,now())", r.ID, view); e != nil {
		t.Fatal(e)
	}
	if _, e = s.Pool.Exec(ctx, "UPDATE raptor.requests SET status='failed' WHERE id=$1", r.ID); e != nil {
		t.Fatal(e)
	}
	if e = s.Action(ctx, r.ID, "continue", "retry"); e != nil {
		t.Fatal(e)
	}
	var raw []byte
	if e = s.Pool.QueryRow(ctx, "SELECT payload FROM raptor.outbox WHERE entity_id=$1 AND topic='signal' ORDER BY created_at DESC LIMIT 1", r.ID).Scan(&raw); e != nil {
		t.Fatal(e)
	}
	var wire struct {
		Payload struct {
			AttemptID string `json:"attemptId"`
		} `json:"payload"`
	}
	if e = json.Unmarshal(raw, &wire); e != nil || wire.Payload.AttemptID != attempt {
		t.Fatal("retry not bound to failed attempt", e, string(raw))
	}
}
