package adapters

import (
	"context"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/domain"
	"sync"
)

type SimulatedCommands struct {
	mu     sync.Mutex
	states map[domain.RestartTarget]DeploymentStatus
}

func (s *SimulatedCommands) GetDeploymentStatus(ctx context.Context, t domain.RestartTarget) (DeploymentStatus, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if ctx.Err() != nil {
		return DeploymentStatus{}, ctx.Err()
	}
	if s.states == nil {
		s.states = map[domain.RestartTarget]DeploymentStatus{}
	}
	v, ok := s.states[t]
	if !ok {
		v = DeploymentStatus{UID: t.UID, Generation: 1, ObservedGeneration: 1, Replicas: 1, ReadyReplicas: 1, UpdatedReplicas: 1, EvidenceMode: "simulated"}
		s.states[t] = v
	}
	return v, nil
}
func (s *SimulatedCommands) RestartDeployment(ctx context.Context, id string, t domain.RestartTarget) error {
	if ctx.Err() != nil {
		return ErrSubmissionUnknown
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.states[t]
	if !ok {
		return ErrRejected
	}
	v.Generation++
	v.ObservedGeneration = v.Generation
	s.states[t] = v
	return nil
}
