package runtime

import (
	"context"
	"github.com/jamesDeng/raptor-iap/components/agent-gateway/internal/execution"
	"testing"
)

func TestCheckpointIsolationAndCleanup(t *testing.T) {
	ctx := context.Background()
	s := &Simulated{Root: t.TempDir()}
	in := execution.ExecutionInput{RequestID: execution.NewID()}
	h, e := s.Start(ctx, in)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.Start(ctx, in); e == nil {
		t.Fatal("overlapping runtime")
	}
	ref, e := s.Checkpoint(ctx, h)
	if e != nil {
		t.Fatal(e)
	}
	out, e := s.Stop(ctx, h)
	if e != nil || !out.Confirmed {
		t.Fatal("cleanup failed")
	}
	wrong := in
	wrong.RequestID = execution.NewID()
	if _, e = s.Restore(ctx, wrong, ref); e == nil {
		t.Fatal("cross-request restore")
	}
	h, e = s.Restore(ctx, in, ref)
	if e != nil {
		t.Fatal(e)
	}
	s.Stop(ctx, h)
	if s.MaxActive != 1 {
		t.Fatal("overlapping agent execution")
	}
}
