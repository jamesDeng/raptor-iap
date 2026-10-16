// Package control provides identity-bound, local-only traffic finalization.
package control

import (
	"fmt"
	"sync"
)

type Status string

const (
	Accepted  Status = "accepted"
	Finishing Status = "finishing"
	Final     Status = "final"
)

type Controller struct {
	mu                sync.Mutex
	podUID, processID string
	done              chan struct{}
	state             Status
}

func New(podUID, processID string) (*Controller, error) {
	if podUID == "" || processID == "" {
		return nil, fmt.Errorf("control identity required")
	}
	return &Controller{podUID: podUID, processID: processID, done: make(chan struct{})}, nil
}
func (c *Controller) Done() <-chan struct{} { return c.done }
func (c *Controller) MarkFinal()            { c.mu.Lock(); defer c.mu.Unlock(); c.state = Final }
func (c *Controller) finish(uid, pid string) (Status, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if uid != c.podUID || pid != c.processID {
		return "", fmt.Errorf("control identity mismatch")
	}
	if c.state == "" {
		c.state = Finishing
		close(c.done)
		return Accepted, nil
	}
	return c.state, nil
}
