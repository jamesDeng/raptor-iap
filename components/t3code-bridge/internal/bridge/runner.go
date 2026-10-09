package bridge

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/jamesDeng/raptor-iap/components/t3code-bridge/internal/config"
	"github.com/jamesDeng/raptor-iap/components/t3code-bridge/internal/raptor"
	"github.com/jamesDeng/raptor-iap/components/t3code-bridge/internal/state"
	"net/url"
	"sync"
	"time"
)

var ErrBusy = errors.New("SessionBusy")
var ErrUnresolved = errors.New("RequestUnresolved")
var ErrBinding = errors.New("RequestBindingMismatch")

type Client interface {
	Login(context.Context) (string, error)
	Create(context.Context, string, json.RawMessage) (raptor.Request, error)
	View(context.Context, string) (raptor.RequestView, error)
	Events(context.Context, string, int64) (raptor.Timeline, error)
	Cancel(context.Context, string) error
}
type Update struct{ Kind, Text, ID, Status string }
type Outcome struct{ Status string }
type Runner struct {
	client                                 Client
	store                                  *state.Store
	cfg                                    config.Config
	binding                                state.Binding
	mu                                     sync.Mutex
	busy                                   map[string]bool
	pollInterval, deadline, cancelDeadline time.Duration
}

func New(c Client, s *state.Store, cfg config.Config) *Runner {
	return &Runner{client: c, store: s, cfg: cfg, busy: map[string]bool{}, pollInterval: 2 * time.Second, deadline: 10 * time.Minute, cancelDeadline: 60 * time.Second}
}
func (r *Runner) authenticate(ctx context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.binding.UserID != "" {
		return nil
	}
	u, e := r.client.Login(ctx)
	if e != nil {
		return e
	}
	r.binding = state.Binding{Origin: r.cfg.Origin, UserID: u, App: r.cfg.ApplicationCode, Env: r.cfg.EnvironmentCode, Model: r.cfg.Model}
	return nil
}
func (r *Runner) NewSession(ctx context.Context) (string, error) {
	if e := r.authenticate(ctx); e != nil {
		return "", e
	}
	v, e := r.store.Create(r.binding)
	return v.ID, e
}
func (r *Runner) enter(id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.busy[id] {
		return ErrBusy
	}
	r.busy[id] = true
	return nil
}
func (r *Runner) leave(id string) { r.mu.Lock(); delete(r.busy, id); r.mu.Unlock() }
func (r *Runner) Prompt(ctx context.Context, id, text string, emit func(Update) error) (Outcome, error) {
	return r.PromptReady(ctx, id, text, emit, func() {})
}

