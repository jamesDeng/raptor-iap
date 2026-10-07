package requests

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"
)

type fakeProgress struct{ fail bool }

func (f *fakeProgress) Execution(context.Context, string) (map[string]any, error) {
	if f.fail {
		return nil, errors.New("offline")
	}
	return map[string]any{"status": "running"}, nil
}
func (f *fakeProgress) Progress(context.Context, string, int64) ([]GatewayEvent, error) {
	if f.fail {
		return nil, errors.New("offline")
	}
	return []GatewayEvent{{EventID: "event", Sequence: 1, Kind: "progress", Summary: "Simulated saved progress", EvidenceMode: "simulated"}}, nil
}
func TestSavedProgressWhenGatewayUnavailable(t *testing.T) {
	s, in, u := setup(t)
	ctx := context.Background()
	request, e := s.Create(ctx, u, "progress", in)
	if e != nil {
		t.Fatal(e)
	}
	gateway := &fakeProgress{}
	s.Gateway = gateway
	first, e := s.Timeline(ctx, request.ID, 0)
	if e != nil || len(first.Events) != 1 {
		t.Fatal("no progress")
	}
	gateway.fail = true
	second, e := s.Timeline(ctx, request.ID, 0)
	if e != nil || len(second.Events) != 1 || !second.SyncUnavailable {
		t.Fatal("saved history lost")
	}
	view, e := s.View(ctx, request.ID)
	if e != nil || view.ExecutionAvailable {
		t.Fatal("gateway unavailable reported live")
	}
}

type pagedProgress struct{ events []GatewayEvent }

func (f *pagedProgress) Execution(context.Context, string) (map[string]any, error) {
	return nil, errors.New("unused")
}
func (f *pagedProgress) Progress(_ context.Context, _ string, after int64) ([]GatewayEvent, error) {
	out := []GatewayEvent{}
	for _, e := range f.events {
		if e.Sequence > after {
			out = append(out, e)
			if len(out) == 100 {
				break
			}
		}
	}
	return out, nil
}
func TestProgressAcrossGatewayPages(t *testing.T) {
	s, in, u := setup(t)
	ctx := context.Background()
	r, e := s.Create(ctx, u, "pages", in)
	if e != nil {
		t.Fatal(e)
	}
	f := &pagedProgress{}
	for i := 1; i <= 205; i++ {
		f.events = append(f.events, GatewayEvent{RequestID: r.ID, AttemptID: "a", EventID: fmt.Sprint(i), Sequence: int64(i), Kind: "progress", Summary: "read", EvidenceMode: "live", OccurredAt: time.Now().UTC()})
	}
	s.Gateway = f
	var after int64
	seen := 0
	for i := 0; i < 4; i++ {
		timeline, e := s.Timeline(ctx, r.ID, after)
		if e != nil {
			t.Fatal(e)
		}
		for _, event := range timeline.Events {
			if event.Sequence <= after {
				t.Fatal("cursor regressed")
			}
			after = event.Sequence
			seen++
		}
	}
	if seen != 205 {
		t.Fatalf("only %d events survived pagination", seen)
	}
}
