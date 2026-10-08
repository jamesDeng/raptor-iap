package evidence

import (
	"github.com/jamesDeng/raptor-iap/components/test-client/internal/traffic"
	"testing"
	"time"
)

func fixture() ([]Sample, []traffic.Event) {
	a := time.Unix(100, 0)
	return []Sample{{ProcessID: "p", At: a, Sequence: 0}, {ProcessID: "p", At: a.Add(5 * time.Second), Sequence: 2, Scheduled: 1, Attempts: 1, Success: 1}}, []traffic.Event{{ProcessID: "p", Sequence: 1, At: a.Add(time.Second), Kind: "scheduled", OperationID: "one"}, {ProcessID: "p", Sequence: 2, At: a.Add(2 * time.Second), Kind: "operation", OperationID: "one", Outcome: traffic.Success}}
}
func TestCompleteEvidencePasses(t *testing.T) {
	ss, es := fixture()
	s := (Collector{MaxGap: 6 * time.Second, EvidenceMode: "local", ExpectedProcesses: []string{"p"}}).Summarize(ss, es)
	if s.Status != "pass" || s.Attempts != 1 || s.Success != 1 || s.EvidenceMode != "local" {
		t.Fatalf("%+v", s)
	}
}
func TestIncompleteEvidenceCannotPass(t *testing.T) {
	for _, name := range []string{"reset", "gap", "zero", "missing-event", "unknown", "skipped", "ambiguous", "duplicate-event"} {
		t.Run(name, func(t *testing.T) {
			ss, es := fixture()
			switch name {
			case "reset":
				ss[0].Attempts = 2
			case "gap":
				ss[1].At = ss[0].At.Add(20 * time.Second)
			case "zero":
				ss[1].Attempts = 0
				ss[1].Success = 0
				es = es[:1]
			case "missing-event":
				es = es[:1]
			case "unknown":
				es[1].Outcome = "unknown"
			case "skipped":
				ss[1].Skipped = 1
			case "ambiguous":
				ss[1].Success = 0
				ss[1].Ambiguous = 1
				es[1].Outcome = traffic.Ambiguous
			case "duplicate-event":
				es = append(es, es[1])
			}
			s := (Collector{MaxGap: 6 * time.Second, EvidenceMode: "local", ExpectedProcesses: []string{"p"}}).Summarize(ss, es)
			if s.Status == "pass" {
				t.Fatal(s)
			}
		})
	}
}
func TestFailedOperationIsFailure(t *testing.T) {
	ss, es := fixture()
	ss[1].Success = 0
	ss[1].Failure = 1
	es[1].Outcome = traffic.Failure
	s := (Collector{MaxGap: 6 * time.Second, EvidenceMode: "local", ExpectedProcesses: []string{"p"}}).Summarize(ss, es)
	if s.Status != "fail" || s.Failure != 1 {
		t.Fatal(s)
	}
}

func TestEntireMissingProcessIsInconclusive(t *testing.T) {
	ss, es := fixture()
	s := (Collector{MaxGap: 6 * time.Second, EvidenceMode: "local", ExpectedProcesses: []string{"p", "missing"}}).Summarize(ss, es)
	if s.Status == "pass" {
		t.Fatal(s)
	}
}

func TestStoppedReplicaCannotPass(t *testing.T) {
	ss, es := fixture()
	ss = append(ss, Sample{ProcessID: "stopped", At: ss[0].At}, Sample{ProcessID: "stopped", At: ss[1].At})
	s := (Collector{MaxGap: 6 * time.Second, EvidenceMode: "local", ExpectedProcesses: []string{"p", "stopped"}}).Summarize(ss, es)
	if s.Status == "pass" {
		t.Fatal(s)
	}
}
func TestDifferentBoundaryOperationsCannotCancelOut(t *testing.T) {
	ss, es := fixture()
	es[0].OperationID = "new-unresolved"
	es[1].OperationID = "old-completed"
	s := (Collector{MaxGap: 6 * time.Second, EvidenceMode: "local", ExpectedProcesses: []string{"p"}}).Summarize(ss, es)
	if s.Status == "pass" {
		t.Fatal(s)
	}
}

func TestProgressStopsBeforeLastBoundary(t *testing.T) {
	ss, es := fixture()
	ss = append(ss, Sample{ProcessID: "p", At: ss[0].At.Add(10 * time.Second), Sequence: 2, Scheduled: 1, Attempts: 1, Success: 1})
	s := (Collector{MaxGap: 6 * time.Second, EvidenceMode: "local", ExpectedProcesses: []string{"p"}}).Summarize(ss, es)
	if s.Status == "pass" {
		t.Fatal(s)
	}
}
func TestDuplicateTerminalCannotPass(t *testing.T) {
	ss, es := fixture()
	es = append(es, traffic.Event{ProcessID: "p", At: ss[0].At.Add(3 * time.Second), Sequence: 3, Kind: "scheduled", OperationID: "two"}, traffic.Event{ProcessID: "p", At: ss[0].At.Add(4 * time.Second), Sequence: 4, Kind: "operation", OperationID: "one", Outcome: traffic.Success})
	ss[1].Sequence = 4
	ss[1].Scheduled = 2
	ss[1].Attempts = 2
	ss[1].Success = 2
	s := (Collector{MaxGap: 6 * time.Second, EvidenceMode: "local", ExpectedProcesses: []string{"p"}}).Summarize(ss, es)
	if s.Status == "pass" {
		t.Fatal(s)
	}
}
