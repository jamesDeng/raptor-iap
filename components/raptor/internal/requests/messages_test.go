package requests

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/agentaccess"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/db"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/domain"
	"strings"
	"testing"
	"time"
)

type messageGateway struct {
	capability ConversationCapability
	fail       bool
	delivered  []MessageReceipt
}

func (g *messageGateway) Execution(context.Context, string) (map[string]any, error) {
	return nil, domain.ErrUnavailable
}
func (g *messageGateway) Progress(context.Context, string, int64) ([]GatewayEvent, error) {
	return nil, nil
}
func (g *messageGateway) Conversation(context.Context, string) (ConversationCapability, error) {
	return g.capability, nil
}
func (g *messageGateway) PutRequest(context.Context, string) error { return nil }
func (g *messageGateway) PutMessage(_ context.Context, _ string, p json.RawMessage) (MessageReceipt, error) {
	if g.fail {
		return MessageReceipt{}, domain.ErrUnavailable
	}
	var in MessageInput
	json.Unmarshal(p, &in)
	r := MessageReceipt{MessageID: in.MessageID, InputSequence: in.InputSequence, Status: "queued", AcceptedAt: in.AcceptedAt}
	g.delivered = append(g.delivered, r)
	return r, nil
}
func messageSetup(t *testing.T) (*Service, domain.Request, domain.User, *messageGateway) {
	s, _, u := setup(t)
	ctx := context.Background()
	id := domain.NewID()
	raw, _ := json.Marshal(questionInput(t, "healthy?", "gpt-5.6-luna"))
	_, e := s.Pool.Exec(ctx, `INSERT INTO raptor.requests(id,creator_id,idempotency_key,input_hash,definition,schema_hash,status) VALUES($1,$2,'message-fixture','fixture',$3,'fixture','queued')`, id, u.ID, raw)
	if e != nil {
		t.Fatal(e)
	}
	g := &messageGateway{capability: ConversationCapability{Version: 1, Enabled: true, CanContinue: true}}
	s.Gateway = g
	s.ConversationEnabled = true
	r, e := s.Get(ctx, id)
	if e != nil {
		t.Fatal(e)
	}
	return s, r, u, g
}
func TestMessageAdmissionAuthorizationAndBounds(t *testing.T) {
	s, r, u, g := messageSetup(t)
	ctx := context.Background()
	if _, e := s.SubmitMessage(ctx, domain.User{ID: domain.NewID(), Role: "user"}, r.ID, domain.NewID(), "hi"); !errors.Is(e, domain.ErrForbidden) {
		t.Fatal(e)
	}
	for _, text := range []string{" ", strings.Repeat("x", 2001), string([]byte{255})} {
		if _, e := s.SubmitMessage(ctx, u, r.ID, domain.NewID(), text); !errors.Is(e, domain.ErrInvalid) {
			t.Fatal(e)
		}
	}
	text := strings.Repeat("😀", 2000)
	v, e := s.SubmitMessage(ctx, u, r.ID, domain.NewID(), text)
	if e != nil || v.Status != "accepted" {
		t.Fatal(e, v)
	}
	rows, e := s.ListMessages(ctx, r.ID, 0)
	if e != nil || len(rows) != 1 || rows[0].Text != "[Awaiting input validation]" {
		t.Fatal(e)
	}
	g.capability.Version = 0
	if _, e = s.SubmitMessage(ctx, u, r.ID, domain.NewID(), "hi"); !errors.Is(e, domain.ErrUnavailable) {
		t.Fatal(e)
	}
	s.ConversationEnabled = false
	if _, e = s.SubmitMessage(ctx, u, r.ID, domain.NewID(), "hi"); !errors.Is(e, domain.ErrForbidden) {
		t.Fatal(e)
	}
}
func TestMessageLostAckRetryAfterCompletion(t *testing.T) {
	s, r, u, g := messageSetup(t)
	ctx := context.Background()
	id := domain.NewID()
	a, e := s.SubmitMessage(ctx, u, r.ID, id, "literal /skill:foo")
	if e != nil {
		t.Fatal(e)
	}
	s.Pool.Exec(ctx, "UPDATE raptor.requests SET status='completed' WHERE id=$1", r.ID)
	g.capability.CanContinue = false
	b, e := s.SubmitMessage(ctx, u, r.ID, id, "literal /skill:foo")
	if e != nil || a.InputSequence != b.InputSequence {
		t.Fatal(e)
	}
	if _, e = s.SubmitMessage(ctx, u, r.ID, id, "changed"); !errors.Is(e, domain.ErrConflict) {
		t.Fatal(e)
	}
	if _, e = s.SubmitMessage(ctx, domain.User{ID: domain.NewID(), Role: "admin"}, r.ID, id, "literal /skill:foo"); !errors.Is(e, domain.ErrConflict) {
		t.Fatal(e)
	}
	if _, e = s.SubmitMessage(ctx, u, r.ID, domain.NewID(), "again"); !errors.Is(e, domain.ErrConflict) {
		t.Fatal(e)
	}
}
func TestMessageCapacityAndSequence(t *testing.T) {
	s, r, u, _ := messageSetup(t)
	ctx := context.Background()
	for i := int64(1); i <= 20; i++ {
		v, e := s.SubmitMessage(ctx, u, r.ID, domain.NewID(), "hi")
		if e != nil || v.InputSequence != i {
			t.Fatal(e, v)
		}
	}
	if _, e := s.SubmitMessage(ctx, u, r.ID, domain.NewID(), "full"); !errors.Is(e, ErrMessageCapacity) {
		t.Fatal(e)
	}
	s.Pool.Exec(ctx, "UPDATE raptor.request_messages SET status='answered' WHERE request_id=$1 AND input_sequence=1", r.ID)
	v, e := s.SubmitMessage(ctx, u, r.ID, domain.NewID(), "now allowed")
	if e != nil || v.InputSequence != 21 {
		t.Fatal(e, v)
	}
}
func TestMessageOutboxAtomicity(t *testing.T) {
	s, r, u, g := messageSetup(t)
	ctx := context.Background()
	_, e := s.Pool.Exec(ctx, `CREATE FUNCTION raptor.reject_message_outbox() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.topic='message' THEN RAISE EXCEPTION 'fixture'; END IF; RETURN NEW; END $$; CREATE TRIGGER reject_message_outbox BEFORE INSERT ON raptor.outbox FOR EACH ROW EXECUTE FUNCTION raptor.reject_message_outbox()`)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.SubmitMessage(ctx, u, r.ID, domain.NewID(), "rollback"); e == nil {
		t.Fatal("outbox failure accepted")
	}
	rows, _ := s.ListMessages(ctx, r.ID, 0)
	if len(rows) != 0 {
		t.Fatal("orphan message")
	}
	s.Pool.Exec(ctx, "DROP TRIGGER reject_message_outbox ON raptor.outbox")
	v, e := s.SubmitMessage(ctx, u, r.ID, domain.NewID(), "persist")
	if e != nil {
		t.Fatal(e)
	}
	g.fail = true
	if e = s.DispatchPending(ctx, g); e == nil {
		t.Fatal("failure swallowed")
	}
	g.fail = false
	if e = s.DispatchPending(ctx, g); e != nil {
		t.Fatal(e)
	}
	rows, e = s.ListMessages(ctx, r.ID, 0)
	if e != nil || len(rows) != 1 || rows[0].Status != "queued" || rows[0].MessageID != v.MessageID {
		t.Fatal(e, rows)
	}
	if e = db.Migrate(ctx, s.Pool); e != nil {
		t.Fatal(e)
	}
	var ok bool
	s.Pool.QueryRow(ctx, "SELECT has_table_privilege('raptor_app','raptor.request_messages','SELECT,INSERT,UPDATE')").Scan(&ok)
	if !ok {
		t.Fatal("missing privilege")
	}
}
func TestMessagePublicHistoryRedaction(t *testing.T) {
	s, r, u, _ := messageSetup(t)
	ctx := context.Background()
	text := "password=my-fixture-secret"
	v, e := s.SubmitMessage(ctx, u, r.ID, domain.NewID(), text)
	if e != nil {
		t.Fatal(e)
	}
	rows, e := s.ListMessages(ctx, r.ID, 0)
	if e != nil || len(rows) != 1 || strings.Contains(rows[0].Text, "my-fixture-secret") {
		t.Fatal("public admission leaked sensitive input")
	}
	tx, e := s.Pool.Begin(ctx)
	if e != nil {
		t.Fatal(e)
	}
	if e = applyMessageReceipt(ctx, tx, r.ID, MessageReceipt{MessageID: v.MessageID, InputSequence: v.InputSequence, Status: "rejected", Reason: "SensitiveInput"}); e != nil {
		t.Fatal(e)
	}
	tx.Commit(ctx)
	rows, _ = s.ListMessages(ctx, r.ID, 0)
	if strings.Contains(rows[0].Text, "my-fixture-secret") {
		t.Fatal("rejected history leaked sensitive input")
	}
	if _, e = s.SubmitMessage(ctx, u, r.ID, v.MessageID, text); e != nil {
		t.Fatal("private immutable retry changed", e)
	}
}

