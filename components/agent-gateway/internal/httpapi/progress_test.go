package httpapi

import (
	"context"
	"encoding/json"
	"github.com/gorilla/websocket"
	"github.com/jamesDeng/raptor-iap/components/agent-gateway/internal/db"
	"github.com/jamesDeng/raptor-iap/components/agent-gateway/internal/execution"
	"github.com/jamesDeng/raptor-iap/components/agent-gateway/internal/testutil"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestProgressTicketSingleUseBoundToRequestAndOrigin(t *testing.T) {
	p := testutil.Database(t)
	ctx := context.Background()
	if e := db.Migrate(ctx, p); e != nil {
		t.Fatal(e)
	}
	s := &execution.Store{Pool: p}
	id := execution.NewID()
	if _, e := s.Receive(ctx, id); e != nil {
		t.Fatal(e)
	}
	g := newProgress(s, ProgressConfig{URL: "ws://127.0.0.1:8874/v1/progress", Origins: []string{"http://127.0.0.1:3790"}})
	ticket, e := g.issue(ctx, id, "user", "http://127.0.0.1:3790")
	if e != nil {
		t.Fatal(e)
	}
	if g.consume(ctx, ticket.Token, execution.NewID(), "http://127.0.0.1:3790") == nil {
		t.Fatal("wrong request accepted")
	}
	if g.consume(ctx, ticket.Token, id, "http://evil.example") == nil {
		t.Fatal("wrong origin accepted")
	}
	if e = g.consume(ctx, ticket.Token, id, "http://127.0.0.1:3790"); e != nil {
		t.Fatal(e)
	}
	if g.consume(ctx, ticket.Token, id, "http://127.0.0.1:3790") == nil {
		t.Fatal("ticket reused")
	}
	ticket, e = g.issue(ctx, id, "user", "http://127.0.0.1:3790")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = p.Exec(ctx, "UPDATE gateway.progress_tickets SET expires_at=now()-interval '1 second'"); e != nil {
		t.Fatal(e)
	}
	if g.consume(ctx, ticket.Token, id, "http://127.0.0.1:3790") == nil {
		t.Fatal("expired ticket accepted")
	}
}

func TestWebSocketReplaysAndStreamsWithoutBackendCredentials(t *testing.T) {
	p := testutil.Database(t)
	ctx := context.Background()
	if e := db.Migrate(ctx, p); e != nil {
		t.Fatal(e)
	}
	s := &execution.Store{Pool: p}
	id := execution.NewID()
	s.Receive(ctx, id)
	for i := 0; i < 103; i++ {
		if e := s.AppendEvent(ctx, execution.ProgressEvent{RequestID: id, EventID: execution.NewID(), Kind: "progress", Summary: "recorded", EvidenceMode: "simulated"}); e != nil {
			t.Fatal(e)
		}
	}
	cfg := ProgressConfig{URL: "ws://127.0.0.1:8874/v1/progress", Origins: []string{"http://localhost:3790"}}
	g := newProgress(s, cfg)
	ticket, e := g.issue(ctx, id, "user", "http://localhost:3790")
	if e != nil {
		t.Fatal(e)
	}
	server := httptest.NewServer(New(s, "service", "private", cfg))
	defer server.Close()
	headers := http.Header{"Origin": []string{"http://localhost:3790"}}
	if c, _, e := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http")+"/v1/progress", http.Header{"Origin": []string{"http://evil.example"}}); e == nil {
		c.Close()
		t.Fatal("wrong origin accepted")
	}
	c, _, e := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http")+"/v1/progress", headers)
	if e != nil {
		t.Fatal(e)
	}
	defer c.Close()
	c.SetReadDeadline(time.Now().Add(5 * time.Second))
	c.WriteJSON(map[string]any{"type": "subscribe", "token": ticket.Token, "requestId": id, "afterSequence": 1})
	for n := int64(2); n <= 103; n++ {
		var frame struct {
			Type  string                  `json:"type"`
			Event execution.ProgressEvent `json:"event"`
		}
		if e = c.ReadJSON(&frame); e != nil {
			t.Fatal(e)
		}
		if frame.Type != "event" || frame.Event.Sequence != n {
			t.Fatalf("missing/out-of-order event: %+v", frame)
		}
	}
	var snapshot map[string]json.RawMessage
	if e = c.ReadJSON(&snapshot); e != nil {
		t.Fatal(e)
	}
	if strings.Contains(string(snapshot["execution"]), `"input"`) || strings.Contains(string(snapshot["execution"]), `"checkpoint"`) {
		t.Fatal("backend state leaked")
	}
	if e = s.AppendEvent(ctx, execution.ProgressEvent{RequestID: id, EventID: execution.NewID(), Kind: "progress", Summary: "new live event", EvidenceMode: "simulated"}); e != nil {
		t.Fatal(e)
	}
	var next struct {
		Type  string                  `json:"type"`
		Event execution.ProgressEvent `json:"event"`
	}
	if e = c.ReadJSON(&next); e != nil {
		t.Fatal(e)
	}
	if next.Event.Sequence != 104 {
		t.Fatal(next)
	}
}
