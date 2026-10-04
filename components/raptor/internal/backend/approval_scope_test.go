package backend

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/db"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/domain"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/testutil"
	"net/http/httptest"
	"testing"
)

func TestApprovalRESTRejectsForeignEnvironment(t *testing.T) {
	ctx := context.Background()
	p := testutil.Database(t)
	if e := db.Migrate(ctx, p); e != nil {
		t.Fatal(e)
	}
	h := New(p)
	h.RegisterService("fixture-service", "fixture-password")
	id := domain.NewID()
	if _, e := p.Exec(ctx, `INSERT INTO raptor.requests(id,creator_id,idempotency_key,input_hash,definition,schema_hash) VALUES($1,$2,'rest','hash','{"type":"agent","envCode":"adev"}','schema')`, id, domain.NewID()); e != nil {
		t.Fatal(e)
	}
	body, _ := json.Marshal(map[string]any{"requestId": id, "actionId": domain.NewID(), "interface": "ess.scale-in", "envCode": "bdev", "target": map[string]string{"groupId": "synthetic"}, "parameters": map[string]any{}})
	r := httptest.NewRequest("POST", "/v1/requests/"+id+"/approvals", bytes.NewReader(body))
	r.SetBasicAuth("fixture-service", "fixture-password")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 400 {
		t.Fatal("REST scope bypass", w.Code, w.Body.String())
	}
	var n int
	p.QueryRow(ctx, "SELECT count(*) FROM raptor.approvals").Scan(&n)
	if n != 0 {
		t.Fatal("invalid REST binding persisted")
	}
}
