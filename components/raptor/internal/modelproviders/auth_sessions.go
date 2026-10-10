package modelproviders

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
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
type SaveCredential func(context.Context, []byte) error

type connectSession struct {
	owner  string
	status ConnectStatus
	cancel context.CancelFunc
}
type Service struct {
	flow     DeviceFlow
	save     SaveCredential
	mu       sync.Mutex
	starting bool
	sessions map[string]*connectSession
}

func NewService(flow DeviceFlow, save SaveCredential) *Service {
	return &Service{flow: flow, save: save, sessions: map[string]*connectSession{}}
}

func (s *Service) StartCodexConnect(ctx context.Context, adminID string) (ConnectChallenge, error) {
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
	s.mu.Unlock()
	defer func() { s.mu.Lock(); s.starting = false; s.mu.Unlock() }()
	attempt, e := s.flow.Start(ctx)
	if e != nil {
		return ConnectChallenge{}, e
	}
	challenge := attempt.Challenge()
	if challenge.VerificationURL == "" || challenge.UserCode == "" || challenge.ExpiresAt.IsZero() {
		return ConnectChallenge{}, errors.New("invalid device challenge")
	}
	var id [16]byte
	if _, e = rand.Read(id[:]); e != nil {
		return ConnectChallenge{}, e
	}
	challenge.SessionID = hex.EncodeToString(id[:])
	runCtx, cancel := context.WithDeadline(context.Background(), challenge.ExpiresAt)
	v := &connectSession{owner: adminID, status: ConnectStatus{SessionID: challenge.SessionID, Status: StatusPending, ExpiresAt: challenge.ExpiresAt}, cancel: cancel}
	s.mu.Lock()
	s.sessions[challenge.SessionID] = v
	s.mu.Unlock()
	go s.await(runCtx, v, attempt)
	return challenge, nil
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
	if s.save == nil || s.save(context.Background(), credential) != nil {
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
