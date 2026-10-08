package evidence

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/jamesDeng/raptor-iap/components/test-client/internal/traffic"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

type Target struct {
	PodUID string `json:"podUid"`
	URL    string `json:"url"`
}
type RemoteSnapshot struct {
	Sample    Sample          `json:"sample"`
	Events    []traffic.Event `json:"events"`
	Connected int             `json:"connected"`
	Truncated bool            `json:"truncated"`
}
type Bundle struct {
	EvidenceMode      string          `json:"evidenceMode"`
	Start             time.Time       `json:"start"`
	End               time.Time       `json:"end"`
	Targets           []Target        `json:"targets"`
	ExpectedProcesses []string        `json:"expectedProcesses"`
	Samples           []Sample        `json:"samples"`
	Events            []traffic.Event `json:"events"`
	Issues            []string        `json:"issues"`
}

// Capture records a fixed, operator-supplied pod inventory. Replacement or missing
// pod identities produce explicit issues; this does not implement Kubernetes discovery.
func Capture(ctx context.Context, targets []Target, duration, interval time.Duration) (Bundle, error) {
	b := Bundle{EvidenceMode: "local", Start: time.Now().UTC(), Targets: targets, Issues: []string{}}
	if len(targets) == 0 || len(targets) > 16 || duration <= 0 || duration > 30*time.Minute || interval < 10*time.Millisecond || interval > 5*time.Second {
		return b, fmt.Errorf("invalid bounded capture configuration")
	}
	pods := map[string]bool{}
	for _, t := range targets {
		u, e := url.Parse(t.URL)
		if e != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.RawQuery != "" || u.Fragment != "" || t.PodUID == "" || pods[t.PodUID] {
			return b, fmt.Errorf("invalid capture target")
		}
		pods[t.PodUID] = true
	}
	limitCtx, cancel := context.WithTimeout(ctx, duration)
	defer cancel()
	client := &http.Client{Timeout: 3 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	byPod := map[string]string{}
	byProcess := map[string]string{}
	lastSeq := map[string]uint64{}
	issueSeen := map[string]bool{}
	issue := func(pod, reason string) {
		key := pod + ": " + reason
		if !issueSeen[key] {
			issueSeen[key] = true
			b.Issues = append(b.Issues, key)
		}
	}
	poll := func() {
		for _, t := range targets {
			if limitCtx.Err() != nil {
				return
			}
			req, e := http.NewRequestWithContext(limitCtx, http.MethodGet, strings.TrimRight(t.URL, "/")+"/evidence", nil)
			if e != nil {
				issue(t.PodUID, "invalid request")
				continue
			}
			r, e := client.Do(req)
			if e != nil {
				issue(t.PodUID, "snapshot unavailable")
				continue
			}
			var snap RemoteSnapshot
			decoder := json.NewDecoder(io.LimitReader(r.Body, 2<<20))
			decoder.DisallowUnknownFields()
			e = decoder.Decode(&snap)
			var extra any
			tail := decoder.Decode(&extra)
			_ = r.Body.Close()
			if r.StatusCode != 200 || e != nil || tail != io.EOF || snap.Sample.ProcessID == "" || snap.Sample.At.IsZero() || time.Since(snap.Sample.At) > 15*time.Second || time.Until(snap.Sample.At) > time.Second {
				issue(t.PodUID, "invalid or stale snapshot")
				continue
			}
			p := snap.Sample.ProcessID
			if old := byPod[t.PodUID]; old != "" && old != p {
				issue(t.PodUID, "process restarted")
			}
			if old := byProcess[p]; old != "" && old != t.PodUID {
				issue(t.PodUID, "duplicate process across pods")
				continue
			}
			byPod[t.PodUID] = p
			byProcess[p] = t.PodUID
			if len(b.Samples) >= 8192 || len(b.Events) >= 100000 {
				issue(t.PodUID, "capture bound exceeded")
				cancel()
				return
			}
			b.Samples = append(b.Samples, snap.Sample)
			// First snapshot defines a counter boundary; events older than it are excluded.
			if _, ok := lastSeq[p]; !ok {
				lastSeq[p] = snap.Sample.Sequence
				continue
			}
			for _, event := range snap.Events {
				if event.ProcessID != p {
					issue(t.PodUID, "event identity mismatch")
					continue
				}
				if event.Sequence > lastSeq[p] && event.Sequence <= snap.Sample.Sequence {
					if len(b.Events) >= 100000 {
						issue(t.PodUID, "capture bound exceeded")
						cancel()
						return
					}
					b.Events = append(b.Events, event)
				}
			}
			if snap.Sample.Sequence < lastSeq[p] {
				issue(t.PodUID, "sequence reset")
			}
			lastSeq[p] = snap.Sample.Sequence
		}
	}
	poll()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for limitCtx.Err() == nil {
		select {
		case <-limitCtx.Done():
		case <-ticker.C:
			poll()
		}
	}
	for _, t := range targets {
		if byPod[t.PodUID] == "" {
			issue(t.PodUID, "no process boundary observed")
		}
	}
	for p := range byProcess {
		b.ExpectedProcesses = append(b.ExpectedProcesses, p)
	}
	sort.Strings(b.ExpectedProcesses)
	b.End = time.Now().UTC()
	if ctx.Err() != nil {
		issue("capture", "interrupted")
	}
	return b, nil
}

func (b Bundle) Summary(maxGap time.Duration) Summary {
	s := (Collector{MaxGap: maxGap, EvidenceMode: "local", ExpectedProcesses: b.ExpectedProcesses}).Summarize(b.Samples, b.Events)
	s.Reasons = append(s.Reasons, b.Issues...)
	if len(b.Issues) > 0 && s.Status == "pass" {
		s.Status = "inconclusive"
	}
	return s
}
