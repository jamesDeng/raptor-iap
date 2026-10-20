package runtime

import (
	"bytes"
	"context"
	"errors"
	"log"
	"strings"
	"testing"

	"github.com/jamesDeng/raptor-iap/components/agent-gateway/internal/execution"
)

func TestAXPollFailureCategoryDoesNotExposeErrorText(t *testing.T) {
	cases := []struct {
		err  error
		want string
	}{
		{ErrFileNotFound, "file_absent"},
		{ErrRuntimeUnavailable, "runtime_unavailable"},
		{ErrCommandUnconfirmed, "command_unconfirmed"},
		{errors.New("secret-bearing provider response"), "other"},
	}
	for _, c := range cases {
		if got := axPollFailureCategory(c.err); got != c.want {
			t.Fatalf("category = %q, want %q", got, c.want)
		}
	}
}

func TestAXPollRunIdentityDiagnosticDistinguishesAbsentAndMismatchedRun(t *testing.T) {
	requestID := "87b0ba57-a6d3-487a-96e7-530426e35878"
	for _, tc := range []struct {
		name string
		runs map[string]*axRun
		want string
	}{
		{"absent", nil, "stage=run_identity category=run_absent"},
		{"mismatch", map[string]*axRun{requestID: {start: execution.LiveStart{Binding: execution.AttemptBinding{AttemptID: "expected-attempt"}}}}, "stage=run_identity category=handle_mismatch"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var logs bytes.Buffer
			original := log.Writer()
			log.SetOutput(&logs)
			defer log.SetOutput(original)
			a := &AXLive{axRuns: tc.runs}
			_, err := a.Poll(context.Background(), execution.RuntimeHandle{RequestID: requestID, ID: "raptor-other-attempt"}, 0)
			if !errors.Is(err, ErrRuntimeUnavailable) {
				t.Fatalf("Poll error = %v, want runtime unavailable", err)
			}
			if !strings.Contains(logs.String(), tc.want) {
				t.Fatalf("log = %q, want %q", logs.String(), tc.want)
			}
			if strings.Contains(logs.String(), "expected-attempt") || strings.Contains(logs.String(), "other-attempt") {
				t.Fatalf("log exposed run handle: %q", logs.String())
			}
		})
	}
}
