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
		in.SessionCheckpoint == nil || decision == nil || !decision.PreparationRetry {
		return nil
	}
	if p == nil || p.ReadPgcatPreDrainHold == nil || p.RecordPgcatObservation == nil || in.Access.ReplacementScope == nil {
		return ErrConfiguration
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	scope := *in.Access.ReplacementScope
	raw, err := p.ReadPgcatPreDrainHold(ctx, b, scope)
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
		capacity.DesiredCapacity != 4 || capacity.ObservedAt.IsZero() {
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
		backend.ListenerID != scope.ListenerID || backend.ObservedAt.IsZero() || len(backend.Nodes) != 4 {
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
