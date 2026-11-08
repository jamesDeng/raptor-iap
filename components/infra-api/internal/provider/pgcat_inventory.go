package provider

import (
	"context"
	"encoding/json"
	"net"
	"net/netip"
	"reflect"
	"sort"
	"strings"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	coreclient "k8s.io/client-go/kubernetes/typed/core/v1"
	"raptor-iap/infra-api/internal/domain"
	"raptor-iap/infra-api/internal/jsoninput"
)

const (
	pgcatInventoryNamespace = "raptor-test"
	pgcatInventoryName      = "pgcat-provider-inventory"
	pgcatInventoryKey       = "targets.json"
)

type pgcatInventoryLabels struct {
	Env        string `json:"env"`
	ProxyCode  string `json:"db_proxy_code"`
	TargetDB   string `json:"target_db_code"`
	InstanceID string `json:"ecs_instance_id"`
}

type pgcatInventoryRow struct {
	Targets []string             `json:"targets"`
	Labels  pgcatInventoryLabels `json:"labels"`
}

func pgcatInventoryTargets(env, proxy, db string, desired int, fleet []domain.FleetNode, backends []domain.BackendNode) ([]pgcatInventoryRow, error) {
	if (desired != 2 && desired != 4) || len(fleet) != desired || len(backends) != desired {
		return nil, domain.ErrScope
	}
	members := make(map[string]bool, len(fleet))
	for _, member := range fleet {
		if member.InstanceID == "" || members[member.InstanceID] || member.HealthStatus != "Healthy" ||
			(member.LifecycleState != "Protected" && member.LifecycleState != "InService") {
			return nil, domain.ErrScope
		}
		members[member.InstanceID] = true
	}
	rows := make([]pgcatInventoryRow, 0, desired)
	seenAddresses := make(map[netip.Addr]bool, desired)
	for _, backend := range backends {
		ip, err := netip.ParseAddr(backend.PrivateIP)
		if !members[backend.InstanceID] || !backend.Registered || !backend.Healthy || backend.Status != "Available" ||
			err != nil || !ip.Is4() || !ip.IsPrivate() || seenAddresses[ip] {
			return nil, domain.ErrScope
		}
		delete(members, backend.InstanceID)
		seenAddresses[ip] = true
		rows = append(rows, pgcatInventoryRow{Targets: []string{net.JoinHostPort(ip.String(), "9930")},
			Labels: pgcatInventoryLabels{Env: env, ProxyCode: proxy, TargetDB: db, InstanceID: backend.InstanceID}})
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].Labels.InstanceID < rows[j].Labels.InstanceID })
	return rows, nil
}

func parsePgcatInventory(raw, env, proxy, db string) ([]pgcatInventoryRow, error) {
	var rows []pgcatInventoryRow
	if raw == "" || len(raw) > 8192 || jsoninput.Decode([]byte(raw), &rows) != nil || rows == nil || len(rows) > 4 {
		return nil, domain.ErrScope
	}
	nodes := make(map[string]bool, len(rows))
	addresses := make(map[netip.Addr]bool, len(rows))
	for _, row := range rows {
		id := row.Labels.InstanceID
		if row.Labels.Env != env || row.Labels.ProxyCode != proxy || row.Labels.TargetDB != db ||
			id == "" || strings.TrimSpace(id) != id || nodes[id] || len(row.Targets) != 1 {
			return nil, domain.ErrScope
		}
		host, port, splitErr := net.SplitHostPort(row.Targets[0])
		ip, err := netip.ParseAddr(host)
		if splitErr != nil || port != "9930" || err != nil || !ip.Is4() || !ip.IsPrivate() || addresses[ip] {
			return nil, domain.ErrScope
		}
		nodes[id], addresses[ip] = true, true
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].Labels.InstanceID < rows[j].Labels.InstanceID })
	return rows, nil
}