// PromptReady admits the prompt durably before the ACP reader handles cancellation.
func (r *Runner) PromptReady(ctx context.Context, id, text string, emit func(Update) error, ready func()) (Outcome, error) {
	var once sync.Once
	admitted := func() { once.Do(ready) }
	defer admitted()
	if e := r.authenticate(ctx); e != nil {
		return Outcome{}, e
	}
	if e := r.enter(id); e != nil {
		return Outcome{}, e
	}
	defer r.leave(id)
	payload, e := raptor.QuestionPayload(r.cfg, text)
	if e != nil {
		return Outcome{}, e
	}
	_, e = r.store.Update(id, r.binding, func(s *state.Session) error {
		if s.Key != "" && !s.Terminal {
			return ErrUnresolved
		}
		s.Key = state.ID()
		s.Payload = payload
		s.RequestID = ""
		s.AttemptID = ""
		s.Cursor = 0
		s.CancelIntent = false
		s.CancelSent = false
		s.Terminal = false
		s.Outcome = ""
		s.Transcript = append(s.Transcript, state.Message{Role: "user", Text: text})
		return nil
	})
	if e != nil {
		return Outcome{}, e
	}
	admitted()
	return r.observe(ctx, id, emit)
}
func (r *Runner) LoadSession(ctx context.Context, id string, emit func(Update) error) error {
	if e := r.authenticate(ctx); e != nil {
		return e
	}
	if e := r.enter(id); e != nil {
		return e
	}
	defer r.leave(id)
	s, e := r.store.Load(id, r.binding)
	if e != nil {
		return e
	}
	for _, m := range s.Transcript {
		kind := "text"
		if m.Role == "progress" {
			kind = "progress"
		}
		if m.Role == "user" {
			kind = "user"
		}
		if e = emit(Update{Kind: kind, Text: m.Text, ID: m.ID, Status: m.Status}); e != nil {
			return e
		}
	}
	if s.Key != "" && !s.Terminal {
		_, e = r.observe(ctx, id, emit)
	}
	return e
}
func (r *Runner) Cancel(ctx context.Context, id string) error {
	if e := r.authenticate(ctx); e != nil {
		return e
	}
	s, e := r.store.Update(id, r.binding, func(s *state.Session) error {
		if !s.Terminal {
			s.CancelIntent = true
		}
		return nil
	})
	if e != nil {
		return e
	}
	r.mu.Lock()
	busy := r.busy[id]
	r.mu.Unlock()
	if !busy && s.RequestID != "" && !s.Terminal {
		return r.client.Cancel(ctx, s.RequestID)
	}
	return nil
}
func (r *Runner) message(id, text string, emit func(Update) error) error {
	_, e := r.store.Update(id, r.binding, func(s *state.Session) error {
		s.Transcript = append(s.Transcript, state.Message{Role: "assistant", Text: text})
		return nil
	})
	if e != nil {
		return e
	}
	return emit(Update{Kind: "text", Text: text})
}
func (r *Runner) observe(ctx context.Context, id string, emit func(Update) error) (Outcome, error) {
	ctx, cancel := context.WithTimeout(ctx, r.deadline)
	defer cancel()
	s, e := r.store.Load(id, r.binding)
	if e != nil {
		return Outcome{}, e
	}
	if s.RequestID == "" {
		q, e := r.client.Create(ctx, s.Key, s.Payload)
		if e != nil {
			return Outcome{}, e
		}
		if !requestMatches(q, s) {
			return Outcome{}, ErrBinding
		}
		s, e = r.store.Update(id, r.binding, func(s *state.Session) error { s.RequestID = q.ID; return nil })
		if e != nil {
			return Outcome{}, e
		}
		if e = r.message(id, "Request "+s.RequestID+" · "+r.cfg.ApplicationCode+" / "+r.cfg.EnvironmentCode+"\n"+r.cfg.Origin+"/#request="+url.QueryEscape(s.RequestID), emit); e != nil {
			return Outcome{}, e
		}
	}
	if e = r.progress(id, Update{Kind: "progress", ID: s.RequestID, Text: "Raptor request", Status: "in_progress"}, emit); e != nil {
		return Outcome{}, e
	}
	var cancelAt time.Time
	for {
		if ctx.Err() != nil {
			return Outcome{Status: "unresolved"}, ErrUnresolved
		}
		s, e = r.store.Load(id, r.binding)
		if e != nil {
			return Outcome{}, e
		}
		if s.CancelIntent {
			if cancelAt.IsZero() {
				cancelAt = time.Now()
			}
			if time.Since(cancelAt) > r.cancelDeadline {
				return Outcome{Status: "unresolved"}, ErrUnresolved
			}
			if !s.CancelSent {
				if e = r.client.Cancel(ctx, s.RequestID); e == nil {
					_, e = r.store.Update(id, r.binding, func(s *state.Session) error { s.CancelSent = true; return nil })
					if e != nil {
						return Outcome{}, e
					}
				} else if errors.Is(e, raptor.ErrLogin) {
					return Outcome{}, e
				}
			}
		}
		v, e := r.client.View(ctx, s.RequestID)
		if e != nil {
			return Outcome{}, e
		}
		if !requestMatches(v.Request, s) {
			return Outcome{}, ErrBinding
		}
		x := v.Execution
		if x != nil {
			if x.RequestID != "" && x.RequestID != s.RequestID {
				return Outcome{}, ErrBinding
			}
			if x.AttemptID != "" {
				s, e = r.store.Update(id, r.binding, func(s *state.Session) error {
					if s.AttemptID != "" && s.AttemptID != x.AttemptID {
						return ErrBinding
					}
					s.AttemptID = x.AttemptID
					return nil
				})
				if e != nil {
					return Outcome{}, e
				}
			}
		}
		timeline, e := r.client.Events(ctx, s.RequestID, s.Cursor)
		if e != nil {
			return Outcome{}, e
		}
		cursor := s.Cursor
		for _, ev := range timeline.Events {
			if ev.RequestID != s.RequestID || ev.Sequence <= cursor || (ev.AttemptID != "" && s.AttemptID != "" && ev.AttemptID != s.AttemptID) {
				return Outcome{}, ErrBinding
			}
			cursor = ev.Sequence
			if e = r.progress(id, Update{Kind: "progress", ID: s.RequestID, Text: ev.Summary, Status: "in_progress"}, emit); e != nil {
				return Outcome{}, e
			}
		}
		if cursor != s.Cursor {
			_, e = r.store.Update(id, r.binding, func(s *state.Session) error { s.Cursor = cursor; return nil })
			if e != nil {
				return Outcome{}, e
			}
		}
		if v.ExecutionAvailable && x != nil {
			switch x.Status {
			case "completed":
				if e = validResult(s, v); e != nil {
					return Outcome{}, e
				}
				if e = r.message(id, x.Result.Answer, emit); e != nil {
					return Outcome{}, e
				}
				return r.finish(id, "completed", emit)
			case "cancelled":
				if !x.CleanupStatus.Confirmed() || x.RecoveryNeeded {
					return Outcome{}, ErrUnresolved
				}
				return r.finish(id, "cancelled", emit)
			case "failed", "blocked", "interrupted":
				if e = r.message(id, "Raptor request "+x.Status+". Inspect its request page for recovery and cleanup status.", emit); e != nil {
					return Outcome{}, e
				}
				if x.RecoveryNeeded || !x.CleanupStatus.Confirmed() {
					return Outcome{Status: "unresolved"}, ErrUnresolved
				}
				return r.finish(id, "failed", emit)
			}
		}
		timer := time.NewTimer(r.pollInterval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return Outcome{Status: "unresolved"}, ErrUnresolved
		case <-timer.C:
		}
	}
}
func (r *Runner) finish(id, status string, emit func(Update) error) (Outcome, error) {
	toolStatus := "failed"
	if status == "completed" {
		toolStatus = "completed"
	}
	s, e := r.store.Update(id, r.binding, func(s *state.Session) error {
		s.Terminal = true
		s.Outcome = status
		s.Transcript = append(s.Transcript, state.Message{Role: "progress", Text: status, ID: s.RequestID, Status: toolStatus})
		return nil
	})
	if e != nil {
		return Outcome{}, e
	}
	if e = emit(Update{Kind: "progress", ID: s.RequestID, Text: status, Status: toolStatus}); e != nil {
		return Outcome{}, e
	}
	if status == "failed" {
		return Outcome{Status: status}, errors.New("RaptorRequestFailed")
	}
	return Outcome{Status: status}, nil
}

func (r *Runner) progress(id string, u Update, emit func(Update) error) error {
	_, e := r.store.Update(id, r.binding, func(s *state.Session) error {
		s.Transcript = append(s.Transcript, state.Message{Role: "progress", Text: u.Text, ID: u.ID, Status: u.Status})
		return nil
	})
	if e != nil {
		return e
	}
	return emit(u)
}
