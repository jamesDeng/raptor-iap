package requests

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/jamesDeng/raptor-iap/components/raptor/internal/domain"
)

func TestTerminalPauseClassification(t *testing.T) {
	pause := json.RawMessage(`{"signalId":"id","kind":"pause","payload":{"reason":"waiting_approval"}}`)
	for _, status := range []string{"cancelled", "completed", "failed"} {
		if !terminalPause(status, "signal", pause) {
			t.Fatalf("terminal %s pause was not superseded", status)
		}
	}
	for _, status := range []string{"queued", "running", "waiting_approval", "blocked"} {
		if terminalPause(status, "signal", pause) {
			t.Fatalf("active %s pause was superseded", status)
		}
	}
	if terminalPause("cancelled", "signal", json.RawMessage(`{"kind":"cancel"}`)) || terminalPause("cancelled", "dispatch", pause) {
		t.Fatal("non-pause item was superseded")
	}
}

type rejectPauseClient struct {
	dispatched []string
	pauseCalls int
}

func (c *rejectPauseClient) PutRequest(_ context.Context, id string) error {
	c.dispatched = append(c.dispatched, id)
	return nil
}
func (c *rejectPauseClient) SendSignal(context.Context, string, json.RawMessage) error {
	c.pauseCalls++
	return domain.ErrUnavailable
}

func TestTerminalPauseDoesNotBlockNextDispatch(t *testing.T) {
	s, in, u := setup(t)
	ctx := context.Background()
	old, err := s.Create(ctx, u, "old-terminal", in)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Pool.Exec(ctx, "UPDATE raptor.outbox SET delivered=true WHERE entity_id=$1 AND topic='dispatch'", old.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Pool.Exec(ctx, "UPDATE raptor.requests SET status='cancelled' WHERE id=$1", old.ID); err != nil {
		t.Fatal(err)
	}
	pauseID := domain.NewID()
	payload, _ := json.Marshal(map[string]any{"signalId": pauseID, "kind": "pause", "payload": map[string]string{"reason": "waiting_approval"}})
	if _, err = s.Pool.Exec(ctx, "INSERT INTO raptor.outbox(id,topic,entity_id,payload,created_at) VALUES($1,'signal',$2,$3,$4)", pauseID, old.ID, payload, time.Now().Add(-time.Minute)); err != nil {
		t.Fatal(err)
	}
	next, err := s.Create(ctx, u, "next-valid", in)
	if err != nil {
		t.Fatal(err)
	}
	client := &rejectPauseClient{}
	if err = s.DispatchPending(ctx, client); err != nil {
		t.Fatal(err)
	}
	if client.pauseCalls != 0 || len(client.dispatched) != 1 || client.dispatched[0] != next.ID {
		t.Fatalf("pauseCalls=%d dispatched=%v", client.pauseCalls, client.dispatched)
	}
	var reason string
	if err = s.Pool.QueryRow(ctx, "SELECT failed_reason FROM raptor.outbox WHERE id=$1", pauseID).Scan(&reason); err != nil || reason != "superseded terminal pause" {
		t.Fatalf("pause reason=%q err=%v", reason, err)
	}
}
