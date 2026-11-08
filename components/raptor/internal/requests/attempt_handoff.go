package requests

import (
	"context"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/agentaccess"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/domain"
)

// PrepareAgentAttempt reconciles trusted Gateway starts before credential
// issuance. Other request profiles retain their existing access policy.
func (s *Service) PrepareAgentAttempt(ctx context.Context, id, attempt, fingerprint string) error {
	r, err := s.Get(ctx, id)
	if err != nil {
		return err
	}
	if r.Status != "waiting_resource" && agentaccess.RequestGrantProtocol(r.Definition) == "" {
		return nil
	}
	hash, err := agentaccess.Fingerprint(r.Definition)
	if err != nil || hash != fingerprint {
		return domain.ErrInvalid
	}
	if s.Gateway == nil {
		return domain.ErrUnavailable
	}
	raw, err := s.Gateway.Execution(ctx, id)
	if err != nil {
		return domain.ErrUnavailable
	}
	view, err := publicExecution(raw, r)
	if err != nil {
		return err
	}
	if view["requestId"] != id || view["attemptId"] != attempt || view["runtimeMode"] != "live" || view["status"] != "running" || view["stage"] != "preparing" || view["recoveryNeeded"] == true || view["updatedAt"] == nil {
		return domain.ErrConflict
	}
	_, _, accepted, err := s.syncExecutionForAttempt(ctx, r, view, attempt)
	if err != nil {
		return err
	}
	if !accepted {
		return domain.ErrConflict
	}
	return nil
}
