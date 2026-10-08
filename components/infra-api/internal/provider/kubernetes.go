package provider

import (
	"context"
	"errors"
	"fmt"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"raptor-iap/infra-api/internal/domain"
)

func (p *reader) cluster(ctx context.Context, env domain.Environment) (kubernetes.Interface, error) {
	if env.ClusterID == "" || p.kube == nil {
		return nil, domain.ErrNotConfigured
	}
	id, e := p.Identity(ctx, env)
	if e != nil {
		return nil, e
	}
	if id != env.AccountID {
		return nil, domain.ErrScope
	}
	return p.kube(ctx, env)
}
func (p *reader) applications(ctx context.Context, env domain.Environment, code string) ([]domain.Deployment, error) {
	c, e := p.cluster(ctx, env)
	if e != nil {
		return nil, e
	}
	out := []domain.Deployment{}
	token := ""
	seen := map[string]bool{}
	selector := labels.Set{"raptor.appcode": code}.AsSelector().String()
	for n := 0; n < 100; n++ {
		r, e := c.AppsV1().Deployments("").List(ctx, metav1.ListOptions{LabelSelector: selector, Limit: 100, Continue: token})
		if e != nil {
			return nil, e
		}
		for _, d := range r.Items {
			if d.Labels["raptor.appcode"] != code {
				return nil, domain.ErrScope
			}
			out = append(out, domain.Deployment{ResourceID: string(d.UID), Kind: "application", EnvCode: env.Code, ObjectCode: code, ClusterID: env.ClusterID, Namespace: d.Namespace, Name: d.Name, UID: string(d.UID), State: fmt.Sprintf("%d/%d ready", d.Status.ReadyReplicas, d.Status.Replicas), EvidenceMode: p.evidence})
		}
		token = r.Continue
		if token == "" {
			return out, nil
		}
		if seen[token] {
			return nil, errors.New("repeated pagination token")
		}
		seen[token] = true
	}
	return nil, errors.New("pagination exhausted")
}
func owned(refs []metav1.OwnerReference, uid types.UID) bool {
	for _, r := range refs {
		if r.UID == uid && r.Controller != nil && *r.Controller {
			return true
		}
	}
	return false
}
func (p *reader) Status(ctx context.Context, env domain.Environment, t domain.Target) (domain.Status, error) {
	if env.ClusterID == "" {
		return domain.Status{}, domain.ErrNotConfigured
	}
	if t.EnvCode != env.Code || t.ClusterID != env.ClusterID {
		return domain.Status{}, domain.ErrScope
	}
	c, e := p.cluster(ctx, env)
	if e != nil {
		return domain.Status{}, e
	}
	d, e := c.AppsV1().Deployments(t.Namespace).Get(ctx, t.Name, metav1.GetOptions{})
	if e != nil {
		return domain.Status{}, e
	}
	if string(d.UID) != t.UID {
		return domain.Status{}, domain.ErrIdentity
	}
	if d.Labels["raptor.appcode"] != t.AppCode {
		return domain.Status{}, domain.ErrScope
	}
	desired := int32(1)
	if d.Spec.Replicas != nil {
		desired = *d.Spec.Replicas
	}
	status := domain.Status{DesiredReplicas: int(desired), AvailableReplicas: int(d.Status.AvailableReplicas), UID: string(d.UID), Generation: d.Generation, ObservedGeneration: d.Status.ObservedGeneration, Replicas: int(d.Status.Replicas), UpdatedReplicas: int(d.Status.UpdatedReplicas), ReadyReplicas: int(d.Status.ReadyReplicas), EvidenceMode: p.evidence, Pods: []domain.Pod{}}
	rsIDs := map[types.UID]bool{}
	token := ""
	seen := map[string]bool{}
	complete := false
	for n := 0; n < 100; n++ {
		rs, e := c.AppsV1().ReplicaSets(t.Namespace).List(ctx, metav1.ListOptions{Limit: 100, Continue: token})
		if e != nil {
			return domain.Status{}, e
		}
		for _, r := range rs.Items {
			if owned(r.OwnerReferences, d.UID) {
				rsIDs[r.UID] = true
			}
		}
		token = rs.Continue
		if token == "" {
			complete = true
			break
		}
		if seen[token] {
			return domain.Status{}, errors.New("repeated pagination token")
		}
		seen[token] = true
	}
	if !complete {
		return domain.Status{}, errors.New("pagination exhausted")
	}
	token = ""
	seen = map[string]bool{}
	for n := 0; n < 100; n++ {
		pods, e := c.CoreV1().Pods(t.Namespace).List(ctx, metav1.ListOptions{Limit: 100, Continue: token})
		if e != nil {
			return domain.Status{}, e
		}
		for _, pod := range pods.Items {
			belongs := false
			for id := range rsIDs {
				if owned(pod.OwnerReferences, id) {
					belongs = true
					break
				}
			}
			if !belongs {
				continue
			}
			ready := false
			for _, co := range pod.Status.Conditions {
				if co.Type == "Ready" && co.Status == "True" {
					ready = true
				}
			}
			status.Pods = append(status.Pods, domain.Pod{Name: pod.Name, UID: string(pod.UID), State: string(pod.Status.Phase), Ready: ready})
		}
		token = pods.Continue
		if token == "" {
			return status, nil
		}
		if seen[token] {
			return domain.Status{}, errors.New("repeated pagination token")
		}
		seen[token] = true
	}
	return domain.Status{}, errors.New("pagination exhausted")
}
