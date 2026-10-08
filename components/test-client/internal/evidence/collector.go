// Package evidence rejects incomplete observation rather than treating it as zero errors.
package evidence

import (
	"github.com/jamesDeng/raptor-iap/components/test-client/internal/traffic"
	"sort"
	"time"
)

type Sample struct {
	ProcessID                                                          string    `json:"processId"`
	At                                                                 time.Time `json:"at"`
	Sequence                                                           uint64    `json:"sequence"`
	Scheduled, Attempts, Success, Failure, Timeout, Ambiguous, Skipped uint64
}
type Summary struct {
	Status                                                             string `json:"status"`
	EvidenceMode                                                       string `json:"evidenceMode"`
	Scheduled, Attempts, Success, Failure, Timeout, Ambiguous, Skipped uint64
	Reasons                                                            []string `json:"reasons"`
}
type Collector struct {
	MaxGap            time.Duration
	EvidenceMode      string
	ExpectedProcesses []string
}

func (c Collector) Summarize(samples []Sample, events []traffic.Event) Summary {
	out := Summary{Status: "inconclusive", EvidenceMode: c.EvidenceMode, Reasons: []string{}}
	issue := func(s string) { out.Reasons = append(out.Reasons, s) }
	if c.MaxGap <= 0 || (c.EvidenceMode != "local" && c.EvidenceMode != "live") {
		issue("invalid collector configuration")
		return out
	}
	by := map[string][]Sample{}
	for _, s := range samples {
		if s.ProcessID == "" || s.At.IsZero() {
			issue("invalid sample identity/time")
		}
		by[s.ProcessID] = append(by[s.ProcessID], s)
	}
	counts := map[string]Summary{}
	sequences := map[string]map[uint64]bool{}
	scheduled := map[string]map[string]time.Time{}
	terminal := map[string]map[string]bool{}
	for _, e := range events {
		ss := by[e.ProcessID]
		if len(ss) < 2 {
			issue("event has no covered process")
			continue
		}
		sort.Slice(ss, func(i, j int) bool { return ss[i].At.Before(ss[j].At) })
		first, last := ss[0], ss[len(ss)-1]
		if e.Sequence <= first.Sequence || e.Sequence > last.Sequence {
			continue
		}
		if e.At.Before(first.At) || e.At.After(last.At) {
			issue("event outside sample interval")
		}
		if sequences[e.ProcessID] == nil {
			sequences[e.ProcessID] = map[uint64]bool{}
		}
		if sequences[e.ProcessID][e.Sequence] {
			issue("duplicate event")
			continue
		}
		sequences[e.ProcessID][e.Sequence] = true
		if scheduled[e.ProcessID] == nil {
			scheduled[e.ProcessID] = map[string]time.Time{}
			terminal[e.ProcessID] = map[string]bool{}
		}
		if e.Kind == "scheduled" || e.Kind == "operation" || e.Kind == "skipped" {
			if e.OperationID == "" {
				issue("missing operation identity")
			}
			if e.Kind == "scheduled" {
				if _, exists := scheduled[e.ProcessID][e.OperationID]; exists {
					issue("duplicate scheduled operation")
				}
				scheduled[e.ProcessID][e.OperationID] = e.At
			} else {
				if terminal[e.ProcessID][e.OperationID] {
					issue("duplicate terminal operation")
				}
				terminal[e.ProcessID][e.OperationID] = true
			}
		}
		v := counts[e.ProcessID]
		switch e.Kind {
		case "scheduled":
			v.Scheduled++
		case "skipped":
			v.Skipped++
		case "operation":
			v.Attempts++
			switch e.Outcome {
			case traffic.Success:
				v.Success++
			case traffic.Failure:
				v.Failure++
			case traffic.Timeout:
				v.Timeout++
			case traffic.Ambiguous:
				v.Ambiguous++
			default:
				issue("unknown operation outcome")
			}
		case "connected", "disconnected", "connection_failure":
		default:
			issue("unknown event kind")
		}
		counts[e.ProcessID] = v
	}
	if len(c.ExpectedProcesses) == 0 {
		issue("expected process roster required")
	}
	seenExpected := map[string]bool{}
	for _, id := range c.ExpectedProcesses {
		if id == "" || seenExpected[id] {
			issue("invalid expected process roster")
		}
		seenExpected[id] = true
		if len(by[id]) < 2 {
			issue("expected process missing boundary samples")
		}
	}
	for id := range by {
		if !seenExpected[id] {
			issue("unexpected process")
		}
	}
	for id, ss := range by {
		if len(ss) < 2 {
			issue("process requires boundary samples")
			continue
		}
		sort.Slice(ss, func(i, j int) bool { return ss[i].At.Before(ss[j].At) })
		first, last := ss[0], ss[len(ss)-1]
		for i := 1; i < len(ss); i++ {
			a, b := ss[i-1], ss[i]
			if !b.At.After(a.At) || b.At.Sub(a.At) > c.MaxGap {
				issue("scrape gap or duplicate time")
			}
			if b.Sequence < a.Sequence || b.Scheduled < a.Scheduled || b.Attempts < a.Attempts || b.Success < a.Success || b.Failure < a.Failure || b.Timeout < a.Timeout || b.Ambiguous < a.Ambiguous || b.Skipped < a.Skipped {
				issue("counter reset")
			}
		}
		// Reject subtraction underflow after reset; evidence remains inconclusive.
		if last.Sequence < first.Sequence || last.Scheduled < first.Scheduled || last.Attempts < first.Attempts || last.Success < first.Success || last.Failure < first.Failure || last.Timeout < first.Timeout || last.Ambiguous < first.Ambiguous || last.Skipped < first.Skipped {
			continue
		}
		d := Summary{Scheduled: last.Scheduled - first.Scheduled, Attempts: last.Attempts - first.Attempts, Success: last.Success - first.Success, Failure: last.Failure - first.Failure, Timeout: last.Timeout - first.Timeout, Ambiguous: last.Ambiguous - first.Ambiguous, Skipped: last.Skipped - first.Skipped}
		if d.Attempts == 0 {
			issue("process has zero attempted operations")
		}
		progress := []time.Time{first.At}
		for operation, at := range scheduled[id] {
			progress = append(progress, at)
			if !terminal[id][operation] {
				issue("scheduled operation unresolved at boundary")
			}
		}
		for operation := range terminal[id] {
			if _, exists := scheduled[id][operation]; !exists {
				issue("terminal operation has no covered schedule")
			}
		}
		progress = append(progress, last.At)
		sort.Slice(progress, func(i, j int) bool { return progress[i].Before(progress[j]) })
		for i := 1; i < len(progress); i++ {
			if progress[i].Sub(progress[i-1]) > c.MaxGap {
				issue("process scheduling progress gap")
			}
		}
		v := counts[id]
		if d.Scheduled != v.Scheduled || d.Attempts != v.Attempts || d.Success != v.Success || d.Failure != v.Failure || d.Timeout != v.Timeout || d.Ambiguous != v.Ambiguous || d.Skipped != v.Skipped {
			issue("counter/event mismatch")
		}
		if uint64(len(sequences[id])) != last.Sequence-first.Sequence {
			issue("missing event sequence")
		}
		out.Scheduled += d.Scheduled
		out.Attempts += d.Attempts
		out.Success += d.Success
		out.Failure += d.Failure
		out.Timeout += d.Timeout
		out.Ambiguous += d.Ambiguous
		out.Skipped += d.Skipped
	}
	if out.Attempts == 0 {
		issue("zero attempted operations")
	}
	if out.Skipped > 0 {
		issue("skipped scheduled work")
	}
	if out.Scheduled != out.Attempts+out.Skipped {
		issue("scheduled work unaccounted")
	}
	if len(out.Reasons) == 0 {
		out.Status = "pass"
	}
	if out.Failure > 0 || out.Timeout > 0 || out.Ambiguous > 0 {
		out.Status = "fail"
	}
	return out
}
