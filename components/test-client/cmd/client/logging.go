package main

import (
	"encoding/json"
	"errors"
	"io"
	"net/netip"
	"sync"
	"time"

	"github.com/jamesDeng/raptor-iap/components/test-client/internal/observe"
	"github.com/jamesDeng/raptor-iap/components/test-client/internal/traffic"
)

// trafficLog writes only the application's fixed, sanitized fields. A failed
// write is sticky: do not append to a possibly partial JSON line or retry SQL.
type trafficLog struct {
	mu                sync.Mutex
	out               io.Writer
	processID, podUID string
	writeErr          error
}

type trafficLogRecord struct {
	Kind            string              `json:"kind"`
	ProcessID       string              `json:"processId"`
	PodUID          string              `json:"podUid"`
	At              time.Time           `json:"at"`
	Sequence        uint64              `json:"sequence,omitempty"`
	OperationID     string              `json:"operationId,omitempty"`
	Outcome         traffic.Outcome     `json:"outcome,omitempty"`
	DurationSeconds *float64            `json:"durationSeconds,omitempty"`
	ProxyIP         string              `json:"proxyIp,omitempty"`
	Snapshot        *trafficLogSnapshot `json:"snapshot,omitempty"`
}

// Keep the existing final snapshot counter names without encoding its event ring.
type trafficLogSnapshot struct {
	Final     bool               `json:"final"`
	Sample    trafficLogCounters `json:"sample"`
	Connected int                `json:"connected"`
	Truncated bool               `json:"truncated"`
}

type trafficLogCounters struct {
	ProcessID                                                          string    `json:"processId"`
	At                                                                 time.Time `json:"at"`
	Sequence                                                           uint64    `json:"sequence"`
	Scheduled, Attempts, Success, Failure, Timeout, Ambiguous, Skipped uint64
}

func newTrafficLog(out io.Writer, processID, podUID string) *trafficLog {
	return &trafficLog{out: out, processID: processID, podUID: podUID}
}

func (l *trafficLog) startup() {
	l.write(trafficLogRecord{Kind: "traffic_startup", At: time.Now().UTC()})
}

func (l *trafficLog) emit(record func(traffic.Event)) func(traffic.Event) {
	return func(e traffic.Event) {
		record(e)
		line := trafficLogRecord{At: e.At.UTC(), Sequence: e.Sequence}
		switch e.Kind {
		case "connected", "disconnected", "connection_failure":
			line.Kind = e.Kind
		case "scheduled", "skipped":
			line.Kind, line.OperationID = e.Kind, e.OperationID
		case "operation":
			line.Kind, line.OperationID = "operation", e.OperationID
			line.DurationSeconds = &e.DurationSeconds
			switch e.Outcome {
			case traffic.Success:
				line.Outcome = e.Outcome
				if addr, err := netip.ParseAddr(e.ProxyIP); err == nil && addr.Is4() && addr.IsPrivate() {
					line.ProxyIP = addr.String()
				}
			case traffic.Failure, traffic.Timeout, traffic.Ambiguous:
				line.Outcome = e.Outcome
			default:
				line.Outcome = "unknown"
			}
		default:
			return
		}
		l.write(line)
	}
}

func (l *trafficLog) final(snapshot observe.Snapshot) {
	s := snapshot.Sample
	l.write(trafficLogRecord{
		Kind: "traffic_final", At: s.At.UTC(), Sequence: s.Sequence,
		Snapshot: &trafficLogSnapshot{
			Final: snapshot.Final, Connected: snapshot.Connected, Truncated: snapshot.Truncated,
			Sample: trafficLogCounters{
				ProcessID: l.processID, At: s.At.UTC(), Sequence: s.Sequence,
				Scheduled: s.Scheduled, Attempts: s.Attempts, Success: s.Success,
				Failure: s.Failure, Timeout: s.Timeout, Ambiguous: s.Ambiguous, Skipped: s.Skipped,
			},
		},
	})
}

func (l *trafficLog) write(record trafficLogRecord) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.writeErr != nil {
		return
	}
	record.ProcessID, record.PodUID = l.processID, l.podUID
	line, err := json.Marshal(record)
	if err == nil {
		line = append(line, '\n')
		var n int
		n, err = l.out.Write(line)
		if err == nil && n != len(line) {
			err = io.ErrShortWrite
		}
	}
	if err != nil {
		// Writer errors may contain credentials or arbitrary runtime details.
		l.writeErr = errors.New("traffic stdout logging failed")
	}
}

func (l *trafficLog) err() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.writeErr
}
