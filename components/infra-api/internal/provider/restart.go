package provider

import (
	"context"
	"encoding/json"
	appsv1 "k8s.io/api/apps/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
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
	submit := p.restartPatch
	if submit == nil {
		submit = patchOnce
	}
	updated, e := submit(ctx, c, in.Namespace, in.Name, patch)
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

func patchOnce(ctx context.Context, c kubernetes.Interface, namespace, name string, patch []byte) (*appsv1.Deployment, error) {
	out := new(appsv1.Deployment)
	err := c.AppsV1().RESTClient().Patch(types.MergePatchType).Namespace(namespace).Resource("deployments").Name(name).Body(patch).MaxRetries(0).Do(ctx).Into(out)
	return out, err
}
