package execution

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func terminalQualification(t *testing.T, clean bool) (*Store, *Execution) {
	t.Helper()
	s, x, b := liveAttempt(t)
	ctx := context.Background()
	b.ExecutionMode = "qualification"
	b.Operation = "db-proxy.replace-nodes"
	b.ObjectKind = "db-proxy"
	b.ClusterID = "cluster"
	b.DefinitionSHA256 = strings.Repeat("a", 64)
	b.ProviderID, b.ConnectionVersion, b.Model = "codex", 1, "gpt-6-luna"
	raw, _ := json.Marshal(b)
	proof := `{"sandboxAbsent":true,"keyAbsent":true,"accessRevoked":true}`
	if !clean {
		proof = `{"sandboxAbsent":false,"keyAbsent":true,"accessRevoked":true}`
	}
	if _, e := s.Pool.Exec(ctx, "UPDATE gateway.attempts SET binding=$2,state='failed',ended_at=now() WHERE id=$1", x.AttemptID, raw); e != nil {
		t.Fatal(e)
	}
	if _, e := s.Pool.Exec(ctx, "UPDATE gateway.executions SET status='failed',runtime_mode='live',cleanup_status=$2,recovery_needed=$3,failure_code='Interrupted' WHERE request_id=$1", x.RequestID, proof, !clean); e != nil {
		t.Fatal(e)
	}
	if clean {
		if _, e := s.Pool.Exec(ctx, "UPDATE gateway.runtime_slot SET request_id=NULL,owner=NULL,unresolved=false WHERE id=1"); e != nil {
			t.Fatal(e)
		}
	}
	return s, x
}

func TestTerminalQualificationRetryRequiresCleanMatchingAttempt(t *testing.T) {
	for _, clean := range []bool{false, true} {
		s, x := terminalQualification(t, clean)
		ctx := context.Background()
		old := NewID()
		if e := s.DeliverSignal(ctx, Signal{ID: old, RequestID: x.RequestID, Kind: "continue", Payload: json.RawMessage(`{"instructions":"old click"}`)}); e != nil {
			t.Fatal(e)
		}
		stale := NewID()
		if e := s.DeliverSignal(ctx, Signal{ID: stale, RequestID: x.RequestID, Kind: "continue", Payload: json.RawMessage(`{"attemptId":"00000000-0000-0000-0000-000000000000"}`)}); e != nil {
			t.Fatal(e)
		}
		state, _ := s.Get(ctx, x.RequestID)
		if state.Status != "failed" {
			t.Fatal("stale signal retried", state.Status)
		}
		id := NewID()
		payload, _ := json.Marshal(map[string]string{"attemptId": x.AttemptID, "instructions": "retry"})
		if e := s.DeliverSignal(ctx, Signal{ID: id, RequestID: x.RequestID, Kind: "continue", Payload: payload}); e != nil {
			t.Fatal(e)
		}
		state, _ = s.Get(ctx, x.RequestID)
		if clean && state.Status != "queued" || !clean && state.Status != "failed" {
			t.Fatal("unsafe terminal retry state", clean, state.Status)
		}
		var pending int
		if e := s.Pool.QueryRow(ctx, "SELECT count(*) FROM gateway.signals WHERE request_id=$1 AND kind='continue' AND NOT consumed AND payload->>'approvalId' IS NULL", x.RequestID).Scan(&pending); e != nil || pending != 0 {
			t.Fatal("terminal signals left to replay", e, pending)
		}
		if !clean {
			continue
		}
		next, e := s.ClaimLiveNext(ctx, "next-owner")
		if e != nil || next == nil || next.AttemptID == x.AttemptID {
			t.Fatal("fresh attempt not claimed", e, next)
		}
		var oldBinding []byte
		if e = s.Pool.QueryRow(ctx, "SELECT binding FROM gateway.attempts WHERE id=$1", x.AttemptID).Scan(&oldBinding); e != nil {
			t.Fatal(e)
		}
		var b AttemptBinding
		if e = json.Unmarshal(oldBinding, &b); e != nil {
			t.Fatal(e)
		}
		b.AttemptID = next.AttemptID
		newBinding, _ := json.Marshal(b)
		if _, e = s.Pool.Exec(ctx, "UPDATE gateway.attempts SET binding=$2 WHERE id=$1", next.AttemptID, newBinding); e != nil {
			t.Fatal(e)
		}
		if e = s.DeliverSignal(ctx, Signal{ID: NewID(), RequestID: x.RequestID, Kind: "continue", Payload: payload}); e != nil {
			t.Fatal(e)
		}
		state, _ = s.Get(ctx, x.RequestID)
		if state.Status != "running" {
			t.Fatal("duplicate interrupted fresh attempt", state.Status)
		}
		if _, e = s.Pool.Exec(ctx, "UPDATE gateway.executions SET status='failed',cleanup_status='{\"sandboxAbsent\":true,\"keyAbsent\":true,\"accessRevoked\":true}' WHERE request_id=$1", x.RequestID); e != nil {
			t.Fatal(e)
		}
		if _, e = s.Pool.Exec(ctx, "UPDATE gateway.runtime_slot SET request_id=NULL,owner=NULL,unresolved=false WHERE id=1"); e != nil {
			t.Fatal(e)
		}
		if e = s.DeliverSignal(ctx, Signal{ID: NewID(), RequestID: x.RequestID, Kind: "continue", Payload: payload}); e != nil {
			t.Fatal(e)
		}
		state, _ = s.Get(ctx, x.RequestID)
		if state.Status != "failed" {
			t.Fatal("old click retried later attempt", state.Status)
		}
	}
}

func TestTerminalReplacementIsNotRetriedByQualificationContinue(t *testing.T) {
	s, x := terminalQualification(t, true)
	ctx := context.Background()
	var raw []byte
	if e := s.Pool.QueryRow(ctx, "SELECT binding FROM gateway.attempts WHERE id=$1", x.AttemptID).Scan(&raw); e != nil {
		t.Fatal(e)
	}
	var b AttemptBinding
	if e := json.Unmarshal(raw, &b); e != nil {
		t.Fatal(e)
	}
	b.ExecutionMode = "replacement"
	b.ProviderID, b.ConnectionVersion, b.Model = "", 0, "gpt-5.6-luna"
	raw, _ = json.Marshal(b)
	if _, e := s.Pool.Exec(ctx, "UPDATE gateway.attempts SET binding=$2 WHERE id=$1", x.AttemptID, raw); e != nil {
		t.Fatal(e)
	}
	payload, _ := json.Marshal(map[string]string{"attemptId": x.AttemptID, "instructions": "retry"})
	if e := s.DeliverSignal(ctx, Signal{ID: NewID(), RequestID: x.RequestID, Kind: "continue", Payload: payload}); e != nil {
		t.Fatal(e)
	}
	state, e := s.Get(ctx, x.RequestID)
	if e != nil || state.Status != "failed" {
		t.Fatal("replacement was queued", e, state.Status)
	}
}
