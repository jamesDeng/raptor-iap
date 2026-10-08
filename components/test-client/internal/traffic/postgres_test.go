package traffic

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5/pgconn"
	"testing"
)

func TestCommitTransportLossIsAmbiguous(t *testing.T) {
	if got := commitOutcome(errors.New("connection lost")); got != Ambiguous {
		t.Fatal(got)
	}
}
func TestCommitServerRejectionIsFailure(t *testing.T) {
	if got := commitOutcome(&pgconn.PgError{Code: "40001"}); got != Failure {
		t.Fatal(got)
	}
}
func TestDeadlineBeforeCommitIsTimeout(t *testing.T) {
	if got := statementOutcome(context.DeadlineExceeded); got != Timeout {
		t.Fatal(got)
	}
}
func TestNoErrorCommitsSuccessfully(t *testing.T) {
	if got := commitOutcome(nil); got != Success {
		t.Fatal(got)
	}
}

func TestCommitResolutionUnknownIsAmbiguous(t *testing.T) {
	for _, code := range []string{"40003", "08007"} {
		if got := commitOutcome(&pgconn.PgError{Code: code}); got != Ambiguous {
			t.Fatal(code, got)
		}
	}
}
