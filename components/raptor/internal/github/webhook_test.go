package github

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/db"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/domain"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/testutil"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestReviewAndMergeDelivery(t *testing.T) {
	ctx := context.Background()
	p := testutil.Database(t)
	if e := db.Migrate(ctx, p); e != nil {
		t.Fatal(e)
	}
	s := &Service{Pool: p}
	id := domain.NewID()
	p.Exec(ctx, "INSERT INTO raptor.requests(id,creator_id,idempotency_key,input_hash,definition,schema_hash,status) VALUES($1,$2,'test','hash','{}','schema','waiting_review')", id, domain.NewID())
	sha := strings.Repeat("a", 40)
	if e := s.AttachPR(ctx, id, PRInput{Repository: "owner/repo", Number: 1, URL: "https://github.com/owner/repo/pull/1", HeadSHA: sha}); e != nil {
		t.Fatal(e)
	}
	body := func(action, state string, merged bool) []byte {
		b, _ := json.Marshal(map[string]any{"action": action, "repository": map[string]string{"full_name": "owner/repo"}, "pull_request": map[string]any{"number": 1, "merged": merged, "head": map[string]string{"sha": sha}}, "review": map[string]string{"state": state, "commit_id": sha}})
		return b
	}
	for _, v := range []struct {
		id, event, action, state string
		merged                   bool
	}{{"approved", "pull_request_review", "submitted", "approved", false}, {"comment", "issue_comment", "created", "", false}, {"review", "pull_request_review", "submitted", "changes_requested", false}, {"review", "pull_request_review", "submitted", "changes_requested", false}, {"merge", "pull_request", "closed", "", true}} {
		if e := s.HandleDelivery(ctx, v.id, v.event, body(v.action, v.state, v.merged)); e != nil {
			t.Fatal(e)
		}
	}
	var n int
	p.QueryRow(ctx, "SELECT count(*) FROM raptor.outbox WHERE topic='signal'").Scan(&n)
	if n != 2 {
		t.Fatalf("expected two durable wakeups, got %d", n)
	}
	p.Exec(ctx, "UPDATE raptor.requests SET status='cancelled' WHERE id=$1", id)
	s.HandleDelivery(ctx, "late", "pull_request", body("closed", "", true))
	p.QueryRow(ctx, "SELECT count(*) FROM raptor.outbox WHERE topic='signal'").Scan(&n)
	if n != 2 {
		t.Fatal("cancelled request resurrected")
	}
}
func TestWebhookSignature(t *testing.T) {
	s := &Service{}
	h := s.Handler("test-secret")
	r := httptest.NewRequest("POST", "/webhooks/github", bytes.NewBufferString(`{}`))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 401 {
		t.Fatal("bad signature accepted")
	}
	mac := hmac.New(sha256.New, []byte("test-secret"))
	mac.Write([]byte(`{}`))
	r = httptest.NewRequest("POST", "/webhooks/github", bytes.NewBufferString(`{}`))
	r.Header.Set("X-Hub-Signature-256", "sha256="+hex.EncodeToString(mac.Sum(nil)))
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 400 {
		t.Fatal("missing delivery identity accepted")
	}
}
