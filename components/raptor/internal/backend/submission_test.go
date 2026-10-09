package backend

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/approvals"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/db"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/domain"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/testutil"
	"net/http/httptest"
	"testing"
)

func TestSubmissionServiceRoutes(t *testing.T) {
	ctx := context.Background()
	p := testutil.Database(t)
	if e := db.Migrate(ctx, p); e != nil {
		t.Fatal(e)
	}
	h := New(p)
	h.RegisterService("service", "private")
	id := domain.NewID()
	u := domain.User{ID: domain.NewID(), Role: "user"}
	if _, e := p.Exec(ctx, `INSERT INTO raptor.requests(id,creator_id,idempotency_key,input_hash,definition,schema_hash) VALUES($1,$2,'route','hash','{"type":"agent","envCode":"rdev.ali"}','schema')`, id, u.ID); e != nil {
		t.Fatal(e)
	}
	in := approvals.ApprovalInput{RequestID: id, ActionID: domain.NewID(), Interface: "ess.scale-in", EnvCode: "rdev.ali", Target: map[string]any{"proxyCode": "p", "groupId": "g"}, Parameters: map[string]any{"desiredCapacity": 3}}
	a, e := h.Approvals.Request(ctx, in)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = h.Approvals.Decide(ctx, u, a.ID, approvals.DecisionInput{Decision: "approve"}); e != nil {
		t.Fatal(e)
	}
	call := func(path string, in any, auth bool) *httptest.ResponseRecorder {
		b, _ := json.Marshal(in)
		r := httptest.NewRequest("POST", path, bytes.NewReader(b))
		if auth {
			r.SetBasicAuth("service", "private")
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	if w := call("/v1/approval-claim", in, false); w.Code != 401 {
		t.Fatal("claim without service auth", w.Code)
	}
	rec := approvals.SubmissionRecordInput{Binding: in, Outcome: "submitted", ProviderRequestID: "ack"}
	if w := call("/v1/approval-record", rec, false); w.Code != 401 {
		t.Fatal("record without service auth", w.Code)
	}
	for i := 0; i < 2; i++ {
		w := call("/v1/approval-claim", in, true)
		var out struct {
			Data struct {
				Claimed bool `json:"claimed"`
			} `json:"data"`
		}
		if e := json.Unmarshal(w.Body.Bytes(), &out); e != nil || w.Code != 200 || out.Data.Claimed != (i == 0) {
			t.Fatal("claim route", w.Code, w.Body.String())
		}
	}
	if w := call("/v1/approval-record", rec, true); w.Code != 200 {
		t.Fatal("record route", w.Code)
	}
	rec.ProviderRequestID = "different"
	if w := call("/v1/approval-record", rec, true); w.Code != 409 {
		t.Fatal("conflicting record", w.Code)
	}
}
