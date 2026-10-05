package provider

import (
	"context"
	"encoding/json"
	"errors"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
	"raptor-iap/infra-api/internal/domain"
	"strings"
	"testing"
)

func TestProviderDiscoveryScopeAndPagination(t *testing.T) {
	for _, kind := range []string{"database", "db-proxy"} {
		p := &reader{evidence: "simulated", identity: func(context.Context, domain.Environment) (string, error) { return "1", nil }, page: func(_ context.Context, e domain.Environment, k, c string, n int) (page, error) {
			tags := map[string]string{"env": "dev", "db-code": "x", "db-proxy-code": "x", "target-db-code": "db"}
			if n == 1 {
				return page{Total: 2, Records: []record{{ID: "a", Tags: tags}}}, nil
			}
			return page{Total: 2, Records: []record{{ID: "b", Tags: tags}}}, nil
		}}
		rows, e := p.Deployments(context.Background(), domain.Environment{Code: "dev", AccountID: "1"}, kind, "x")
		if e != nil || len(rows) != 2 || rows[0].EvidenceMode != "simulated" {
			t.Fatal(rows, e)
		}
		if kind == "db-proxy" && rows[0].TargetDBCode != "db" {
			t.Fatal(rows)
		}
	}
	for _, mode := range []string{"duplicate", "changed", "exhausted"} {
		p := &reader{page: func(_ context.Context, _ domain.Environment, _, _ string, n int) (page, error) {
			total := 101
			id := string(rune(64 + n))
			if mode == "duplicate" {
				id = "same"
			}
			if mode == "changed" {
				total += n
			}
			return page{Total: total, Records: []record{{ID: id}}}, nil
		}}
		if _, e := p.discover(context.Background(), domain.Environment{}, "database", "x"); e == nil {
			t.Fatal(mode)
		}
	}
}
func TestMissingACKIsNotEmpty(t *testing.T) {
	p := &reader{}
	if _, e := p.Deployments(context.Background(), domain.Environment{}, "application", "app"); !errors.Is(e, domain.ErrNotConfigured) {
		t.Fatal(e)
	}
}
func TestIdentityUsesTemporaryCredentialClient(t *testing.T) {
	called := false
	p := &reader{identity: func(context.Context, domain.Environment) (string, error) { called = true; return "1", nil }}
	id, e := p.Identity(context.Background(), domain.Environment{AccountID: "1"})
	if e != nil || id != "1" || !called {
		t.Fatal(id, e)
	}
}
func TestProviderSecretProjection(t *testing.T) {
	r := record{ID: "id", Name: "name", Tags: map[string]string{"password": "secret", "target-db-code": "db"}}
	b, _ := json.Marshal(project(r, domain.Environment{Code: "dev"}, "database", "x", "simulated"))
	if strings.Contains(string(b), "secret") || strings.Contains(string(b), "password") {
		t.Fatal(string(b))
	}
}
func TestDeploymentAndPodIdentityScope(t *testing.T) {
	ctx := context.Background()
	env := domain.Environment{Code: "dev", AccountID: "1", ClusterID: "cluster"}
	p := &reader{identity: func(context.Context, domain.Environment) (string, error) { return "1", nil }, evidence: "simulated"}
	target := domain.Target{EnvCode: "dev", ClusterID: "wrong"}
	if _, e := p.Status(ctx, env, target); !errors.Is(e, domain.ErrScope) {
		t.Fatal(e)
	}
}

func TestDeploymentAndPodIdentity(t *testing.T) {
	yes := true
	d := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "app", Namespace: "ns", UID: "deployment", Labels: map[string]string{"raptor.appcode": "app"}}}
	rs := &appsv1.ReplicaSet{ObjectMeta: metav1.ObjectMeta{Name: "rs", Namespace: "ns", UID: "rs", OwnerReferences: []metav1.OwnerReference{{UID: "deployment", Controller: &yes}}}}
	good := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "good", Namespace: "ns", UID: "good", OwnerReferences: []metav1.OwnerReference{{UID: "rs", Controller: &yes}}}}
	other := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "unrelated", Namespace: "ns", UID: "other", Labels: map[string]string{"raptor.appcode": "app"}}}
	c := fake.NewSimpleClientset(d, rs, good, other)
	p := &reader{evidence: "simulated", identity: func(context.Context, domain.Environment) (string, error) { return "1", nil }, kube: func(context.Context, domain.Environment) (kubernetes.Interface, error) { return c, nil }}
	env := domain.Environment{Code: "dev", AccountID: "1", ClusterID: "cluster"}
	target := domain.Target{EnvCode: "dev", AppCode: "app", ClusterID: "cluster", Namespace: "ns", Name: "app", UID: "deployment"}
	st, e := p.Status(context.Background(), env, target)
	if e != nil || len(st.Pods) != 1 || st.Pods[0].Name != "good" {
		t.Fatal(st, e)
	}
	target.UID = "changed"
	if _, e = p.Status(context.Background(), env, target); !errors.Is(e, domain.ErrIdentity) {
		t.Fatal(e)
	}
	target.UID = "deployment"
	target.AppCode = "other"
	if _, e = p.Status(context.Background(), env, target); !errors.Is(e, domain.ErrScope) {
		t.Fatal(e)
	}
}
func TestRepeatedKubernetesToken(t *testing.T) {
	c := fake.NewSimpleClientset()
	c.PrependReactor("list", "deployments", func(k8stesting.Action) (bool, runtime.Object, error) {
		return true, &appsv1.DeploymentList{ListMeta: metav1.ListMeta{Continue: "same"}}, nil
	})
	p := &reader{identity: func(context.Context, domain.Environment) (string, error) { return "1", nil }, kube: func(context.Context, domain.Environment) (kubernetes.Interface, error) { return c, nil }}
	if _, e := p.applications(context.Background(), domain.Environment{AccountID: "1", ClusterID: "cluster"}, "app"); e == nil {
		t.Fatal("accepted repeated token")
	}
}
