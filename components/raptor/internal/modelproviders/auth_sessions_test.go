package modelproviders

import (
	"context"
	"errors"
	"testing"
	"time"
)

type blockingFlow struct {
	entered chan struct{}
	release chan struct{}
	attempt *fakeAttempt
}

func (f blockingFlow) Start(context.Context) (DeviceAttempt, error) {
	close(f.entered)
	<-f.release
	return f.attempt, nil
}

func TestConnectConcurrentStartsAllowOnlyOne(t *testing.T) {
	attempt := &fakeAttempt{challenge: ConnectChallenge{VerificationURL: "https://auth.openai.com/codex/device", UserCode: "ABCD-EFGH", ExpiresAt: time.Now().Add(time.Minute)}, result: make(chan []byte), failure: make(chan error)}
	flow := blockingFlow{entered: make(chan struct{}), release: make(chan struct{}), attempt: attempt}
	s := NewService(flow, func(context.Context, string, []byte) error { return nil })
	done := make(chan error, 1)
	go func() { _, e := s.StartCodexConnect(context.Background(), "admin-1"); done <- e }()
	<-flow.entered
	if _, e := s.StartCodexConnect(context.Background(), "admin-2"); !errors.Is(e, ErrBusy) {
		t.Fatalf("second start while first initializes: %v", e)
	}
	close(flow.release)
	if e := <-done; e != nil {
		t.Fatal(e)
	}
}

type fakeFlow struct{ attempt *fakeAttempt }

func (f fakeFlow) Start(context.Context) (DeviceAttempt, error) { return f.attempt, nil }

type fakeAttempt struct {
	challenge ConnectChallenge
	result    chan []byte
	failure   chan error
}

func (f *fakeAttempt) Challenge() ConnectChallenge { return f.challenge }
func (f *fakeAttempt) Await(ctx context.Context) ([]byte, error) {
	select {
	case b := <-f.result:
		return b, nil
	case e := <-f.failure:
		return nil, e
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}
func newFixture(expires time.Time) (*Service, *fakeAttempt, *[]byte) {
	attempt := &fakeAttempt{challenge: ConnectChallenge{VerificationURL: "https://auth.openai.com/codex/device", UserCode: "ABCD-EFGH", ExpiresAt: expires}, result: make(chan []byte, 1), failure: make(chan error, 1)}
	saved := []byte("old-credential")
	service := NewService(fakeFlow{attempt}, func(_ context.Context, _ string, next []byte) error { saved = append([]byte(nil), next...); return nil })
	return service, attempt, &saved
}
func waitStatus(t *testing.T, s *Service, admin, id string, want ConnectionStatus) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		v, e := s.PollCodexConnect(context.Background(), admin, id)
		if e == nil && v.Status == want {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("status did not become %s", want)
}
func TestConnectOwnerAndSingleUse(t *testing.T) {
	s, a, saved := newFixture(time.Now().Add(time.Minute))
	c, e := s.StartCodexConnect(context.Background(), "admin-1")
	if e != nil || c.SessionID == "" || c.UserCode != "ABCD-EFGH" {
		t.Fatalf("challenge: %+v %v", c, e)
	}
	if _, e = s.PollCodexConnect(context.Background(), "admin-2", c.SessionID); !errors.Is(e, ErrForbidden) {
		t.Fatalf("foreign admin read: %v", e)
	}
	if _, e = s.StartCodexConnect(context.Background(), "admin-2"); !errors.Is(e, ErrBusy) {
		t.Fatalf("concurrent start: %v", e)
	}
	a.result <- []byte("new-credential")
	waitStatus(t, s, "admin-1", c.SessionID, StatusConnected)
	if string(*saved) != "new-credential" {
		t.Fatal("successful credential not saved")
	}
	if e = s.CancelCodexConnect(context.Background(), "admin-1", c.SessionID); !errors.Is(e, ErrClosed) {
		t.Fatalf("completed session reused: %v", e)
	}
}
func TestConnectCancelPreservesOldCredential(t *testing.T) {
	s, _, saved := newFixture(time.Now().Add(time.Minute))
	c, _ := s.StartCodexConnect(context.Background(), "admin")
	if e := s.CancelCodexConnect(context.Background(), "admin", c.SessionID); e != nil {
		t.Fatal(e)
	}
	waitStatus(t, s, "admin", c.SessionID, StatusCancelled)
	if string(*saved) != "old-credential" {
		t.Fatal("cancel replaced credential")
	}
}
func TestConnectExpiryAndFailurePreserveOldCredential(t *testing.T) {
	s, _, saved := newFixture(time.Now().Add(-time.Second))
	c, e := s.StartCodexConnect(context.Background(), "admin")
	if e != nil {
		t.Fatal(e)
	}
	waitStatus(t, s, "admin", c.SessionID, StatusExpired)
	if string(*saved) != "old-credential" {
		t.Fatal("expiry replaced credential")
	}
	s2, a2, saved2 := newFixture(time.Now().Add(time.Minute))
	c2, _ := s2.StartCodexConnect(context.Background(), "admin")
	a2.failure <- errors.New("secret provider response")
	waitStatus(t, s2, "admin", c2.SessionID, StatusFailed)
	v, _ := s2.PollCodexConnect(context.Background(), "admin", c2.SessionID)
	if v.ErrorCode != "ProviderAuthorizationFailed" || string(*saved2) != "old-credential" {
		t.Fatalf("failure leaked or changed state: %+v", v)
	}
}
