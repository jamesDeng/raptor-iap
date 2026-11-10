package runtime

import (
	"context"
	"errors"
	"net/netip"
	"sync/atomic"
	"testing"
	"time"
)

type fixturePgcatSocket struct {
	backend netip.Addr
	lost    atomic.Bool
	closed  atomic.Bool
	reads   atomic.Int32
}

func (s *fixturePgcatSocket) backendAddress(context.Context) (netip.Addr, error) {
	if s.lost.Load() || s.closed.Load() {
		return netip.Addr{}, errors.New("connection interrupted")
	}
	s.reads.Add(1)
	return s.backend, nil
}

func (s *fixturePgcatSocket) close() { s.closed.Store(true) }

func TestPgcatContinuityRetainsTheExactSocketsAndRejectsLoss(t *testing.T) {
	ctx := context.Background()
	original := map[string]netip.Addr{"old-1": netip.MustParseAddr("10.70.1.220"), "old-2": netip.MustParseAddr("10.70.1.221")}
	added := map[string]netip.Addr{"new-1": netip.MustParseAddr("10.70.1.225"), "new-2": netip.MustParseAddr("10.70.1.226")}
	start := time.Now().UTC().Add(-2 * time.Minute)
	first := &fixturePgcatSocket{backend: original["old-1"]}
	second := &fixturePgcatSocket{backend: original["old-2"]}
	attempt := 0
	connectOld := func(context.Context) (pgcatSocket, error) {
		attempt++
		switch attempt {
		case 1:
			return first, nil
		case 2:
			return second, nil
		}
		return nil, errors.New("old sockets must never be replaced")
	}
	registry := newPgcatContinuity()
	defer registry.release("request")
	hold, err := registry.hold(ctx, "request", "scope", original, start, connectOld)
	if err != nil || len(hold.Sockets) != 2 {
		t.Fatalf("baseline: %+v %v", hold, err)
	}
	again, err := registry.hold(ctx, "request", "scope", original, start, connectOld)
	if err != nil || again.ObservationID != hold.ObservationID || attempt != 2 {
		t.Fatalf("reattachment: %+v %v", again, err)
	}
	fresh := 0
	connectNew := func(context.Context) (pgcatSocket, error) {
		fresh++
		if fresh%2 == 0 {
			return &fixturePgcatSocket{backend: added["new-1"]}, nil
		}
		return &fixturePgcatSocket{backend: added["new-2"]}, nil
	}
	drained, err := registry.verifyDrain(ctx, "request", "scope", hold.ObservationID, "drain-action", time.Now().UTC().Add(time.Millisecond), added, connectNew)
	if err != nil || drained.ObservationID != hold.ObservationID || drained.FreshConnections["new-1"] != 12 ||
		drained.FreshConnections["new-2"] != 12 || first.reads.Load() < 2 || second.reads.Load() < 2 {
		t.Fatalf("drain proof: %+v %v", drained, err)
	}
	if _, err := newPgcatContinuity().verifyDrain(ctx, "request", "scope", hold.ObservationID, "drain-action", time.Now().UTC().Add(time.Millisecond), added, connectNew); err == nil {
		t.Fatal("Gateway restart reused a lost physical-socket witness")
	}
	first.lost.Store(true)
	if _, err := registry.verifyDrain(ctx, "request", "scope", hold.ObservationID, "drain-action", time.Now().UTC().Add(time.Millisecond), added, connectNew); err == nil {
		t.Fatal("accepted a broken original socket")
	}
	registry.release("request")
	if !first.closed.Load() || !second.closed.Load() {
		t.Fatal("release left a database connection open")
	}
}

func TestPgcatContinuityRejectsAnyFreshConnectionOnTheOriginalPair(t *testing.T) {
	ctx := context.Background()
	first := netip.MustParseAddr("10.70.1.220")
	second := netip.MustParseAddr("10.70.1.221")
	start := time.Now().UTC().Add(-time.Second)
	original := map[string]netip.Addr{"old-1": first, "old-2": second}
	added := map[string]netip.Addr{"new-1": netip.MustParseAddr("10.70.1.225"), "new-2": netip.MustParseAddr("10.70.1.226")}
	open := 0
	registry := newPgcatContinuity()
	defer registry.release("request")
	hold, err := registry.hold(ctx, "request", "scope", original, start, func(context.Context) (pgcatSocket, error) {
		open++
		if open == 1 {
			return &fixturePgcatSocket{backend: first}, nil
		}
		return &fixturePgcatSocket{backend: second}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	foreign := &fixturePgcatSocket{backend: first}
	if _, err := registry.verifyDrain(ctx, "request", "scope", hold.ObservationID, "drain-action", time.Now().UTC().Add(time.Millisecond), added, func(context.Context) (pgcatSocket, error) { return foreign, nil }); err == nil {
		t.Fatal("fresh connection still routed to a drained node")
	}
	if !foreign.closed.Load() {
		t.Fatal("fresh sample connection was not closed")
	}
}
