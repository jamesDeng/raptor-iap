package provider

import (
	"context"
	"encoding/json"
	"errors"
	appsv1 "k8s.io/api/apps/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
	kt "k8s.io/client-go/testing"
	"raptor-iap/infra-api/internal/domain"
	"testing"
)

// A patch lacking UID/resourceVersion checks can restart a replacement object.
func TestRestartPinsIdentityAndOnlyChangesPodTemplate(t *testing.T) {
	for _, mode := range []string{"ok", "uid", "label", "conflict", "timeout"} {
		t.Run(mode, func(t *testing.T) {
			d := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "app", Namespace: "ns", UID: "uid", ResourceVersion: "12", Generation: 4, Labels: map[string]string{"raptor.appcode": "app"}}}
			if mode == "uid" {
				d.UID = "replacement"
			}
			if mode == "label" {
				d.Labels["raptor.appcode"] = "foreign"
			}
			c := fake.NewSimpleClientset(d)
			patches := 0
			c.PrependReactor("patch", "deployments", func(a kt.Action) (bool, runtime.Object, error) {
				patches++
				var patch map[string]any
				if e := json.Unmarshal(a.(kt.PatchAction).GetPatch(), &patch); e != nil {
					t.Fatal(e)
				}
				if len(patch) != 2 {
					t.Fatalf("unexpected patch: %s", a.(kt.PatchAction).GetPatch())
				}
				meta := patch["metadata"].(map[string]any)
				if meta["uid"] != "uid" || meta["resourceVersion"] != "12" {
					t.Fatal(meta)
				}
				spec := patch["spec"].(map[string]any)
				template := spec["template"].(map[string]any)
				annotations := template["metadata"].(map[string]any)["annotations"].(map[string]any)
				if len(spec) != 1 || len(template) != 1 || len(annotations) != 1 || annotations["kubectl.kubernetes.io/restartedAt"] == nil {
					t.Fatal(patch)
				}
				if mode == "conflict" {
					return true, nil, conflictError()
				}
				if mode == "timeout" {
					return true, nil, context.DeadlineExceeded
				}
				return true, d, nil
			})
			p := &reader{evidence: "simulated", identity: func(context.Context, domain.Environment) (string, error) { return "123", nil }, kube: func(context.Context, domain.Environment) (kubernetes.Interface, error) { return c, nil }}
			receipt, e := p.Restart(context.Background(), domain.Environment{Code: "dev", AccountID: "123", ClusterID: "cluster"}, domain.RestartCommand{RequestID: "req", EnvCode: "dev", AppCode: "app", ClusterID: "cluster", Namespace: "ns", Name: "app", UID: "uid"})
			switch mode {
			case "ok":
				if e != nil || !receipt.Accepted || receipt.EvidenceMode != "simulated" || patches != 1 {
					t.Fatal(receipt, e, patches)
				}
			case "uid":
				if !errors.Is(e, domain.ErrIdentity) || patches != 0 {
					t.Fatal(e, patches)
				}
			case "label":
				if !errors.Is(e, domain.ErrScope) || patches != 0 {
					t.Fatal(e, patches)
				}
			case "conflict":
				if e == nil || e.Error() != "TargetChanged" || patches != 1 {
					t.Fatal(e, patches)
				}
			case "timeout":
				if e == nil || e.Error() != "SubmissionUnknown" || patches != 1 {
					t.Fatal(e, patches)
				}
			}
		})
	}
}

func conflictError() error {
	return apierrors.NewConflict(schema.GroupResource{Group: "apps", Resource: "deployments"}, "app", errors.New("fixture"))
}
