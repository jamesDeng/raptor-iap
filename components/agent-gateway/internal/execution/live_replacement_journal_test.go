package execution

import (
	"context"
	"encoding/json"
	"fmt"
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

func TestReplacementJournalStoresBothContractVersions(t *testing.T) {
	for _, version := range []int{1, 2} {
		t.Run(fmt.Sprint(version), func(t *testing.T) {
			s := testStore(t)
			ctx := context.Background()
			id := NewID()
			if _, err := s.Receive(ctx, id); err != nil {
				t.Fatal(err)
			}
			x, err := s.ClaimNext(ctx, "owner")
			if err != nil || x == nil {
				t.Fatal(err)
			}
			b, r := directHarnessFixture(t)
			if version == 2 {
				b.ExecutionMode = "replacement"
			}
			b.RequestID = id
			b.AttemptID = x.AttemptID
			r.RequestID = id
			r.AttemptID = x.AttemptID
			if version == 1 {
				r.Replacement.Version = 1
				r.Replacement.RestartGeneration = 0
				r.Replacement.Actions = []ReplacementScaleAction{{ActionID: "expand", Previous: 2, Desired: 4, Outcome: "submitted"}, {ActionID: "shrink-a", Previous: 4, Desired: 3, Outcome: "submitted"}, {ActionID: "shrink-b", Previous: 3, Desired: 2, Outcome: "unknown"}}
			}
			if err = s.BindLive(ctx, x.AttemptID, "owner", b, b.DefinitionSHA256); err != nil {
				t.Fatal(err)
			}
			if version == 1 {
				if err = s.SaveLiveResult(ctx, x.AttemptID, "owner", r); err == nil {
					t.Fatal("legacy result authorized without issued proof")
				}
				// Historical rows remain readable, but cannot authorize new work.
				raw, _ := json.Marshal(r)
				if _, err = s.Pool.Exec(ctx, "UPDATE gateway.executions SET result=$2 WHERE request_id=$1", id, raw); err != nil {
					t.Fatal(err)
				}
				got, err := s.Get(ctx, id)
				if err != nil || got.Result == nil || got.Result.Replacement.Version != 1 {
					t.Fatal("historical result not readable", err)
				}
				return
			}
			r.Replacement.Scope = resourceTestScope(b)
			if err = s.SaveIssuedScope(ctx, x.AttemptID, "owner", r.Replacement.Scope); err != nil {
				t.Fatal(err)
			}
			if err = s.SaveLiveResult(ctx, x.AttemptID, "owner", r); err != nil {
				t.Fatal(err)
			}
			if err = s.FinalizeLive(ctx, x.AttemptID, "owner", LiveOutcome{Status: "completed", Checkpoint: verifiedCheckpoint(), SandboxAbsent: true, KeyAbsent: true, AccessRevoked: true}); err != nil {
				t.Fatal(err)
			}
			got, err := s.Get(ctx, id)
			if err != nil || got.Result == nil || got.Result.Replacement == nil || got.Result.Replacement.Version != version || got.Result.Replacement.RestartGeneration != r.Replacement.RestartGeneration {
				t.Fatalf("contract lost: %+v %v", got, err)
			}
		})
	}
}
