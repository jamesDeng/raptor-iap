package requests

import (
	"context"
	"errors"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/adapters"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/domain"
	"sync"
	"time"
)

type InfraCommands = adapters.InfraCommands

func (s *Service) restartNow() time.Time {
	if s.RestartNow != nil {
		return s.RestartNow()
	}
	return time.Now()
}

func (s *Service) RunRestartBatch(ctx context.Context, id string, client InfraCommands) error {
	request, e := s.Get(ctx, id)
	if e != nil {
		return e
	}
	if request.Definition.Type != "direct" || request.Status == "cancelled" || request.Status == "blocked" {
		return domain.ErrConflict
	}
	items, e := s.RestartItems(ctx, id)
	if e != nil {
		return e
	}
	var wg sync.WaitGroup
	var mu sync.Mutex
	var first error
	for _, item := range items {
		item := item
		if item.State != "queued" && item.State != "failed" {
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			if e := s.restartOne(ctx, id, item.Target, client); e != nil {
				mu.Lock()
				if first == nil {
					first = e
				}
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	_, e = s.Pool.Exec(ctx, "UPDATE raptor.requests SET status=CASE WHEN EXISTS(SELECT 1 FROM raptor.targets WHERE request_id=$1 AND state<>'succeeded') THEN 'failed' ELSE 'completed' END WHERE id=$1 AND status NOT IN ('cancelled','blocked')", id)
	if first != nil {
		return first
	}
	return e
}
func (s *Service) restartOne(ctx context.Context, id string, target domain.RestartTarget, client InfraCommands) error {
	tag, e := s.Pool.Exec(ctx, "UPDATE raptor.targets SET state='submitting',details='{}' WHERE request_id=$1 AND target_key=$2 AND state IN ('queued','failed') AND (SELECT status FROM raptor.requests WHERE id=$1) NOT IN ('cancelled','blocked')", id, TargetKey(target))
	if e != nil || tag.RowsAffected() == 0 {
		return e
	}
	baseline, e := client.GetDeploymentStatus(ctx, target)
	if e != nil || baseline.UID != target.UID {
		return s.setRestartState(ctx, id, target, "failed", map[string]string{"reason": "deployment lookup unavailable or identity changed", "evidenceMode": "unverified"})
	}
	deadline := s.restartNow().Add(10 * time.Minute)
	if e = s.setRestartState(ctx, id, target, "submitting", map[string]any{"baselineGeneration": baseline.Generation, "deadline": deadline, "evidenceMode": baseline.EvidenceMode}); e != nil {
		return e
	}
	e = client.RestartDeployment(ctx, id, target)
	if e != nil {
		state := "unknown"
		if errors.Is(e, adapters.ErrRejected) {
			state = "failed"
		}
		return s.setRestartState(ctx, id, target, state, map[string]string{"reason": "restart submission " + state, "evidenceMode": baseline.EvidenceMode})
	}
	if e = s.setRestartState(ctx, id, target, "observing", map[string]any{"baselineGeneration": baseline.Generation, "deadline": deadline, "evidenceMode": baseline.EvidenceMode}); e != nil {
		return e
	}
	for {
		status, e := client.GetDeploymentStatus(ctx, target)
		if e != nil || status.UID != target.UID {
			return s.setRestartState(ctx, id, target, "unknown", map[string]string{"reason": "rollout lookup unavailable or identity changed", "evidenceMode": baseline.EvidenceMode})
		}
		if status.Generation > baseline.Generation && status.ObservedGeneration >= status.Generation && status.Replicas == status.DesiredReplicas && status.UpdatedReplicas == status.DesiredReplicas && status.ReadyReplicas == status.DesiredReplicas && status.AvailableReplicas == status.DesiredReplicas {
			return s.setRestartState(ctx, id, target, "succeeded", status)
		}
		if !s.restartNow().Before(deadline) {
			return s.setRestartState(ctx, id, target, "unknown", map[string]string{"reason": "ten-minute observation deadline exceeded", "evidenceMode": baseline.EvidenceMode})
		}
		timer := time.NewTimer(time.Second)
		select {
		case <-ctx.Done():
			timer.Stop()
			return s.setRestartState(context.WithoutCancel(ctx), id, target, "unknown", map[string]string{"reason": "observation interrupted", "evidenceMode": baseline.EvidenceMode})
		case <-timer.C:
		}
	}
}
