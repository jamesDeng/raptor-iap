package execution

import (
	"context"
	"testing"
)

func TestSegmentUsageIsImmutableFencedAndCumulative(t *testing.T) {
	s, x, b := liveAttempt(t)
	ctx := context.Background()
	usage := []Usage{{Input: 3, Output: 2, TotalTokens: 5}}
	if e := s.SaveSegmentUsage(ctx, x.AttemptID, "stale", usage); e == nil {
		t.Fatal("stale usage writer")
	}
	for i := 0; i < 2; i++ {
		if e := s.SaveSegmentUsage(ctx, x.AttemptID, "owner", usage); e != nil {
			t.Fatal(e)
		}
	}
	if e := s.SaveSegmentUsage(ctx, x.AttemptID, "owner", []Usage{{Input: 4, TotalTokens: 4}}); e == nil {
		t.Fatal("usage overwritten")
	}
	total, e := s.RequestUsage(ctx, b.RequestID)
	if e != nil || total.TotalTokens != 5 {
		t.Fatal(total, e)
	}
	s.FinalizeLive(ctx, x.AttemptID, "owner", LiveOutcome{Status: "failed", FailureCode: "Interrupted", Checkpoint: verifiedCheckpoint(), SandboxAbsent: true, KeyAbsent: true, AccessRevoked: true})
	s.Pool.Exec(ctx, "UPDATE gateway.executions SET status='queued' WHERE request_id=$1", b.RequestID)
	next, e := s.ClaimLiveNext(ctx, "owner")
	if e != nil || next == nil {
		t.Fatal(e)
	}
	b.AttemptID = next.AttemptID
	s.BindLive(ctx, next.AttemptID, "owner", b, "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef")
	if e = s.SaveSegmentUsage(ctx, next.AttemptID, "owner", []Usage{{Input: 6, Output: 4, TotalTokens: 10}}); e != nil {
		t.Fatal(e)
	}
	total, e = s.RequestUsage(ctx, b.RequestID)
	if e != nil || total.Input != 9 || total.Output != 6 || total.TotalTokens != 15 {
		t.Fatal(total, e)
	}
	if e = s.SaveSegmentUsage(ctx, next.AttemptID, "owner", []Usage{{Input: -1}}); e == nil {
		t.Fatal("negative usage")
	}
}

func TestSegmentUsagePersistsMoreThanTenRecords(t *testing.T) {
	s, x, b := liveAttempt(t)
	ctx := context.Background()
	usage := make([]Usage, 11)
	for i := range usage {
		usage[i] = Usage{Input: 3, Output: 2, TotalTokens: 5}
	}
	for i := 0; i < 2; i++ {
		if err := s.SaveSegmentUsage(ctx, x.AttemptID, "owner", usage); err != nil {
			t.Fatal(err)
		}
	}
	want := Usage{Input: 33, Output: 22, TotalTokens: 55}
	if got, err := s.RequestUsage(ctx, b.RequestID); err != nil || got != want {
		t.Fatalf("request usage: got %+v, want %+v, error %v", got, want, err)
	}
}

func TestSegmentUsageRejectsOversizedPayloadBeforePersisting(t *testing.T) {
	s, x, b := liveAttempt(t)
	ctx := context.Background()
	if err := s.SaveSegmentUsage(ctx, x.AttemptID, "owner", make([]Usage, 2048)); err != ErrInvalid {
		t.Fatalf("oversized usage: got %v, want %v", err, ErrInvalid)
	}
	if got, err := s.RequestUsage(ctx, b.RequestID); err != nil || got != (Usage{}) {
		t.Fatalf("oversized usage affected request totals: %+v, %v", got, err)
	}
}
