package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/jamesDeng/raptor-iap/components/agent-gateway/internal/db"
	"github.com/jamesDeng/raptor-iap/components/agent-gateway/internal/execution"
	"github.com/jamesDeng/raptor-iap/components/agent-gateway/internal/testutil"
	"net/http/httptest"
	"testing"
	"time"
)

func TestConversationServiceAuthAndBinding(t *testing.T) {
	p := testutil.Database(t)
	if e := db.Migrate(context.Background(), p); e != nil {
		t.Fatal(e)
	}
	s := &execution.Store{Pool: p, RuntimeMode: "live", ConversationEnabled: true, ConversationRuntime: true}
	id := execution.NewID()
	s.Receive(context.Background(), id)
	h := New(s, "service", "private")
	in := execution.ConversationInput{RequestID: id, MessageID: execution.NewID(), ActorID: execution.NewID(), InputSequence: 1, Text: "hi", AcceptedAt: time.Now()}
	raw, _ := json.Marshal(in)
	for _, tc := range []struct {
		auth bool
		id   string
		want int
	}{{false, id, 401}, {true, execution.NewID(), 400}, {true, id, 200}} {
		req := httptest.NewRequest("POST", "/v1/requests/"+tc.id+"/messages", bytes.NewReader(raw))
		if tc.auth {
			req.SetBasicAuth("service", "private")
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		if w.Code != tc.want {
			t.Fatal(w.Code, tc.want)
		}
	}
}
