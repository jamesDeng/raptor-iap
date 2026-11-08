package provider

import (
	"context"
	"encoding/json"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/fake"
	"raptor-iap/infra-api/internal/domain"
)

func TestPgcatInventoryUsesOnlyExactProviderMembers(t *testing.T) {
	fleet := []domain.FleetNode{{InstanceID: "old-1", LifecycleState: "Protected", HealthStatus: "Healthy"}, {InstanceID: "old-2", LifecycleState: "Protected", HealthStatus: "Healthy"}}
	backends := []domain.BackendNode{{InstanceID: "old-1", PrivateIP: "10.70.1.220", Status: "Available", Registered: true, Healthy: true},
		{InstanceID: "old-2", PrivateIP: "10.70.1.221", Status: "Available", Registered: true, Healthy: true}}
	rows, err := pgcatInventoryTargets("rdev.ali", "DP00001", "D00002", 2, fleet, backends)
	if err != nil || len(rows) != 2 || rows[0].Targets[0] != "10.70.1.220:9930" || rows[1].Labels.InstanceID != "old-2" {
		t.Fatalf("bound targets: %+v %v", rows, err)
	}
	backends[1].InstanceID = "foreign"
	if _, err := pgcatInventoryTargets("rdev.ali", "DP00001", "D00002", 2, fleet, backends); err == nil {
		t.Fatal("unexpected backend entered the inventory")
	}
}

func TestPgcatMonitoringTargetsConvergeExpandDrainAndRetire(t *testing.T) {
	fleet := []domain.FleetNode{{InstanceID: "old-1", LifecycleState: "Protected", HealthStatus: "Healthy"}, {InstanceID: "old-2", LifecycleState: "Protected", HealthStatus: "Healthy"}}
	backends := []domain.BackendNode{{InstanceID: "old-1", PrivateIP: "10.70.1.220", Status: "Available", Registered: true}, {InstanceID: "old-2", PrivateIP: "10.70.1.221", Status: "Available", Registered: true}}
	check := func(want int) []pgcatInventoryRow {
		t.Helper()
		rows, err := pgcatMonitoringTargets("rdev.ali", "DP00001", "D00002", want, fleet, backends)
		if err != nil || len(rows) != want {
			t.Fatalf("targets %d: %+v %v", want, rows, err)
		}
		return rows
	}
	check(2)
	fleet = append(fleet, domain.FleetNode{InstanceID: "new-1", LifecycleState: "Protected", HealthStatus: "Healthy"})
	if _, err := pgcatMonitoringTargets("rdev.ali", "DP00001", "D00002", 4, fleet, backends); err == nil {
		t.Fatal("partial scale out published")
	}
	fleet = append(fleet, domain.FleetNode{InstanceID: "new-2", LifecycleState: "Protected", HealthStatus: "Healthy"})
	backends = append(backends, domain.BackendNode{InstanceID: "new-1", PrivateIP: "10.70.1.222", Status: "Available", Registered: true}, domain.BackendNode{InstanceID: "new-2", PrivateIP: "10.70.1.223", Status: "Available", Registered: true})
	check(4)
	backends[0].Registered, backends[1].Registered = false, false
	if rows := check(4); rows[2].Labels.InstanceID != "old-1" {
		t.Fatal("drained member disappeared from scrape")
	}
	fleet = fleet[2:]
	rows := check(2)
	if rows[0].Labels.InstanceID != "new-1" || rows[1].Labels.InstanceID != "new-2" {
		t.Fatalf("retired targets remained: %+v", rows)
	}
}

func TestPgcatMonitoringTargetsRefuseIncompleteOrForeignEvidence(t *testing.T) {
	fleet := []domain.FleetNode{{InstanceID: "one", LifecycleState: "Protected", HealthStatus: "Healthy"}, {InstanceID: "two", LifecycleState: "Protected", HealthStatus: "Healthy"}}
	backends := []domain.BackendNode{{InstanceID: "one", PrivateIP: "10.70.1.1", Status: "Available"}, {InstanceID: "two", PrivateIP: "10.70.1.2", Status: "Available"}}
	for _, mutate := range []func(){
		func() { backends[1].Status = "Adding" },
		func() { backends[1].PrivateIP = "8.8.8.8" },
		func() { backends[1].InstanceID = "foreign" },
		func() { fleet[1].HealthStatus = "Unhealthy" },
	} {
		copyFleet := append([]domain.FleetNode(nil), fleet...)
		copyBackends := append([]domain.BackendNode(nil), backends...)
		mutate()
		if _, err := pgcatMonitoringTargets("rdev.ali", "DP00001", "D00002", 2, fleet, backends); err == nil {
			t.Fatal("invalid provider evidence published")
		}
		fleet, backends = copyFleet, copyBackends
	}
}

func TestPgcatInventoryPatchPreservesUnrelatedFieldsAndRefusesForeignTargets(t *testing.T) {
	ctx := context.Background()
	const namespace = "raptor-test"
	const name = "pgcat-provider-inventory"
	original := `[{"targets":["10.70.1.220:9930"],"labels":{"env":"rdev.ali","db_proxy_code":"DP00001","target_db_code":"D00002","ecs_instance_id":"old-1"}}]`
	targets := []pgcatInventoryRow{}
	if err := json.Unmarshal([]byte(original), &targets); err != nil {
		t.Fatal(err)
	}
	targets[0].Targets[0] = "10.70.1.225:9930"
	targets[0].Labels.InstanceID = "new-1"
	makeClient := func(payload string) *fake.Clientset {
		return fake.NewSimpleClientset(&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: name, UID: types.UID("fixture-uid"), ResourceVersion: "42"},
			Data: map[string]string{"targets.json": payload, "unrelated": "untouched"}})
	}
	for _, tc := range []struct {
		payload string
		allowed bool
	}{
		{original, true},
		{`[{"targets":["10.70.1.220:9930"],"labels":{"env":"different","db_proxy_code":"DP00001","target_db_code":"D00002","ecs_instance_id":"old-1"}}]`, false},
	} {
		client := makeClient(tc.payload)
		receipt, err := syncPgcatInventory(ctx, client.CoreV1().ConfigMaps(namespace), "rdev.ali", "DP00001", "D00002", targets, func(context.Context) error { return nil })
		if (err == nil) != tc.allowed {
			t.Fatalf("patch %q: %+v %v", tc.payload, receipt, err)
		}
		current, err := client.CoreV1().ConfigMaps(namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil || current.Data["unrelated"] != "untouched" {
			t.Fatal("unrelated field changed")
		}
		if !tc.allowed && current.Data["targets.json"] != tc.payload {
			t.Fatal("foreign content overwritten")
		}
		if tc.allowed && receipt.UID != "fixture-uid" {
			t.Fatalf("wrong resource receipt: %+v", receipt)
		}
	}
}
