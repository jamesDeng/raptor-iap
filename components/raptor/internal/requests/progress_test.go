package requests

import (
	"context"
	"errors"
	"testing"
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
