package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jamesDeng/raptor-iap/components/test-client/internal/observe"
	"github.com/jamesDeng/raptor-iap/components/test-client/internal/traffic"
)

func logLines(t *testing.T, b *bytes.Buffer) []map[string]json.RawMessage {
	t.Helper()
	if b.Len() == 0 || !bytes.HasSuffix(b.Bytes(), []byte("\n")) {
		t.Fatal("missing newline-delimited logs")
	}
	var records []map[string]json.RawMessage
	for _, line := range strings.Split(strings.TrimSuffix(b.String(), "\n"), "\n") {
		var record map[string]json.RawMessage
		if err := json.Unmarshal([]byte(line), &record); err != nil || record == nil {
			t.Fatalf("expected one JSON object per line: %q (%v)", line, err)
		}
		records = append(records, record)
	}
	return records
}

func logString(t *testing.T, record map[string]json.RawMessage, field string) string {
	t.Helper()
	var value string
	if err := json.Unmarshal(record[field], &value); err != nil {
		t.Fatalf("missing/invalid %s: %v", field, err)
	}
	return value
}

// Missing startup emission or lost process/Pod identity must fail this test.
func TestTrafficLogStartupIdentity(t *testing.T) {
	var out bytes.Buffer
	logs := newTrafficLog(&out, "process-1", "pod-1")
	before := time.Now().UTC()
	logs.startup()
	after := time.Now().UTC()
	records := logLines(t, &out)
	if len(records) != 1 || logString(t, records[0], "kind") != "traffic_startup" || logString(t, records[0], "processId") != "process-1" || logString(t, records[0], "podUid") != "pod-1" {
		t.Fatal(out.String())
	}
	at, err := time.Parse(time.RFC3339Nano, logString(t, records[0], "at"))
	if err != nil || !strings.HasSuffix(logString(t, records[0], "at"), "Z") || at.Before(before) || at.After(after) {
		t.Fatal("startup must contain current UTC time", out.String())
	}
	if logs.err() != nil {
		t.Fatal(logs.err())
	}
}

// Dropping any outcome, rewriting a source sequence/time, or omitting an SQL
// operation's zero duration breaks reconstruction of the traffic stream.
func TestTrafficLogProjectsEventsAndPreservesEvidence(t *testing.T) {
	var out bytes.Buffer
	logs := newTrafficLog(&out, "process-1", "pod-1")
	store := observe.New("process-1", 1, 8192)
	emit := logs.emit(store.Record)
	at := time.Date(2026, 11, 3, 12, 0, 0, 123, time.FixedZone("local", 8*3600))
	events := []traffic.Event{
		{Kind: "connected"},
		{Kind: "scheduled", OperationID: "process-1-0-1"},
		{Kind: "operation", OperationID: "process-1-0-1", Outcome: traffic.Success, DurationSeconds: 0.125},
		{Kind: "operation", OperationID: "process-1-0-2", Outcome: traffic.Failure, DurationSeconds: 0.25},
		{Kind: "operation", OperationID: "process-1-0-3", Outcome: traffic.Timeout, DurationSeconds: 5},
		{Kind: "operation", OperationID: "process-1-0-4", Outcome: traffic.Ambiguous},
		{Kind: "disconnected"},
		{Kind: "connection_failure"},
		{Kind: "skipped", OperationID: "process-1-0-5"},
	}
	for i := range events {
		events[i].ProcessID, events[i].Sequence, events[i].At = "process-1", uint64(41+i), at
		emit(events[i])
	}
	records := logLines(t, &out)
	if len(records) != len(events) {
		t.Fatalf("got %d event lines; want %d", len(records), len(events))
	}
	for i, record := range records {
		var got traffic.Event
		line, _ := json.Marshal(record)
		if err := json.Unmarshal(line, &got); err != nil {
			t.Fatal(err)
		}
		want := events[i]
		if got.ProcessID != want.ProcessID || got.Sequence != want.Sequence || !got.At.Equal(at) || got.Kind != want.Kind || got.OperationID != want.OperationID || got.Outcome != want.Outcome || got.DurationSeconds != want.DurationSeconds || logString(t, record, "podUid") != "pod-1" {
			t.Fatalf("event changed: %s", line)
		}
		if !strings.HasSuffix(logString(t, record, "at"), "Z") {
			t.Fatal("event timestamp was not normalized to UTC")
		}
		if want.Kind == "operation" && record["durationSeconds"] == nil {
			t.Fatal("zero operation duration omitted")
		}
	}
	snapshot := store.Snapshot()
	if len(snapshot.Events) != 9 || snapshot.Sample.Sequence != 49 || snapshot.Sample.Attempts != 4 || snapshot.Sample.Success != 1 || snapshot.Sample.Failure != 1 || snapshot.Sample.Timeout != 1 || snapshot.Sample.Ambiguous != 1 || snapshot.Sample.Skipped != 1 || snapshot.Connected != 0 {
		t.Fatalf("evidence changed: %+v", snapshot)
	}
}

