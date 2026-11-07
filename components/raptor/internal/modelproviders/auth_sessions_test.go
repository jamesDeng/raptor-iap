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

type fakeBrowserFlow struct{ attempt *fakeBrowserAttempt }

func (f fakeBrowserFlow) Start(context.Context) (DeviceAttempt, error) {
	return nil, errors.New("device not used")
}
func (f fakeBrowserFlow) StartBrowser(context.Context) (BrowserAttempt, error) { return f.attempt, nil }

type fakeBrowserAttempt struct {
	fakeAttempt
	submitted string
}

func (f *fakeBrowserAttempt) SubmitRedirect(v string) error {
	f.submitted = v
	f.result <- []byte("browser-credential")
	return nil
}
func TestBrowserConnectRequiresOwnerAndSingleSubmission(t *testing.T) {
	a := &fakeBrowserAttempt{fakeAttempt: fakeAttempt{challenge: ConnectChallenge{AuthorizationURL: "https://auth.openai.com/oauth/authorize?state=s", ExpiresAt: time.Now().Add(time.Minute)}, result: make(chan []byte, 1)}}
	saved := []byte("old")
	s := NewService(fakeBrowserFlow{a}, func(_ context.Context, _ string, b []byte) error { saved = b; return nil })
	c, e := s.StartCodexBrowserConnect(context.Background(), "admin-1")
	if e != nil {
		t.Fatal(e)
	}
	redirect := "http://localhost:1455/auth/callback?code=one-time&state=s"
	if e = s.SubmitCodexBrowserRedirect("admin-2", c.SessionID, redirect); !errors.Is(e, ErrForbidden) {
		t.Fatalf("other owner: %v", e)
	}
	if e = s.SubmitCodexBrowserRedirect("admin-1", c.SessionID, redirect); e != nil {
		t.Fatal(e)
	}
	if e = s.SubmitCodexBrowserRedirect("admin-1", c.SessionID, redirect); !errors.Is(e, ErrClosed) {
		t.Fatalf("replay: %v", e)
	}
	waitStatus(t, s, "admin-1", c.SessionID, StatusConnected)
	if a.submitted != redirect || string(saved) != "browser-credential" {
		t.Fatal("browser credential not saved")
	}
}

func TestBrowserConnectCanResumeAfterPageReload(t *testing.T) {
	a := &fakeBrowserAttempt{fakeAttempt: fakeAttempt{challenge: ConnectChallenge{AuthorizationURL: "https://auth.openai.com/oauth/authorize?state=resume", ExpiresAt: time.Now().Add(time.Minute)}, result: make(chan []byte, 1)}}
	s := NewService(fakeBrowserFlow{a}, nil)
	first, err := s.StartCodexBrowserConnect(context.Background(), "admin:session")
	if err != nil {
		t.Fatal(err)
	}
	current, err := s.CurrentCodexConnect("admin:session")
	if err != nil || current.SessionID != first.SessionID || current.AuthorizationURL != first.AuthorizationURL {
		t.Fatalf("reload lost pending challenge: %+v %v", current, err)
	}
	retried, err := s.StartCodexBrowserConnect(context.Background(), "admin:session")
	if err != nil || retried.SessionID != first.SessionID {
		t.Fatalf("retry started another flow: %+v %v", retried, err)
	}
	if _, err := s.CurrentCodexConnect("admin:other-session"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("other session recovered challenge: %v", err)
	}
	if _, err := s.StartCodexBrowserConnect(context.Background(), "admin:other-session"); !errors.Is(err, ErrBusy) {
		t.Fatalf("other session bypassed pending flow: %v", err)
	}
}

func TestBrowserConnectCanRestartAfterExpiry(t *testing.T) {
	a := &fakeBrowserAttempt{fakeAttempt: fakeAttempt{challenge: ConnectChallenge{AuthorizationURL: "https://auth.openai.com/oauth/authorize?state=expired", ExpiresAt: time.Now().Add(-time.Second)}, result: make(chan []byte, 1)}}
	b := &fakeBrowserAttempt{fakeAttempt: fakeAttempt{challenge: ConnectChallenge{AuthorizationURL: "https://auth.openai.com/oauth/authorize?state=fresh", ExpiresAt: time.Now().Add(time.Minute)}, result: make(chan []byte, 1)}}
	s := NewService(&sequentialBrowserFlow{first: a, second: b, starts: make(chan struct{})}, nil)
	first, err := s.StartCodexBrowserConnect(context.Background(), "admin")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CurrentCodexConnect("admin"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expired challenge still recoverable: %v", err)
	}
	if err := s.CancelCodexConnect(context.Background(), "admin", first.SessionID); !errors.Is(err, ErrClosed) {
		t.Fatalf("expired challenge cancellable: %v", err)
	}
	fresh, err := s.StartCodexBrowserConnect(context.Background(), "admin")
	if err != nil || fresh.SessionID == first.SessionID || fresh.AuthorizationURL != b.challenge.AuthorizationURL {
		t.Fatalf("expired challenge blocked retry: %+v %v", fresh, err)
	}
}

type delayedStopAttempt struct {
	challenge ConnectChallenge
	release   chan struct{}
	stopping  chan struct{}
}

func (a *delayedStopAttempt) Challenge() ConnectChallenge { return a.challenge }
func (a *delayedStopAttempt) SubmitRedirect(string) error { return nil }
func (a *delayedStopAttempt) Await(ctx context.Context) ([]byte, error) {
	<-ctx.Done()
	close(a.stopping)
	<-a.release
	return nil, ctx.Err()
}

type sequentialBrowserFlow struct {
	first  BrowserAttempt
	second BrowserAttempt
	starts chan struct{}
	count  int
}

func (f *sequentialBrowserFlow) Start(context.Context) (DeviceAttempt, error) {
	return nil, errors.New("device unused")
}
func (f *sequentialBrowserFlow) StartBrowser(context.Context) (BrowserAttempt, error) {
	f.count++
	if f.count == 1 {
		return f.first, nil
	}
	close(f.starts)
	return f.second, nil
}
func TestBrowserConnectWaitsForCancelledHelperBeforeRetry(t *testing.T) {
	first := &delayedStopAttempt{challenge: ConnectChallenge{AuthorizationURL: "https://auth.openai.com/oauth/authorize?state=first", ExpiresAt: time.Now().Add(time.Minute)}, release: make(chan struct{}), stopping: make(chan struct{})}
	second := &fakeBrowserAttempt{fakeAttempt: fakeAttempt{challenge: ConnectChallenge{AuthorizationURL: "https://auth.openai.com/oauth/authorize?state=second", ExpiresAt: time.Now().Add(time.Minute)}, result: make(chan []byte)}}
	flow := &sequentialBrowserFlow{first: first, second: second, starts: make(chan struct{})}
	s := NewService(flow, nil)
	c, err := s.StartCodexBrowserConnect(context.Background(), "admin")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.CancelCodexConnect(context.Background(), "admin", c.SessionID); err != nil {
		t.Fatal(err)
	}
	<-first.stopping
	done := make(chan error, 1)
	go func() { _, err := s.StartCodexBrowserConnect(context.Background(), "admin"); done <- err }()
	select {
	case <-flow.starts:
		t.Fatal("new helper started before cancelled helper exited")
	case <-time.After(10 * time.Millisecond):
	}
	close(first.release)
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("retry did not start after helper exit")
	}
}
