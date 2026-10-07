package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/jamesDeng/raptor-iap/components/agent-gateway/internal/db"
	"github.com/jamesDeng/raptor-iap/components/agent-gateway/internal/execution"
	"github.com/jamesDeng/raptor-iap/components/agent-gateway/internal/testutil"
	"net/http/httptest"
	"testing"
)

func TestEventCursorIsBoundedAndResumable(t *testing.T) {
	p := testutil.Database(t)
	ctx := context.Background()
	if e := db.Migrate(ctx, p); e != nil {
		t.Fatal(e)
	}
	s := &execution.Store{Pool: p}
	id := execution.NewID()
	s.Receive(ctx, id)
	for i := 0; i < 101; i++ {
		if e := s.AppendEvent(ctx, execution.ProgressEvent{RequestID: id, EventID: fmt.Sprint(i), Kind: "progress", Summary: "fixture", EvidenceMode: "simulated"}); e != nil {
			t.Fatal(e)
		}
	}
	h := New(s, "service", "test-password")
	after := int64(0)
	total := 0
	for page := 0; page < 2; page++ {
		r := httptest.NewRequest("GET", fmt.Sprintf("/v1/requests/%s/events?after=%d", id, after), nil)
		r.SetBasicAuth("service", "test-password")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		var v struct {
			Data      []execution.ProgressEvent `json:"data"`
			NextAfter int64                     `json:"nextAfter"`
			HasMore   bool                      `json:"hasMore"`
		}
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &v) != nil {
			t.Fatal(w.Code, w.Body.String())
		}
		if page == 0 && (len(v.Data) != 100 || !v.HasMore || v.NextAfter != 100) {
			t.Fatal("first page not bounded", len(v.Data), v.NextAfter, v.HasMore)
		}
		if page == 1 && (len(v.Data) != 1 || v.HasMore || v.NextAfter != 101) {
			t.Fatal("second page missing", len(v.Data), v.NextAfter)
		}
		after = v.NextAfter
		total += len(v.Data)
	}
	if total != 101 {
		t.Fatal(total)
	}
}
func TestInvalidCursorIsSanitized(t *testing.T) {
	h := New(nil, "service", "test-password")
	for _, cursor := range []string{"-1", "wrong", "9223372036854775808"} {
		r := httptest.NewRequest("GET", "/v1/requests/id/events?after="+cursor, nil)
		r.SetBasicAuth("service", "test-password")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != 400 || w.Body.String() != "{\"error\":\"InvalidInput\"}\n" {
			t.Fatal(w.Code, w.Body.String())
		}
	}
}
