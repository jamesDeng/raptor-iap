package modelproviders

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"strings"
	"sync"
	"time"
)

type DeviceAttempt interface {
	Challenge() ConnectChallenge
	Await(context.Context) ([]byte, error)
}
type DeviceFlow interface {
	Start(context.Context) (DeviceAttempt, error)
}
type BrowserAttempt interface {
	DeviceAttempt
	SubmitRedirect(string) error
}
type BrowserFlow interface {
	StartBrowser(context.Context) (BrowserAttempt, error)
}
type SaveCredential func(context.Context, string, []byte) error

type connectSession struct {
	owner     string
	status    ConnectStatus
	cancel    context.CancelFunc
	attempt   BrowserAttempt
	submitted bool
}
type Service struct {
	flow     DeviceFlow
	save     SaveCredential
	mu       sync.Mutex
	starting bool
	epoch    uint64
	sessions map[string]*connectSession
}

func NewService(flow DeviceFlow, save SaveCredential) *Service {
	return &Service{flow: flow, save: save, sessions: map[string]*connectSession{}}
}

func (s *Service) StartCodexConnect(ctx context.Context, adminID string) (ConnectChallenge, error) {
	return s.start(ctx, adminID, false)
}
func (s *Service) StartCodexBrowserConnect(ctx context.Context, adminID string) (ConnectChallenge, error) {
	return s.start(ctx, adminID, true)
}
func (s *Service) start(ctx context.Context, adminID string, browser bool) (ConnectChallenge, error) {
	if adminID == "" {
		return ConnectChallenge{}, ErrForbidden
	}
	s.mu.Lock()
	if s.starting {
		s.mu.Unlock()
		return ConnectChallenge{}, ErrBusy
	}
	for _, v := range s.sessions {
		if v.status.Status == StatusPending {
			s.mu.Unlock()
			return ConnectChallenge{}, ErrBusy
		}
	}
	s.starting = true
	startEpoch := s.epoch
	s.mu.Unlock()
	defer func() { s.mu.Lock(); s.starting = false; s.mu.Unlock() }()
	var attempt DeviceAttempt
	var browserAttempt BrowserAttempt
	var e error
	if browser {
		flow, ok := s.flow.(BrowserFlow)
		if !ok {
			return ConnectChallenge{}, errors.New("browser flow unavailable")
		}
		browserAttempt, e = flow.StartBrowser(ctx)
		attempt = browserAttempt
	} else {
		attempt, e = s.flow.Start(ctx)
	}
	if e != nil {
		return ConnectChallenge{}, e
	}
	challenge := attempt.Challenge()
	if (browser && challenge.AuthorizationURL == "") || (!browser && (challenge.VerificationURL == "" || challenge.UserCode == "")) || challenge.ExpiresAt.IsZero() {
		return ConnectChallenge{}, errors.New("invalid device challenge")
	}
	var id [16]byte
	if _, e = rand.Read(id[:]); e != nil {
		return ConnectChallenge{}, e
	}
	challenge.SessionID = hex.EncodeToString(id[:])
	runCtx, cancel := context.WithDeadline(context.Background(), challenge.ExpiresAt)
	v := &connectSession{owner: adminID, status: ConnectStatus{SessionID: challenge.SessionID, Status: StatusPending, ExpiresAt: challenge.ExpiresAt}, cancel: cancel, attempt: browserAttempt}
	s.mu.Lock()
	if s.epoch != startEpoch {
		s.mu.Unlock()
		cancel()
		go attempt.Await(runCtx)
		return ConnectChallenge{}, ErrClosed
	}
	s.sessions[challenge.SessionID] = v
	s.mu.Unlock()
	go s.await(runCtx, v, attempt)
	return challenge, nil
}
func (s *Service) SubmitCodexBrowserRedirect(adminID, sessionID, redirect string) error {
	s.mu.Lock()
	v := s.sessions[sessionID]
	if v == nil {
		s.mu.Unlock()
		return ErrNotFound
	}
	if v.owner != adminID {
		s.mu.Unlock()
		return ErrForbidden
	}
	if v.status.Status != StatusPending || v.attempt == nil || v.submitted || time.Now().After(v.status.ExpiresAt) {
		s.mu.Unlock()
		return ErrClosed
	}
	// Hold the session lock through the bounded pipe write so concurrent submissions cannot race.
	e := v.attempt.SubmitRedirect(redirect)
	if e == nil {
		v.submitted = true
	}
	s.mu.Unlock()
	return e
}
func (s *Service) await(ctx context.Context, v *connectSession, attempt DeviceAttempt) {
	credential, e := attempt.Await(ctx)
	s.mu.Lock()
	defer s.mu.Unlock()
	if v.status.Status != StatusPending {
		return
	}
	if ctx.Err() == context.DeadlineExceeded || time.Now().After(v.status.ExpiresAt) {
		v.status.Status = StatusExpired
		v.cancel()
		return
	}
	if ctx.Err() != nil {
		v.status.Status = StatusCancelled
		v.cancel()
		return
	}
	if e != nil || len(credential) == 0 {
		v.status.Status = StatusFailed
		v.status.ErrorCode = "ProviderAuthorizationFailed"
		v.cancel()
		return
	}
	if s.save == nil || s.save(context.Background(), strings.SplitN(v.owner, ":", 2)[0], credential) != nil {
		v.status.Status = StatusFailed
		v.status.ErrorCode = "CredentialSaveFailed"
		v.cancel()
		return
	}
	v.status.Status = StatusConnected
	v.cancel()
}
func (s *Service) PollCodexConnect(_ context.Context, adminID, sessionID string) (ConnectStatus, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v := s.sessions[sessionID]
	if v == nil {
		return ConnectStatus{}, ErrNotFound
	}
	if v.owner != adminID {
		return ConnectStatus{}, ErrForbidden
	}
	return v.status, nil
}
func (s *Service) CancelCodexConnect(_ context.Context, adminID, sessionID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	v := s.sessions[sessionID]
	if v == nil {
		return ErrNotFound
	}
	if v.owner != adminID {
		return ErrForbidden
	}
	if v.status.Status != StatusPending {
		return ErrClosed
	}
	v.status.Status = StatusCancelled
	v.cancel()
	return nil
}

func (s *Service) CancelPending() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.epoch++
	for _, v := range s.sessions {
		if v.status.Status == StatusPending {
			v.status.Status = StatusCancelled
			v.cancel()
		}
	}
}
