// Package traffic emits scheduled work and outcomes without concealing retries.
package traffic

import (
	"context"
	"fmt"
	"sync"
	"time"
)

type Outcome string

const (
	Success   Outcome = "success"
	Failure   Outcome = "failure"
	Timeout   Outcome = "timeout"
	Ambiguous Outcome = "ambiguous"
)

type Event struct {
	ProcessID       string    `json:"processId"`
	Sequence        uint64    `json:"sequence"`
	At              time.Time `json:"at"`
	Kind            string    `json:"kind"`
	Outcome         Outcome   `json:"outcome,omitempty"`
	OperationID     string    `json:"operationId,omitempty"`
	DurationSeconds float64   `json:"durationSeconds,omitempty"`
}
type Session interface {
	Operate(context.Context, string) Outcome
	Close()
}
type Config struct {
	Sessions, Idle    int
	Timeout, Interval time.Duration
	ProcessID         string
}
type Runner struct {
	Config Config
	Ticks  <-chan time.Time
	Dial   func(context.Context) (Session, error)
	Emit   func(Event)
}

// Run stops scheduling on cancellation, but lets in-flight operations finish
// under their own finite deadlines. It never retries a logical SQL operation.
func (r Runner) Run(ctx context.Context) error {
	c := r.Config
	if c.Sessions < 1 || c.Sessions > 64 || c.Idle < 0 || c.Idle > c.Sessions || c.Timeout <= 0 || c.Timeout > time.Minute || c.ProcessID == "" || r.Dial == nil || r.Emit == nil {
		return fmt.Errorf("invalid traffic configuration")
	}
	ticks := r.Ticks
	if ticks == nil {
		if c.Interval <= 0 {
			return fmt.Errorf("interval must be positive")
		}
		timer := time.NewTicker(c.Interval)
		defer timer.Stop()
		ticks = timer.C
	}
	var mu sync.Mutex
	var seq uint64
	emit := func(e Event) {
		mu.Lock()
		defer mu.Unlock()
		seq++
		e.ProcessID = c.ProcessID
		e.Sequence = seq
		e.At = time.Now().UTC()
		r.Emit(e)
	}
	dial := func() Session {
		dctx, cancel := context.WithTimeout(ctx, c.Timeout)
		defer cancel()
		s, err := r.Dial(dctx)
		if err != nil || s == nil {
			emit(Event{Kind: "connection_failure"})
			return nil
		}
		emit(Event{Kind: "connected"})
		return s
	}
	sessions := make([]Session, c.Sessions)
	for i := range sessions {
		if ctx.Err() != nil {
			break
		}
		sessions[i] = dial()
	}
	var wg sync.WaitGroup
	jobs := make([]chan string, c.Sessions-c.Idle)
	for i := range jobs {
		jobs[i] = make(chan string, 1)
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			s := sessions[i]
			defer func() {
				if s != nil {
					s.Close()
					emit(Event{Kind: "disconnected"})
				}
			}()
			for id := range jobs[i] {
				if ctx.Err() != nil {
					emit(Event{Kind: "skipped", OperationID: id})
					continue
				}
				if s == nil {
					s = dial()
				}
				start := time.Now()
				out := Failure
				if s != nil {
					opctx, cancel := context.WithTimeout(context.Background(), c.Timeout)
					out = s.Operate(opctx, id)
					cancel()
				}
				emit(Event{Kind: "operation", OperationID: id, Outcome: out, DurationSeconds: time.Since(start).Seconds()})
				if out != Success && s != nil {
					s.Close()
					s = nil
					emit(Event{Kind: "disconnected"})
				}
			}
		}(i)
	}
	defer func() {
		for _, j := range jobs {
			close(j)
		}
		wg.Wait()
		for i := len(jobs); i < len(sessions); i++ {
			if sessions[i] != nil {
				sessions[i].Close()
				emit(Event{Kind: "disconnected"})
			}
		}
	}()
	var batch uint64
	for {
		select {
		case <-ctx.Done():
			return nil
		case _, ok := <-ticks:
			if !ok {
				return nil
			}
			if ctx.Err() != nil {
				return nil
			}
			// Retry only missing idle connections, at most once per scheduling tick.
			// Failed dials remain observable; this never retries a SQL operation.
			for i := len(jobs); i < len(sessions); i++ {
				if sessions[i] == nil && ctx.Err() == nil {
					sessions[i] = dial()
				}
			}
			batch++
			for i, j := range jobs {
				id := fmt.Sprintf("%s-%d-%d", c.ProcessID, i, batch)
				emit(Event{Kind: "scheduled", OperationID: id})
				select {
				case j <- id:
				default:
					emit(Event{Kind: "skipped", OperationID: id})
				}
			}
		}
	}
}
