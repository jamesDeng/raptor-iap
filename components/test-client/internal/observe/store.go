package observe

import (
	"fmt"
	"github.com/jamesDeng/raptor-iap/components/test-client/internal/evidence"
	"github.com/jamesDeng/raptor-iap/components/test-client/internal/traffic"
	"strings"
	"sync"
	"time"
)

type Snapshot struct {
	Sample    evidence.Sample `json:"sample"`
	Events    []traffic.Event `json:"events"`
	Connected int             `json:"connected"`
	Truncated bool            `json:"truncated"`
}
type Store struct {
	mu                             sync.Mutex
	sample                         evidence.Sample
	events                         []traffic.Event
	connected, sessions, limit     int
	truncated                      bool
	connectionOK, connectionFailed uint64
	durationCount                  uint64
	durationSum                    float64
}

func New(process string, sessions, limit int) *Store {
	return &Store{sample: evidence.Sample{ProcessID: process}, sessions: sessions, limit: limit}
}
func (s *Store) Record(e traffic.Event) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sample.Sequence = e.Sequence
	switch e.Kind {
	case "scheduled":
		s.sample.Scheduled++
	case "skipped":
		s.sample.Skipped++
	case "connected":
		s.connected++
		s.connectionOK++
	case "disconnected":
		s.connected--
	case "connection_failure":
		s.connectionFailed++
	case "operation":
		s.sample.Attempts++
		s.durationCount++
		s.durationSum += e.DurationSeconds
		switch e.Outcome {
		case traffic.Success:
			s.sample.Success++
		case traffic.Failure:
			s.sample.Failure++
		case traffic.Timeout:
			s.sample.Timeout++
		case traffic.Ambiguous:
			s.sample.Ambiguous++
		}
	}
	s.events = append(s.events, e)
	if len(s.events) > s.limit {
		s.events = append([]traffic.Event(nil), s.events[len(s.events)-s.limit:]...)
		s.truncated = true
	}
}
func (s *Store) Snapshot() Snapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	a := s.sample
	a.At = time.Now().UTC()
	return Snapshot{Sample: a, Events: append([]traffic.Event(nil), s.events...), Connected: s.connected, Truncated: s.truncated}
}
func (s *Store) Ready() bool { s.mu.Lock(); defer s.mu.Unlock(); return s.connected == s.sessions }
func (s *Store) Metrics() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	var b strings.Builder
	label := fmt.Sprintf("process_id=%q", s.sample.ProcessID)
	for _, v := range []struct {
		name string
		n    uint64
	}{{"success", s.sample.Success}, {"failure", s.sample.Failure}, {"timeout", s.sample.Timeout}, {"ambiguous", s.sample.Ambiguous}} {
		fmt.Fprintf(&b, "infra_test_operations_total{%s,outcome=%q} %d\n", label, v.name, v.n)
	}
	fmt.Fprintf(&b, "infra_test_connected_sessions{%s} %d\ninfra_test_scheduled_operations_total{%s} %d\ninfra_test_skipped_operations_total{%s} %d\ninfra_test_connection_attempts_total{%s,outcome=\"success\"} %d\ninfra_test_connection_attempts_total{%s,outcome=\"failure\"} %d\ninfra_test_operation_duration_seconds_sum{%s} %g\ninfra_test_operation_duration_seconds_count{%s} %d\n", label, s.connected, label, s.sample.Scheduled, label, s.sample.Skipped, label, s.connectionOK, label, s.connectionFailed, label, s.durationSum, label, s.durationCount)
	return b.String()
}
