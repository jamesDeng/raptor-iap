package execution

import (
	"context"
	"testing"
)

func TestRetryAllowancePersists(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	id := NewID()
	s.Receive(ctx, id)
	for i := 0; i < 3; i++ {
		allowed, e := (&Store{Pool: s.Pool}).AllowApplyRetry(ctx, id, "original-run")
		if e != nil {
			t.Fatal(e)
		}
		if allowed != (i < 2) {
			t.Fatal("retry allowance reset")
		}
	}
}
