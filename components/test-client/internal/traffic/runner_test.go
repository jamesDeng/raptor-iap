package traffic

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
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

func TestClosedTicksDrainScheduledQueueWithoutSkipped(t *testing.T) {
	ticks := make(chan time.Time)
	s := &session{outcome: Success, gate: make(chan struct{}), started: make(chan struct{})}
	var events []Event
	scheduled := make(chan struct{}, 2)
	done := make(chan error, 1)
	r := Runner{Config: Config{Sessions: 1, Timeout: time.Second, ProcessID: "p"}, Ticks: ticks, Dial: func(context.Context) (Session, error) { return s, nil }, Emit: func(e Event) {
		events = append(events, e)
		if e.Kind == "scheduled" {
			scheduled <- struct{}{}
		}
	}}
	go func() { done <- r.Run(context.Background()) }()
	ticks <- time.Now()
	<-s.started
	ticks <- time.Now()
	<-scheduled
	<-scheduled
	close(ticks)
	close(s.gate)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	counts := map[string]int{}
	for _, e := range events {
		counts[e.Kind]++
	}
	if counts["scheduled"] != 2 || counts["operation"] != 2 || counts["skipped"] != 0 || counts["connected"]-counts["disconnected"] != 0 || !s.closed {
		t.Fatal(counts)
	}
}

// Cancellation is an admission stop, not permission to discard accepted work.
func TestCancellationDrainsAcceptedQueueAndReconnect(t *testing.T) {
	for _, first := range []Outcome{Success, Failure, Timeout} {
		t.Run(string(first), func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			ticks := make(chan time.Time)
			active := &session{outcome: first, gate: make(chan struct{}), started: make(chan struct{})}
			next := &session{outcome: Success}
			var events []Event
			scheduled := make(chan struct{}, 2)
			done := make(chan error, 1)
			dials := 0
			r := Runner{Config: Config{Sessions: 1, Timeout: 50 * time.Millisecond, ProcessID: "p"}, Ticks: ticks,
				Dial: func(c context.Context) (Session, error) {
					dials++
					if dials == 1 {
						return active, nil
					}
					if c.Err() != nil {
						return nil, c.Err()
					}
					return next, nil
				}, Emit: func(e Event) {
					events = append(events, e)
					if e.Kind == "scheduled" {
						scheduled <- struct{}{}
					}
				}}
			go func() { done <- r.Run(ctx) }()
			ticks <- time.Now()
			<-active.started
			ticks <- time.Now()
			<-scheduled
			<-scheduled
			cancel()
			if first != Timeout {
				close(active.gate)
			}
			select {
			case err := <-done:
				if err != nil {
					t.Fatal(err)
				}
			case <-time.After(3 * time.Second):
				t.Fatal("drain exceeded finite operation budget")
			}
			counts := map[string]int{}
			var outcomes []Outcome
			for _, e := range events {
				counts[e.Kind]++
				if e.Kind == "operation" {
					outcomes = append(outcomes, e.Outcome)
				}
			}
			if counts["scheduled"] != 2 || counts["operation"] != 2 || counts["skipped"] != 0 || counts["connected"] != counts["disconnected"] || len(outcomes) != 2 || outcomes[0] != first || outcomes[1] != Success {
				t.Fatalf("counts=%v outcomes=%v", counts, outcomes)
			}
			if !active.closed || first != Success && !next.closed {
				t.Fatal("accepted sessions leaked")
			}
		})
	}
}

