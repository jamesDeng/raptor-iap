package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/jamesDeng/raptor-iap/components/agent-gateway/internal/execution"
	"os"
	"path/filepath"
	"sync"
)

type RuntimeHandle = execution.RuntimeHandle
type Simulated struct {
	Root      string
	mu        sync.Mutex
	active    map[string]RuntimeHandle
	MaxActive int
}

func (s *Simulated) Start(ctx context.Context, in execution.ExecutionInput) (RuntimeHandle, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if ctx.Err() != nil {
		return RuntimeHandle{}, ctx.Err()
	}
	if s.active == nil {
		s.active = map[string]RuntimeHandle{}
	}
	if in.RequestID == "" || len(s.active) > 0 {
		return RuntimeHandle{}, errors.New("runtime busy")
	}
	h := RuntimeHandle{ID: execution.NewID(), RequestID: in.RequestID}
	s.active[h.ID] = h
	if len(s.active) > s.MaxActive {
		s.MaxActive = len(s.active)
	}
	return h, nil
}
func (s *Simulated) Checkpoint(ctx context.Context, h RuntimeHandle) (execution.CheckpointRef, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if ctx.Err() != nil {
		return execution.CheckpointRef{}, ctx.Err()
	}
	if s.active[h.ID] != h {
		return execution.CheckpointRef{}, errors.New("unknown runtime")
	}
	if e := os.MkdirAll(s.Root, 0700); e != nil {
		return execution.CheckpointRef{}, e
	}
	name := execution.NewID() + ".json"
	b, _ := json.Marshal(map[string]string{"requestId": h.RequestID, "evidenceMode": "simulated", "conversation": "inert local fixture"})
	if e := os.WriteFile(filepath.Join(s.Root, name), b, 0600); e != nil {
		return execution.CheckpointRef{}, e
	}
	return execution.CheckpointRef{Path: name, RequestID: h.RequestID}, nil
}
func (s *Simulated) Stop(ctx context.Context, h RuntimeHandle) (execution.CleanupOutcome, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if ctx.Err() != nil {
		return execution.CleanupOutcome{}, ctx.Err()
	}
	if s.active[h.ID] != h {
		return execution.CleanupOutcome{}, errors.New("unknown runtime")
	}
	delete(s.active, h.ID)
	return execution.CleanupOutcome{Confirmed: true}, nil
}
func (s *Simulated) Restore(ctx context.Context, in execution.ExecutionInput, ref execution.CheckpointRef) (RuntimeHandle, error) {
	if ref.RequestID != in.RequestID || filepath.Base(ref.Path) != ref.Path {
		return RuntimeHandle{}, errors.New("checkpoint mismatch")
	}
	b, e := os.ReadFile(filepath.Join(s.Root, ref.Path))
	if e != nil {
		return RuntimeHandle{}, e
	}
	var v map[string]string
	if json.Unmarshal(b, &v) != nil || v["requestId"] != in.RequestID {
		return RuntimeHandle{}, errors.New("checkpoint mismatch")
	}
	return s.Start(ctx, in)
}