func syncPgcatInventory(ctx context.Context, client coreclient.ConfigMapInterface, env, proxy, db string, rows []pgcatInventoryRow) (domain.PgcatInventoryReceipt, error) {
	current, err := client.Get(ctx, pgcatInventoryName, metav1.GetOptions{})
	if err != nil {
		return domain.PgcatInventoryReceipt{}, err
	}
	if current.Namespace != pgcatInventoryNamespace || current.Name != pgcatInventoryName ||
		current.UID == "" || current.ResourceVersion == "" {
		return domain.PgcatInventoryReceipt{}, domain.ErrIdentity
	}
	original, err := parsePgcatInventory(current.Data[pgcatInventoryKey], env, proxy, db)
	if err != nil {
		return domain.PgcatInventoryReceipt{}, err
	}
	result := domain.PgcatInventoryReceipt{UID: string(current.UID), ResourceVersion: current.ResourceVersion}
	for _, target := range rows {
		result.InstanceIDs = append(result.InstanceIDs, target.Labels.InstanceID)
	}
	if reflect.DeepEqual(original, rows) {
		return result, nil
	}

	payload, err := json.Marshal(rows)
	if err != nil {
		return domain.PgcatInventoryReceipt{}, domain.ErrScope
	}
	patch, _ := json.Marshal([]map[string]string{
		{"op": "test", "path": "/metadata/uid", "value": string(current.UID)},
		{"op": "test", "path": "/metadata/resourceVersion", "value": current.ResourceVersion},
		{"op": "replace", "path": "/data/targets.json", "value": string(payload)},
	})
	if _, err := client.Patch(ctx, pgcatInventoryName, types.JSONPatchType, patch, metav1.PatchOptions{}); err != nil {
		if apierrors.IsInvalid(err) || apierrors.IsConflict(err) || apierrors.IsForbidden(err) || apierrors.IsNotFound(err) {
			return domain.PgcatInventoryReceipt{}, err
		}
		return domain.PgcatInventoryReceipt{}, &domain.CommandError{Code: "SubmissionUnknown"}
	}
	verified, err := client.Get(ctx, pgcatInventoryName, metav1.GetOptions{})
	if err != nil || verified.UID != current.UID || verified.ResourceVersion == "" ||
		verified.Namespace != pgcatInventoryNamespace || verified.Name != pgcatInventoryName ||
		verified.Data[pgcatInventoryKey] != string(payload) {
		return domain.PgcatInventoryReceipt{}, &domain.CommandError{Code: "SubmissionUnknown"}
	}
	result.Changed = true
	result.ResourceVersion = verified.ResourceVersion
	return result, nil
}

// SyncPgcatInventory only manages the existing POC target key and derives
// every endpoint from matching ESS membership and available NLB backends.
func (p *reader) SyncPgcatInventory(ctx context.Context, env domain.Environment, target domain.ProxyReadTarget) (domain.PgcatInventoryReceipt, error) {
	if target.RequestID == "" || target.EnvCode != "rdev.ali" || target.ProxyCode != "DP00001" {
		return domain.PgcatInventoryReceipt{}, domain.ErrScope
	}
	mapping, group, err := p.observationGroup(ctx, env, target)
	if err != nil {
		return domain.PgcatInventoryReceipt{}, err
	}
	if mapping.TargetDBCode != "D00002" {
		return domain.PgcatInventoryReceipt{}, domain.ErrScope
	}
	fleet, err := p.proxy.observationMembers(ctx, env, mapping)
	if err != nil {
		return domain.PgcatInventoryReceipt{}, err
	}
	backends, err := p.observeBackend(ctx, env, mapping, fleet)
	if err != nil {
		return domain.PgcatInventoryReceipt{}, err
	}
	targets, err := pgcatInventoryTargets(env.Code, target.ProxyCode, mapping.TargetDBCode, int(*group.DesiredCapacity), fleet, backends.Nodes)
	if err != nil {
		return domain.PgcatInventoryReceipt{}, err
	}
	client, err := p.cluster(ctx, env)
	if err != nil {
		return domain.PgcatInventoryReceipt{}, err
	}
	return syncPgcatInventory(ctx, client.CoreV1().ConfigMaps(pgcatInventoryNamespace), env.Code, target.ProxyCode, mapping.TargetDBCode, targets)
}
