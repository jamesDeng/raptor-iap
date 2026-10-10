package execution

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestReplacementJournalDoesNotBroadenQuestionAdmission(t *testing.T) {
	s := testStore(t)
	id := NewID()
	s.Receive(context.Background(), id)
	x, err := s.ClaimNext(context.Background(), "owner")
	if err != nil || x == nil {
		t.Fatal(err)
	}
	b := AttemptBinding{RequestID: id, AttemptID: x.AttemptID, ObjectCode: "proxy", EnvCode: "rdev.ali", SkillsCommit: strings.Repeat("b", 40), Model: "gpt-5.6-luna"}
	b.Operation = "db-proxy.replace-nodes"
	b.ObjectKind = "db-proxy"
	b.DefinitionSHA256 = strings.Repeat("a", 64)
	b.ClusterID = "cluster"
	if b.valid() {
		t.Fatal("question validator admits replacement")
	}
	if err := s.BindLive(context.Background(), x.AttemptID, "owner", b, b.DefinitionSHA256); err != nil {
		t.Fatalf("replacement journal rejected: %v", err)
	}
	if _, err := s.RecoveryRecord(context.Background(), x.AttemptID); err != nil {
		t.Fatal(err)
	}
	b.Operation = "db-proxy.destroy"
	if err := s.BindLive(context.Background(), x.AttemptID, "owner", b, b.DefinitionSHA256); err == nil {
		t.Fatal("unknown operation admitted")
	}
}

func TestReplacementWakeupsPersistBeforePauseAndQuestionRejectsThem(t *testing.T) {
	s, x, b := liveAttempt(t)
	ctx := context.Background()
	for _, kind := range []string{"approval", "continue", "block"} {
		if err := s.DeliverSignal(ctx, Signal{ID: NewID(), RequestID: b.RequestID, Kind: kind, Payload: []byte(`{}`)}); err == nil {
			t.Fatal("question accepted", kind)
		}
	}
	b.Operation = "db-proxy.replace-nodes"
	b.ObjectKind = "db-proxy"
	b.ClusterID = "cluster"
	b.DefinitionSHA256 = strings.Repeat("a", 64)
	raw, _ := json.Marshal(b)
	if _, err := s.Pool.Exec(ctx, "UPDATE gateway.attempts SET binding=$2 WHERE id=$1", x.AttemptID, raw); err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"approval", "continue", "block"} {
		id := NewID()
		v := Signal{ID: id, RequestID: b.RequestID, Kind: kind, Payload: []byte(`{}`)}
		if err := s.DeliverSignal(ctx, v); err != nil {
			t.Fatalf("early replacement %s rejected: %v", kind, err)
		}
		if err := s.DeliverSignal(ctx, v); err != nil {
			t.Fatal(err)
		}
		var n int
		s.Pool.QueryRow(ctx, "SELECT count(*) FROM gateway.signals WHERE id=$1 AND NOT consumed", id).Scan(&n)
		if n != 1 {
			t.Fatal("event lost or consumed early")
		}
	}
	if err := s.DeliverSignal(ctx, Signal{ID: NewID(), RequestID: b.RequestID, Kind: "skills", Payload: []byte(`{}`)}); err == nil {
		t.Fatal("unsupported live signal admitted")
	}
}
func TestReplacementProgressDoesNotBroadenQuestionCatalog(t *testing.T) {
	s, x, b := liveAttempt(t)
	ctx := context.Background()
	event := RuntimeEvent{RuntimeSequence: 1, Kind: "tool_start", Tool: "mcp__raptor__approval_request", Outcome: "started", OccurredAt: time.Now().UTC()}
	if e := s.AppendRuntimeEvents(ctx, x.AttemptID, "owner", []RuntimeEvent{event}); e == nil {
		t.Fatal("question mutation progress accepted")
	}
	b.Operation = "db-proxy.replace-nodes"
	b.ObjectKind = "db-proxy"
	b.ClusterID = "cluster"
	b.DefinitionSHA256 = strings.Repeat("a", 64)
	raw, _ := json.Marshal(b)
	s.Pool.Exec(ctx, "UPDATE gateway.attempts SET binding=$2 WHERE id=$1", x.AttemptID, raw)
	if e := s.AppendRuntimeEvents(ctx, x.AttemptID, "owner", []RuntimeEvent{event}); e != nil {
		t.Fatal("replacement progress rejected", e)
	}
	event.RuntimeSequence = 2
	event.Tool = "bash"
	if e := s.AppendRuntimeEvents(ctx, x.AttemptID, "owner", []RuntimeEvent{event}); e == nil {
		t.Fatal("unknown progress admitted")
	}
}

func TestObserverProgressIsReplacementOnly(t *testing.T) {
	for _, name := range []string{"cloud_read", "deployment_read", "metrics_read", "db_connection_probe"} {
		if !ProgressToolAllowed("db-proxy.replace-nodes", name) || ProgressToolAllowed("application.question", name) {
			t.Fatalf("unexpected observer catalog %s", name)
		}
	}
}
