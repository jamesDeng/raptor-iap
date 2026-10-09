package approvals

import (
	"context"
	"encoding/json"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/domain"
	"sync"
	"sync/atomic"
	"testing"
)

func TestFleetGuardConcurrentDurableAndExactRelease(t *testing.T) {
	s, _ := submissionFixture(t)
	ctx := context.Background()
	var wins atomic.Int32
	var wg sync.WaitGroup
	var token string
	var mu sync.Mutex
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			id := domain.NewID()
			out, e := s.FleetClaim(ctx, FleetInput{EnvCode: "rdev.ali", GroupID: "asg", Token: id, Intent: json.RawMessage(`{"kind":"deregister"}`)})
			if e != nil {
				t.Error(e)
				return
			}
			if out.Claimed {
				wins.Add(1)
				mu.Lock()
				token = id
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if wins.Load() != 1 {
		t.Fatalf("winners=%d", wins.Load())
	}
	s = &Service{Pool: s.Pool}
	in := FleetInput{EnvCode: "rdev.ali", GroupID: "asg", Token: domain.NewID(), Intent: json.RawMessage(`{"kind":"scale"}`)}
	out, e := s.FleetClaim(ctx, in)
	if e != nil || out.Claimed || out.Token != token {
		t.Fatalf("durability %v %v", out, e)
	}
	if e = s.FleetRecord(ctx, in); e == nil {
		t.Fatal("wrong token recorded guard")
	}
	if e = s.FleetResolve(ctx, in); e == nil {
		t.Fatal("wrong token released guard")
	}
	in.Token = token
	if out.Recorded {
		t.Fatal("unsubmitted guard already recorded")
	}
	if e = s.FleetRecord(ctx, in); e != nil {
		t.Fatal(e)
	}
	out, e = s.FleetClaim(ctx, FleetInput{EnvCode: in.EnvCode, GroupID: in.GroupID, Token: domain.NewID(), Intent: in.Intent})
	if e != nil || out.Claimed || !out.Recorded {
		t.Fatal("record phase not durable", e)
	}
	if e = s.FleetResolve(ctx, in); e != nil {
		t.Fatal(e)
	}
	in.Token = domain.NewID()
	out, e = s.FleetClaim(ctx, in)
	if e != nil || !out.Claimed {
		t.Fatal("resolved guard not available", e)
	}
}
