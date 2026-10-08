package traffic

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

type session struct {
	outcome Outcome
	gate    chan struct{}
	started chan struct{}
	once    sync.Once
	calls   int
	closed  bool
	mu      sync.Mutex
}

func (s *session) Operate(ctx context.Context, id string) Outcome {
	s.mu.Lock()
	s.calls++
	s.mu.Unlock()
	if s.started != nil {
		s.once.Do(func() { close(s.started) })
	}
	if s.gate != nil {
		select {
		case <-s.gate:
		case <-ctx.Done():
			return Timeout
		}
	}
	return s.outcome
}
func (s *session) Close() { s.closed = true }

func runOne(t *testing.T, result Outcome) []Event {
	t.Helper()
	ticks := make(chan time.Time)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s := &session{outcome: result}
	var events []Event
	var mu sync.Mutex
	done := make(chan error, 1)
	ready := make(chan struct{})
	completed := make(chan struct{})
	r := Runner{Config: Config{Sessions: 1, Idle: 0, Timeout: time.Second, ProcessID: "test"}, Ticks: ticks, Dial: func(context.Context) (Session, error) { return s, nil }, Emit: func(e Event) {
		mu.Lock()
		events = append(events, e)
		mu.Unlock()
		if e.Kind == "connected" {
			close(ready)
		}
		if e.Kind == "operation" {
			close(completed)
		}
	}}
	go func() { done <- r.Run(ctx) }()
	<-ready
	ticks <- time.Now()
	<-completed
	cancel()
	if e := <-done; e != nil {
		t.Fatal(e)
	}
	if s.calls != 1 || !s.closed {
		t.Fatalf("calls=%d closed=%v", s.calls, s.closed)
	}
	return events
}
func TestOutcomesAreRecordedWithoutRetry(t *testing.T) {
	for _, o := range []Outcome{Success, Failure, Timeout, Ambiguous} {
		t.Run(string(o), func(t *testing.T) {
			es := runOne(t, o)
			var n int
			for _, e := range es {
				if e.Kind == "operation" {
					n++
					if e.Outcome != o {
						t.Fatalf("%+v", e)
					}
				}
			}
			if n != 1 {
				t.Fatal(n)
			}
		})
	}
}
func TestIdleSessionsConnectButDoNotRunQueries(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	ticks := make(chan time.Time)
	ready := make(chan struct{})
	finished := make(chan struct{})
	var mu sync.Mutex
	n := 0
	ss := []*session{}
	done := make(chan error, 1)
	r := Runner{Config: Config{Sessions: 4, Idle: 1, Timeout: time.Second, ProcessID: "p"}, Ticks: ticks, Dial: func(context.Context) (Session, error) {
		mu.Lock()
		defer mu.Unlock()
		s := &session{outcome: Success}
		ss = append(ss, s)
		return s, nil
	}, Emit: func(e Event) {
		mu.Lock()
		defer mu.Unlock()
		if e.Kind == "connected" {
			n++
			if n == 4 {
				close(ready)
			}
		}
		if e.Kind == "operation" {
			n--
			if n == 1 {
				close(finished)
			}
		}
	}}
	go func() { done <- r.Run(ctx) }()
	<-ready
	ticks <- time.Now()
	<-finished
	cancel()
	<-done
	total := 0
	for _, s := range ss {
		total += s.calls
		if !s.closed {
			t.Fatal("leaked session")
		}
	}
	if total != 3 {
		t.Fatal(total)
	}
}
func TestShutdownFinishesInflightOperation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	ticks := make(chan time.Time)
	s := &session{outcome: Success, gate: make(chan struct{}), started: make(chan struct{})}
	ready := make(chan struct{})
	done := make(chan error, 1)
	var outcome Outcome
	r := Runner{Config: Config{Sessions: 1, Timeout: time.Second, ProcessID: "p"}, Ticks: ticks, Dial: func(context.Context) (Session, error) { return s, nil }, Emit: func(e Event) {
		if e.Kind == "connected" {
			close(ready)
		}
		if e.Kind == "operation" {
			outcome = e.Outcome
		}
	}}
	go func() { done <- r.Run(ctx) }()
	<-ready
	ticks <- time.Now()
	<-s.started
	cancel()
	select {
	case <-done:
		t.Fatal("abandoned in-flight operation")
	case <-time.After(10 * time.Millisecond):
	}
	close(s.gate)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if outcome != Success || !s.closed {
		t.Fatal(outcome)
	}
}
func TestDeadlineRecordsTimeout(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ticks := make(chan time.Time)
	ready := make(chan struct{})
	result := make(chan Event, 1)
	done := make(chan error, 1)
	s := &session{gate: make(chan struct{})}
	r := Runner{Config: Config{Sessions: 1, Timeout: 10 * time.Millisecond, ProcessID: "p"}, Ticks: ticks, Dial: func(context.Context) (Session, error) { return s, nil }, Emit: func(e Event) {
		if e.Kind == "connected" {
			close(ready)
		}
		if e.Kind == "operation" {
			result <- e
		}
	}}
	go func() { done <- r.Run(ctx) }()
	<-ready
	ticks <- time.Now()
	e := <-result
	cancel()
	<-done
	if e.Outcome != Timeout {
		t.Fatal(e)
	}
}
func TestDialErrorDoesNotLeakSecret(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ticks := make(chan time.Time)
	failed := make(chan Event, 1)
	done := make(chan error, 1)
	r := Runner{Config: Config{Sessions: 1, Timeout: time.Second, ProcessID: "p"}, Ticks: ticks, Dial: func(context.Context) (Session, error) { return nil, errors.New("secret dsn") }, Emit: func(e Event) {
		if e.Kind == "connection_failure" {
			failed <- e
		}
	}}
	go func() { done <- r.Run(ctx) }()
	<-failed
	cancel()
	<-done
}

func TestIdleInitialDialFailureRecovers(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ticks := make(chan time.Time)
	failed := make(chan struct{})
	recovered := make(chan struct{})
	done := make(chan error, 1)
	calls := 0
	idle := &session{outcome: Success}
	r := Runner{Config: Config{Sessions: 1, Idle: 1, Timeout: time.Second, ProcessID: "idle"}, Ticks: ticks, Dial: func(context.Context) (Session, error) {
		calls++
		if calls == 1 {
			return nil, errors.New("transient")
		}
		return idle, nil
	}, Emit: func(e Event) {
		if e.Kind == "connection_failure" {
			close(failed)
		}
		if e.Kind == "connected" {
			close(recovered)
		}
	}}
	go func() { done <- r.Run(ctx) }()
	<-failed
	ticks <- time.Now()
	select {
	case <-recovered:
	case <-time.After(100 * time.Millisecond):
		cancel()
		<-done
		t.Fatal("idle connection never retried")
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if idle.calls != 0 || !idle.closed {
		t.Fatal("idle connection must stay idle and close")
	}
}
