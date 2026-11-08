package runtime

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestPgcatApplicationLogsRequireContinuousSuccessfulPodEvents(t *testing.T) {
	since := time.Now().UTC().Add(-time.Minute)
	line := func(kind, pod, detail string, sequence int) string {
		return fmt.Sprintf(`{"kind":%q,"podUid":%q,"processId":"proc-1","at":%q,"sequence":%d%s}`+"\n",
			kind, pod, since.Add(time.Duration(sequence)*time.Second).Format(time.RFC3339Nano), sequence, detail)
	}
	const pod = "pod-1"
	start := line("traffic_startup", pod, "", 0)
	connected := line("connected", pod, "", 1)
	success := line("operation", pod, `,"outcome":"success"`, 2)
	final := line("traffic_final", pod, `,"snapshot":{"final":true,"connected":0}`, 2)
	for _, test := range []struct {
		name   string
		log    string
		follow bool
		valid  bool
	}{
		{"rolling pod", start + connected + success, false, true},
		{"old pod shutdown", start + connected + success + final, true, true},
		{"missing final", start + connected + success, true, false},
		{"foreign pod", start + connected + line("operation", "foreign", `,"outcome":"success"`, 2), false, false},
		{"dropped event", start + line("operation", pod, `,"outcome":"success"`, 2), false, false},
		{"failed SQL", start + connected + line("operation", pod, `,"outcome":"timeout"`, 2), false, false},
		{"skipped SQL", start + connected + success + line("skipped", pod, "", 3), false, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			state := newPgcatLogState(pod)
			state.readLogs(strings.NewReader(test.log), since, test.follow)
			if got := state.check(test.follow, true, since.Add(time.Second)); got != test.valid {
				t.Fatalf("log valid = %v, want %v", got, test.valid)
			}
		})
	}
}

func TestPgcatApplicationCounterUsesOnlyBoundPoolAndUser(t *testing.T) {
	const queries = "pgcat_stats_total_query_count"
	const transactions = "pgcat_stats_total_xact_count"
	const app = `{pool="test",user="poc_app"}`
	const probe = `{pool="test",user="probe"}`
	counts := queries + app + " 17\n" + transactions + app + " 9\n"
	for _, test := range []struct {
		name  string
		body  string
		valid bool
	}{
		{"counts with unrelated probes", counts + queries + probe + " 100\n", true},
		{"probe only", queries + probe + " 100\n" + transactions + probe + " 100\n", false},
		{"missing transactions", queries + app + " 17\n", false},
		{"duplicate series", counts + transactions + app + " 9\n", false},
		{"negative counter", queries + app + " -1\n" + transactions + app + " 9\n", false},
		{"oversized response", counts + strings.Repeat("#\n", 140000), false},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := parsePgcatApplicationCounter(strings.NewReader(test.body))
			if (err == nil) != test.valid || test.valid && got != (pgcatQueryCount{Queries: 17, Transactions: 9}) {
				t.Fatalf("counter = %+v, err = %v", got, err)
			}
		})
	}
}