func TestCompletedMessageReopensForFreshAttemptAccess(t *testing.T) {
	s, r, u, _ := messageSetup(t)
	ctx := context.Background()
	r.Definition.EnvCode = "adev"
	r.Definition.Skills = domain.SkillsVersion{Tag: "skills-v0.1.0", CommitSHA: strings.Repeat("a", 40)}
	body, _ := json.Marshal(r.Definition)
	if _, e := s.Pool.Exec(ctx, "UPDATE raptor.requests SET definition=$2 WHERE id=$1", r.ID, body); e != nil {
		t.Fatal(e)
	}
	old := map[string]any{"requestId": r.ID, "attemptId": "53bd8d1d-9bd5-42f1-af0c-bd2a6717f802", "status": "completed", "updatedAt": "2026-10-09T11:00:00Z"}
	if _, _, _, e := s.syncExecution(ctx, r, old); e != nil {
		t.Fatal(e)
	}
	if _, e := s.SubmitMessage(ctx, u, r.ID, domain.NewID(), "read fresh state"); e != nil {
		t.Fatal(e)
	}
	reopened, e := s.Get(ctx, r.ID)
	if e != nil || reopened.Status != "queued" {
		t.Fatalf("continuation is not queued: %s %v", reopened.Status, e)
	}
	if _, _, _, e = s.syncExecution(ctx, reopened, old); e != nil {
		t.Fatal(e)
	}
	reopened, e = s.Get(ctx, r.ID)
	if e != nil || reopened.Status != "queued" {
		t.Fatalf("old snapshot closed continuation: %s %v", reopened.Status, e)
	}
	// Receipts may reach the timeline before the first new execution snapshot.
	if _, e = s.Pool.Exec(ctx, "UPDATE raptor.request_messages SET status='answered' WHERE request_id=$1", r.ID); e != nil {
		t.Fatal(e)
	}
	if _, _, _, e = s.syncExecution(ctx, reopened, old); e != nil {
		t.Fatal(e)
	}
	reopened, e = s.Get(ctx, r.ID)
	if e != nil || reopened.Status != "queued" {
		t.Fatal("answered receipt lifted the old-attempt fence", reopened.Status, e)
	}
	access := agentaccess.Service{Pool: s.Pool, RaptorMCPURL: "https://raptor.fixture/mcp", InfraMCPURL: "https://infra.fixture/mcp"}
	hash, _ := agentaccess.Fingerprint(r.Definition)
	attempt := domain.NewID()
	issued, e := access.Issue(ctx, r.ID, agentaccess.IssueInput{AttemptID: attempt, DefinitionSHA256: hash, ExpiresAt: time.Now().Add(time.Minute)})
	if e != nil || issued.Binding.AttemptID != attempt {
		t.Fatalf("fresh attempt access denied: %v", e)
	}
	checked, e := access.Check(ctx, agentaccess.CheckInput{Credential: issued.Credential, Audience: "raptor", Tool: "request_get", Selectors: map[string]string{"requestId": r.ID}})
	if e != nil || !checked.Active {
		t.Fatalf("fresh access inactive: %v", e)
	}
	next := map[string]any{"requestId": r.ID, "attemptId": attempt, "status": "running", "updatedAt": "2026-10-09T11:01:00Z"}
	if _, _, _, e = s.syncExecution(ctx, reopened, next); e != nil {
		t.Fatal(e)
	}
	next["status"] = "completed"
	next["updatedAt"] = "2026-10-09T11:02:00Z"
	if _, _, _, e = s.syncExecution(ctx, reopened, next); e != nil {
		t.Fatal(e)
	}
	checked, e = access.Check(ctx, agentaccess.CheckInput{Credential: issued.Credential, Audience: "raptor"})
	if e != nil || checked.Active {
		t.Fatal("terminal continuation access remained active", e)
	}
}

func TestRejectedContinuationRestoresPreviousCompletion(t *testing.T) {
	s, r, u, _ := messageSetup(t)
	ctx := context.Background()
	old := map[string]any{"requestId": r.ID, "attemptId": "53bd8d1d-9bd5-42f1-af0c-bd2a6717f802", "status": "completed", "updatedAt": "2026-10-09T11:00:00Z"}
	if _, _, _, e := s.syncExecution(ctx, r, old); e != nil {
		t.Fatal(e)
	}
	receipt, e := s.SubmitMessage(ctx, u, r.ID, domain.NewID(), "read state")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.Pool.Exec(ctx, "UPDATE raptor.request_messages SET status='rejected' WHERE message_id=$1", receipt.MessageID); e != nil {
		t.Fatal(e)
	}
	if _, _, _, e = s.syncExecution(ctx, r, old); e != nil {
		t.Fatal(e)
	}
	restored, e := s.Get(ctx, r.ID)
	if e != nil || restored.Status != "completed" {
		t.Fatal("rejected input left request queued", restored.Status, e)
	}
}
