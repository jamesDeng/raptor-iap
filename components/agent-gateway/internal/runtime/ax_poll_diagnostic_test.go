package runtime

import (
	"errors"
	"testing"
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