// Accidentally passing Snapshot through the encoder would leak event payloads
// and make the terminal line grow with the 8192-event ring.
func TestTrafficLogFinalIsCompactCounters(t *testing.T) {
	var out bytes.Buffer
	logs := newTrafficLog(&out, "process-1", "pod-1")
	store := observe.New("process-1", 1, 8192)
	at := time.Now().UTC()
	for i := 0; i < 4096; i++ {
		store.Record(traffic.Event{Sequence: uint64(2*i + 1), Kind: "scheduled", OperationID: "SECRET_EVENT_PAYLOAD", At: at})
		store.Record(traffic.Event{Sequence: uint64(2*i + 2), Kind: "operation", Outcome: traffic.Success, OperationID: "SECRET_EVENT_PAYLOAD", At: at})
	}
	final, err := store.Finalize()
	if err != nil {
		t.Fatal(err)
	}
	logs.final(final)
	records := logLines(t, &out)
	if len(records) != 1 || logString(t, records[0], "kind") != "traffic_final" || logString(t, records[0], "podUid") != "pod-1" || logString(t, records[0], "processId") != "process-1" {
		t.Fatal(out.String())
	}
	var compact map[string]json.RawMessage
	if err := json.Unmarshal(records[0]["snapshot"], &compact); err != nil {
		t.Fatal(err)
	}
	var summary observe.Snapshot
	if err := json.Unmarshal(records[0]["snapshot"], &summary); err != nil || !summary.Final || summary.Sample.Scheduled != 4096 || summary.Sample.Attempts != 4096 || summary.Sample.Success != 4096 || summary.Sample.Sequence != 8192 || summary.Connected != 0 {
		t.Fatalf("terminal counters changed: %s (%v)", out.String(), err)
	}
	if _, found := compact["events"]; found || out.Len() > 2048 || strings.Contains(out.String(), "SECRET_EVENT_PAYLOAD") {
		t.Fatal("final log contains event history or is oversized")
	}
}

// Unknown caller labels and irrelevant fields must never become log text.
func TestTrafficLogUsesFixedLabels(t *testing.T) {
	var out bytes.Buffer
	logs := newTrafficLog(&out, "process-1", "pod-1")
	emit := logs.emit(func(traffic.Event) {})
	emit(traffic.Event{Kind: "SECRET_CALLER_LABEL", Outcome: "SECRET_OUTCOME"})
	emit(traffic.Event{Kind: "connected", Outcome: "SECRET_OUTCOME", OperationID: "SECRET_SQL_TEXT"})
	emit(traffic.Event{Kind: "operation", Outcome: "SECRET_OUTCOME", OperationID: "process-1-0-1"})
	if strings.Contains(out.String(), "SECRET_") {
		t.Fatal("arbitrary labels/irrelevant values logged", out.String())
	}
	records := logLines(t, &out)
	if len(records) != 2 || logString(t, records[1], "outcome") != "unknown" {
		t.Fatal(out.String())
	}
}

type failedLogWriter struct {
	writes int
	short  bool
}

func (w *failedLogWriter) Write(p []byte) (int, error) {
	w.writes++
	if w.short {
		return len(p) / 2, nil
	}
	return 0, errors.New("SECRET_RAW_WRITER_ERROR")
}

type logSession struct{ calls int }

func (s *logSession) Operate(context.Context, string) traffic.Outcome {
	s.calls++
	return traffic.Success
}
func (*logSession) Close() {}

// Writer errors (including short writes) must be visible without retrying SQL,
// cancelling traffic or preventing evidence from reaching a settled final.
func TestTrafficLogWriterFailureDoesNotChangeTraffic(t *testing.T) {
	for _, short := range []bool{false, true} {
		t.Run(map[bool]string{false: "error", true: "short_write"}[short], func(t *testing.T) {
			writer := &failedLogWriter{short: short}
			logs := newTrafficLog(writer, "process-1", "pod-1")
			store := observe.New("process-1", 1, 8192)
			logs.startup()
			ticks := make(chan time.Time, 1)
			ticks <- time.Now()
			close(ticks)
			session := &logSession{}
			runner := traffic.Runner{Config: traffic.Config{Sessions: 1, Timeout: time.Second, ProcessID: "process-1"}, Ticks: ticks, Dial: func(context.Context) (traffic.Session, error) { return session, nil }, Emit: logs.emit(store.Record)}
			if err := runner.Run(context.Background()); err != nil {
				t.Fatal(err)
			}
			final, err := store.Finalize()
			if err != nil || final.Sample.Success != 1 || session.calls != 1 {
				t.Fatalf("logging failure changed SQL/evidence: %+v %v calls=%d", final, err, session.calls)
			}
			logs.final(final)
			if logs.err() == nil || strings.Contains(logs.err().Error(), "SECRET_") || writer.writes != 1 {
				t.Fatalf("log failure was hidden/leaked/retried: %v writes=%d", logs.err(), writer.writes)
			}
		})
	}
}

// Raw dial errors and DSNs never enter the sanitized Runner event projection.
func TestTrafficLogDialFailureDoesNotLeakSecret(t *testing.T) {
	var out bytes.Buffer
	logs := newTrafficLog(&out, "process-1", "pod-1")
	store := observe.New("process-1", 1, 8192)
	ticks := make(chan time.Time, 1)
	ticks <- time.Now()
	close(ticks)
	runner := traffic.Runner{Config: traffic.Config{Sessions: 1, Timeout: time.Second, ProcessID: "process-1"}, Ticks: ticks, Dial: func(context.Context) (traffic.Session, error) {
		return nil, errors.New("postgres://user:SECRET_CREDENTIAL@localhost/db SELECT SECRET_SQL")
	}, Emit: logs.emit(store.Record)}
	if err := runner.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), "SECRET_") || strings.Contains(out.String(), "postgres://") || strings.Contains(out.String(), "SELECT") {
		t.Fatal("raw dial error/DSN/SQL leaked")
	}
	counts := map[string]int{}
	for _, record := range logLines(t, &out) {
		counts[logString(t, record, "kind")]++
	}
	if counts["connection_failure"] != 2 || counts["operation"] != 1 || counts["scheduled"] != 1 {
		t.Fatal("connection attempts or failure missing", counts)
	}
	if logs.err() != nil {
		t.Fatal(logs.err())
	}
}
