package runtime

import (
	"context"
	"encoding/json"
	"net/netip"
	"time"

	"github.com/jamesDeng/raptor-iap/components/agent-gateway/internal/execution"
)

// A resource continuation may restore Pi's journal after the Gateway process
// has lost the pre-drain canaries. Recover them before starting the model.
func (p *ObservationProxy) recoverPgcatPreDrainHold(ctx context.Context, in execution.LiveStart) error {
	b := in.Binding
	decision := in.DecisionContext
	if b.Operation != "db-proxy.replace-nodes" || b.ExecutionMode != "replacement" || b.ApprovalProtocol != "request-grants-v1" ||
		in.SessionCheckpoint == nil || decision == nil || (!decision.PreparationRetry && !decision.PreExpansionRetry) {
		return nil
	}
	if p == nil || p.RecordPgcatObservation == nil || in.Access.ReplacementScope == nil {
		return ErrConfiguration
	}
	desired, timeout := 4, 20*time.Second
	if decision.PreExpansionRetry {
		desired, timeout = 2, 30*time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	scope := *in.Access.ReplacementScope
	var raw json.RawMessage
	var err error
	if decision.PreExpansionRetry {
		if p.ReadPgcatRequestJournal == nil {
			return ErrConfiguration
		}
		state, journalErr := p.ReadPgcatRequestJournal(ctx, b, scope)
		if journalErr != nil {
			return journalErr
		}
		raw, err = preExpansionHold(state, b, scope)
	} else {
		if p.ReadPgcatPreDrainHold == nil {
			return ErrConfiguration
		}
		raw, err = p.ReadPgcatPreDrainHold(ctx, b, scope)
	}
	if err != nil || raw == nil {
		return err
	}
	var saved pgcatHoldEvidence
	if json.Unmarshal(raw, &saved) != nil || saved.RequestID != b.RequestID || saved.ScopeDigest != scope.ResolutionDigest ||
		!transportID.MatchString(saved.ObservationID) || len(saved.Sockets) != 2 || saved.ObservedAt.IsZero() {
		return ErrConfiguration
	}
	hash, entry, ok := p.registeredEntry(in)
	if !ok || entry.active == nil {
		return ErrConfiguration
	}
	raw, status := p.guardedExecute(ctx, hash, entry, "cloud_read", json.RawMessage(`{"kind":"capacity"}`))
	var capacity struct {
		GroupID         string    `json:"groupId"`
		DesiredCapacity int       `json:"desiredCapacity"`
		ObservedAt      time.Time `json:"observedAt"`
	}
	if status != 200 || json.Unmarshal(raw, &capacity) != nil || capacity.GroupID != scope.GroupID ||
		capacity.DesiredCapacity != desired || capacity.ObservedAt.IsZero() {
		return ErrRuntimeUnavailable
	}
	raw, status = p.guardedExecute(ctx, hash, entry, "cloud_read", json.RawMessage(`{"kind":"backend"}`))
	var backend struct {
		ServerGroupID string                 `json:"serverGroupId"`
		ListenerID    string                 `json:"listenerId"`
		ObservedAt    time.Time              `json:"observedAt"`
		Nodes         []pgcatObservedBackend `json:"nodes"`
	}
	if status != 200 || json.Unmarshal(raw, &backend) != nil || backend.ServerGroupID != scope.ServerGroupID ||
		backend.ListenerID != scope.ListenerID || backend.ObservedAt.IsZero() || len(backend.Nodes) != desired {
		return ErrRuntimeUnavailable
	}
	routed := make(map[string]netip.Addr, 4)
	for _, node := range backend.Nodes {
		addr, err := netip.ParseAddr(node.PrivateIP)
		if err != nil || routed[node.InstanceID].IsValid() || node.Status != "Available" || !node.Registered || !node.Healthy {
			return ErrRuntimeUnavailable
		}
		routed[node.InstanceID] = addr
	}
	if decision.PreExpansionRetry {
		raw, status = p.guardedExecute(ctx, hash, entry, "cloud_read", json.RawMessage(`{"kind":"fleet"}`))
		var fleet struct {
			GroupID    string    `json:"groupId"`
			ObservedAt time.Time `json:"observedAt"`
			Instances  []struct {
				InstanceID           string `json:"instanceId"`
				LifecycleState       string `json:"lifecycleState"`
				HealthStatus         string `json:"healthStatus"`
				ProtectedFromScaleIn bool   `json:"protectedFromScaleIn"`
			} `json:"instances"`
		}
		if status != 200 || json.Unmarshal(raw, &fleet) != nil || fleet.GroupID != scope.GroupID || fleet.ObservedAt.IsZero() || len(fleet.Instances) != desired {
			return ErrRuntimeUnavailable
		}
		seen := make(map[string]bool, 2)
		for _, node := range fleet.Instances {
			if seen[node.InstanceID] || !routed[node.InstanceID].IsValid() || saved.Sockets[node.InstanceID].ID == "" ||
				node.HealthStatus != "Healthy" || node.LifecycleState != "Protected" || !node.ProtectedFromScaleIn {
				return ErrRuntimeUnavailable
			}
			seen[node.InstanceID] = true
		}
	}
	connect, err := privatePgcatConnector(entry.material, scope.Port)
	if err != nil {
		return err
	}
	hold, err := p.pgcat.recaptureBeforeDrain(ctx, saved, routed, backend.ObservedAt, connect, entry.active)
	if err != nil {
		return err
	}
	if err := p.startPgcatLogWatch(ctx, entry, hold); err != nil {
		p.pgcat.release(b.RequestID)
		return err
	}
	raw, err = json.Marshal(hold)
	if err == nil {
		err = p.RecordPgcatObservation(ctx, execution.PgcatObservationRecord{
			RequestID: b.RequestID, AttemptID: b.AttemptID, ScopeDigest: scope.ResolutionDigest,
			Kind: "hold", Subject: "fleet", ObservedAt: hold.ObservedAt, Data: raw,
		})
	}
	if err != nil {
		p.pgcat.release(b.RequestID)
	}
	return err
}

func preExpansionHold(state execution.PgcatRequestJournal, binding execution.AttemptBinding, scope execution.ReplacementScope) (json.RawMessage, error) {
	if len(state.Actions) != 0 {
		return nil, ErrConfiguration
	}
	var hold json.RawMessage
	for _, row := range state.Observations {
		if row.RequestID != binding.RequestID || row.ScopeDigest != scope.ResolutionDigest {
			return nil, ErrConfiguration
		}
		switch row.Kind {
		case "hold":
			if hold != nil || row.Subject != "fleet" || row.ObservedAt.IsZero() || len(row.Data) == 0 {
				return nil, ErrConfiguration
			}
			hold = row.Data
		case "sql":
		default:
			return nil, ErrConfiguration
		}
	}
	return hold, nil
}