func TestCancellationDuringIdleDialStopsAdmission(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ticks := make(chan time.Time)
	active := &session{outcome: Success, gate: make(chan struct{}), started: make(chan struct{})}
	entered := make(chan struct{})
	done := make(chan error, 1)
	var events []Event
	scheduled := make(chan struct{}, 2)
	dials := 0
	r := Runner{Config: Config{Sessions: 2, Idle: 1, Timeout: time.Second, ProcessID: "p"}, Ticks: ticks,
		Dial: func(c context.Context) (Session, error) {
			dials++
			if dials == 1 {
				return active, nil
			}
			if dials == 5 {
				close(entered)
				<-c.Done()
			}
			return nil, errors.New("idle unavailable")
		}, Emit: func(e Event) {
			events = append(events, e)
			if e.Kind == "scheduled" {
				scheduled <- struct{}{}
			}
		}}
	go func() { done <- r.Run(ctx) }()
	ticks <- time.Now()
	<-scheduled
	<-active.started
	ticks <- time.Now()
	<-scheduled
	ticks <- time.Now()
	<-entered
	cancel()
	close(active.gate)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	counts := map[string]int{}
	for _, e := range events {
		counts[e.Kind]++
	}
	if counts["scheduled"] != 2 || counts["operation"] != 2 || counts["skipped"] != 0 {
		t.Fatal("idle cancellation changed admitted work", counts)
	}
}

type deadlineSession struct{ deadline time.Time }

func (s *deadlineSession) Operate(ctx context.Context, _ string) Outcome {
	deadline, ok := ctx.Deadline()
	if !ok || !deadline.Equal(s.deadline) {
		return Failure
	}
	return Success
}
func (*deadlineSession) Close() {}

func TestReconnectSharesOperationDeadline(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ticks := make(chan time.Time)
	done := make(chan error, 1)
	var outcome Outcome
	dials := 0
	r := Runner{Config: Config{Sessions: 1, Timeout: time.Second, ProcessID: "p"}, Ticks: ticks,
		Dial: func(c context.Context) (Session, error) {
			dials++
			if dials == 1 {
				return nil, errors.New("initial connection unavailable")
			}
			deadline, ok := c.Deadline()
			if !ok {
				return nil, errors.New("unbounded reconnect")
			}
			return &deadlineSession{deadline: deadline}, nil
		}, Emit: func(e Event) {
			if e.Kind == "operation" {
				outcome = e.Outcome
				cancel()
			}
		}}
	go func() { done <- r.Run(ctx) }()
	ticks <- time.Now()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if outcome != Success {
		t.Fatal("reconnect extended the accepted operation deadline", outcome)
	}
}

// A tick may already be inside an idle reconnect when finish is accepted.
// It must not schedule another operation into the full worker queue afterward.
func TestFinishDuringTickWithFullQueue(t *testing.T) {
	ticks := make(chan time.Time)
	active := &session{outcome: Success, gate: make(chan struct{}), started: make(chan struct{})}
	reconnect := make(chan struct{})
	release := make(chan struct{})
	scheduled := make(chan struct{}, 3)
	done := make(chan error, 1)
	var events []Event
	var finishing atomic.Bool
	refused := make(chan struct{})
	skipped := make(chan struct{}, 1)
	calls := 0
	r := Runner{Config: Config{Sessions: 2, Idle: 1, Timeout: time.Second, ProcessID: "p"}, Ticks: ticks,
		Admit: func(schedule func()) bool {
			if finishing.Load() {
				close(refused)
				return false
			}
			schedule()
			return true
		},
		Dial: func(context.Context) (Session, error) {
			calls++
			if calls == 1 {
				return active, nil
			}
			if calls == 5 {
				close(reconnect)
				<-release
			}
			return nil, errors.New("idle unavailable")
		},
		Emit: func(e Event) {
			events = append(events, e)
			if e.Kind == "skipped" {
				skipped <- struct{}{}
			}
			if e.Kind == "scheduled" {
				scheduled <- struct{}{}
			}
		},
	}
	go func() { done <- r.Run(context.Background()) }()
	ticks <- time.Now()
	<-scheduled
	<-active.started
	ticks <- time.Now()
	<-scheduled
	ticks <- time.Now()
	<-reconnect
	finishing.Store(true)
	close(release)
	select {
	case <-refused:
	case <-skipped:
	case <-time.After(time.Second):
		t.Fatal("scheduler did not reach admission")
	}
	close(ticks)
	close(active.gate)
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("finish did not stop admitted tick")
	}
	counts := map[string]int{}
	for _, e := range events {
		counts[e.Kind]++
	}
	if counts["scheduled"] != 2 || counts["operation"] != 2 || counts["skipped"] != 0 || counts["connected"] != counts["disconnected"] {
		t.Fatal(counts)
	}
}
