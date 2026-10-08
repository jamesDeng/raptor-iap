package observe

import (
	"github.com/jamesDeng/raptor-iap/components/test-client/internal/traffic"
	"strings"
	"testing"
	"time"
)

func TestStoreReportsOutcomesAndIdleConnections(t *testing.T) {
	s := New("p", 4, 3)
	events := []traffic.Event{{Sequence: 1, Kind: "connected"}, {Sequence: 2, Kind: "scheduled"}, {Sequence: 3, Kind: "operation", Outcome: traffic.Ambiguous}, {Sequence: 4, Kind: "connected"}, {Sequence: 5, Kind: "disconnected"}}
	for _, e := range events {
		e.ProcessID = "p"
		e.At = time.Now()
		s.Record(e)
	}
	snap := s.Snapshot()
	if snap.Sample.Ambiguous != 1 || snap.Sample.Attempts != 1 || snap.Connected != 1 || len(snap.Events) != 3 || !snap.Truncated {
		t.Fatalf("%+v", snap)
	}
	text := s.Metrics()
	if !strings.Contains(text, "outcome=\"ambiguous\"") || !strings.Contains(text, "infra_test_connected_sessions{process_id=\"p\"} 1") {
		t.Fatal(text)
	}
}
func TestReadinessNeedsEveryConfiguredSession(t *testing.T) {
	s := New("p", 4, 10)
	for i := uint64(1); i <= 4; i++ {
		s.Record(traffic.Event{ProcessID: "p", Sequence: i, Kind: "connected"})
	}
	if !s.Ready() {
		t.Fatal("not ready")
	}
	s.Record(traffic.Event{ProcessID: "p", Sequence: 5, Kind: "disconnected"})
	if s.Ready() {
		t.Fatal("false readiness")
	}
}
