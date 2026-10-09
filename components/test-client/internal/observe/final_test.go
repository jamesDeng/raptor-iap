package observe

import (
	"github.com/jamesDeng/raptor-iap/components/test-client/internal/traffic"
	"testing"
)

func TestFinalSnapshotRequiresSettledWorkAndClosedSessions(t *testing.T) {
	s := New("p", 1, 8)
	s.Record(traffic.Event{ProcessID: "p", Sequence: 1, Kind: "connected"})
	s.Record(traffic.Event{ProcessID: "p", Sequence: 2, Kind: "scheduled", OperationID: "op"})
	if _, err := s.Finalize(); err == nil {
		t.Fatal("unfinished work finalized")
	}
	s.Record(traffic.Event{ProcessID: "p", Sequence: 3, Kind: "operation", OperationID: "op", Outcome: traffic.Success})
	if _, err := s.Finalize(); err == nil {
		t.Fatal("connected session finalized")
	}
	s.Record(traffic.Event{ProcessID: "p", Sequence: 4, Kind: "disconnected"})
	snap, err := s.Finalize()
	if err != nil || !snap.Final || snap.Sample.Success != 1 || snap.Connected != 0 {
		t.Fatalf("final snapshot: %+v %v", snap, err)
	}
	if !s.Snapshot().Final {
		t.Fatal("final marker unavailable to external collector")
	}
}
