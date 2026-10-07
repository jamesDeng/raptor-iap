package provider

import (
	"context"
	"encoding/json"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"raptor-iap/infra-api/internal/domain"
	"time"
)

// Restart submits one pod-template patch. It does not track rollout ownership
// or retry a submission whose acknowledgement has been lost.
func (p *reader) Restart(ctx context.Context, env domain.Environment, in domain.RestartCommand) (domain.RestartReceipt, error) {
	if in.EnvCode != env.Code || in.ClusterID != env.ClusterID {
		return domain.RestartReceipt{}, domain.ErrScope
	}
	c, e := p.cluster(ctx, env)
	if e != nil {
		return domain.RestartReceipt{}, e
	}
	d, e := c.AppsV1().Deployments(in.Namespace).Get(ctx, in.Name, metav1.GetOptions{})
	if e != nil {
		return domain.RestartReceipt{}, e
	}
	if string(d.UID) != in.UID {
		return domain.RestartReceipt{}, domain.ErrIdentity
	}
	if d.Labels["raptor.appcode"] != in.AppCode {
		return domain.RestartReceipt{}, domain.ErrScope
	}
	patch, _ := json.Marshal(map[string]any{
		"metadata": map[string]string{"uid": in.UID, "resourceVersion": d.ResourceVersion},
		"spec":     map[string]any{"template": map[string]any{"metadata": map[string]any{"annotations": map[string]string{"kubectl.kubernetes.io/restartedAt": time.Now().UTC().Format(time.RFC3339Nano)}}}},
	})
	updated, e := c.AppsV1().Deployments(in.Namespace).Patch(ctx, in.Name, types.MergePatchType, patch, metav1.PatchOptions{})
	if e != nil {
		if apierrors.IsConflict(e) {
			return domain.RestartReceipt{}, &domain.CommandError{Code: "TargetChanged"}
		}
		if apierrors.IsForbidden(e) || apierrors.IsInvalid(e) || apierrors.IsNotFound(e) {
			return domain.RestartReceipt{}, e
		}
		return domain.RestartReceipt{}, &domain.CommandError{Code: "SubmissionUnknown"}
	}
	return domain.RestartReceipt{Accepted: true, Target: in, Generation: updated.Generation, EvidenceMode: p.evidence}, nil
}
