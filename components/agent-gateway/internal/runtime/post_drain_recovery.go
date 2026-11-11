package runtime

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/netip"
	"time"

	"github.com/jamesDeng/raptor-iap/components/agent-gateway/internal/execution"
)

func (n *NativeLive) restorePostDrain(ctx context.Context, in *execution.LiveStart) error {
	if in.DecisionContext == nil || in.DecisionContext.PostDrainAttempt == "" {
		return nil
	}
	p := n.Observations
	if p == nil || p.ReadPgcatRequestJournal == nil || in.SessionCheckpoint == nil || in.Access.ReplacementScope == nil {
		return ErrConfiguration
	}
	ctx, cancel := context.WithTimeout(ctx, 50*time.Second)
	defer cancel()
	b, scope, failed := in.Binding, *in.Access.ReplacementScope, in.DecisionContext.PostDrainAttempt
	archive, ended, err := n.Store.PostDrainCheckpoint(ctx, b, failed)
	if err != nil {
		return err
	}
	// Historical probes belong to an earlier controller. The deployment
	// must have replaced it before a post-drain continuation is admitted.
	if !ended.Before(p.startedAt) || archive.ArchiveKey != n.Config.BucketPrefix+"/lifecycle/"+failed+".tgz" {
		return ErrConfiguration
	}
	state, err := p.ReadPgcatRequestJournal(ctx, b, scope)
	if err != nil {
		return err
	}
	journal, err := postDrainJournal(state, b, scope, failed, ended)
	if err != nil {
		return err
	}
	if journal.release != nil {
		receiptArchive, ended, err := n.Store.PostDrainCheckpoint(ctx, b, journal.releaseAttempt)
		if err != nil || !ended.Before(p.startedAt) || journal.release.ReleasedAt.After(ended) ||
			receiptArchive.ArchiveKey != n.Config.BucketPrefix+"/lifecycle/"+journal.releaseAttempt+".tgz" {
			return ErrConfiguration
		}
	}
	reference := *in.SessionCheckpoint
	anchorArchive, err := n.Verifier.VerifyCheckpoint(ctx, reference.Archive)
	if err != nil {
		return err
	}
	verified, err := n.Verifier.VerifyCheckpoint(ctx, archive)
	if err != nil {
		return err
	}
	anchor, err := n.Verifier.postDrainSessionFile(ctx, anchorArchive, reference.SessionFile)
	anchorHash := sha256.Sum256(anchor)
	if err != nil || hex.EncodeToString(anchorHash[:]) != reference.SessionSHA256 {
		return ErrCheckpointVerification
	}
	terminal, err := n.Verifier.postDrainSessionFile(ctx, verified, reference.SessionFile)
	if err != nil {
		return err
	}
	hash, err := verifyPostDrainSession(anchor, terminal, reference.SessionID, journal.drain)
	if err != nil {
		return err
	}
	if err := p.restoreDrainObserver(ctx, *in, journal); err != nil {
		return err
	}
	reference.Archive, reference.SessionSHA256 = verified, hash
	in.SessionCheckpoint, in.Checkpoint = &reference, verified
	decision := *in.DecisionContext
	if journal.release == nil {
		decision.Guidance = fmt.Sprintf("The saved expansion and drain are verified. After request_get and request_grant_get, call "+
			"pgcat_connection_continuity alone with {\"phase\":\"release-probes\",\"observationId\":%q,\"drainActionId\":%q}. ",
			journal.drain.ObservationID, journal.drain.DrainActionID)
	} else {
		receipt := journal.release
		decision.ProbeRelease = &execution.PgcatContinuationRelease{RequestID: receipt.RequestID, ScopeDigest: receipt.ScopeDigest,
			ObservationID: receipt.ObservationID, DrainActionID: receipt.DrainActionID, ObservedAt: receipt.ObservedAt, ReleasedAt: receipt.ReleasedAt}
		decision.Guidance = "The expansion, drain and probe release are verified and complete. After request_get and request_grant_get, proceed directly to the application reads. "
	}
	decision.Guidance += "Discover the bound application with Raptor and Infra deployments_list, then read Infra deployment_status_get with its returned workloadKey. " +
		"Read deployment_read, cloud_read fleet, backend and capacity, and metrics_read pgcatClients. Submit deployment_restart once, then follow the selected skill through SQL on both replacement nodes, " +
		"zero original clients, exact retirement and final SQL. Use resource_wait if readiness or traffic is warming."
	in.DecisionContext = &decision
	return nil
}

func (p *ObservationProxy) restoreDrainObserver(ctx context.Context, in execution.LiveStart, journal pgcatPostDrainJournal) error {
	b, scope := in.Binding, in.Access.ReplacementScope
	hash, entry, ok := p.registeredEntry(in)
	if !ok || entry.active == nil {
		return ErrConfiguration
	}
	raw, status := p.guardedExecute(ctx, hash, entry, "cloud_read", json.RawMessage(`{"kind":"capacity"}`))
	var capacity struct {
		GroupID         string `json:"groupId"`
		DesiredCapacity int    `json:"desiredCapacity"`
		MinSize         int    `json:"minSize"`
		MaxSize         int    `json:"maxSize"`
	}
	if status != 200 || json.Unmarshal(raw, &capacity) != nil || capacity.GroupID != scope.GroupID ||
		capacity.DesiredCapacity != 4 || capacity.MinSize != 2 || capacity.MaxSize != 4 {
		return ErrRuntimeUnavailable
	}
	raw, status = p.guardedExecute(ctx, hash, entry, "cloud_read", json.RawMessage(`{"kind":"fleet"}`))
	var fleet struct {
		GroupID   string `json:"groupId"`
		Instances []struct {
			InstanceID           string `json:"instanceId"`
			LifecycleState       string `json:"lifecycleState"`
			HealthStatus         string `json:"healthStatus"`
			ProtectedFromScaleIn bool   `json:"protectedFromScaleIn"`
		} `json:"instances"`
	}
	if status != 200 || json.Unmarshal(raw, &fleet) != nil || fleet.GroupID != scope.GroupID || len(fleet.Instances) != 4 {
		return ErrRuntimeUnavailable
	}
	seen := make(map[string]bool, 4)
	for _, node := range fleet.Instances {
		_, old := journal.hold.Sockets[node.InstanceID]
		if (!old && journal.drain.FreshConnections[node.InstanceID] == 0) || seen[node.InstanceID] ||
			node.LifecycleState != "Protected" || node.HealthStatus != "Healthy" || !node.ProtectedFromScaleIn {
			return ErrRuntimeUnavailable
		}
		seen[node.InstanceID] = true
	}
	raw, status = p.guardedExecute(ctx, hash, entry, "cloud_read", json.RawMessage(`{"kind":"backend"}`))
	var backend struct {
		ServerGroupID string                 `json:"serverGroupId"`
		ListenerID    string                 `json:"listenerId"`
		Nodes         []pgcatObservedBackend `json:"nodes"`
	}
	if status != 200 || json.Unmarshal(raw, &backend) != nil || backend.ServerGroupID != scope.ServerGroupID ||
		backend.ListenerID != scope.ListenerID {
		return ErrRuntimeUnavailable
	}
	original, err := postDrainBackends(backend.Nodes, journal)
	if err != nil {
		return err
	}
	if err := p.pgcat.installRecoveredDrain(ctx, b.RequestID, scope.ResolutionDigest, journal.drain, original, entry.active); err != nil {
		return err
	}
	fail := func(err error) error {
		p.pgcat.release(b.RequestID)
		return err
	}
	through := time.Now()
	if err := p.startPgcatLogWatch(ctx, entry, journal.hold); err != nil {
		return fail(err)
	}
	pair := p.pgcat.get(b.RequestID)
	if pair == nil {
		return fail(ErrRuntimeUnavailable)
	}
	pair.mu.Lock()
	logs := pair.logs
	pair.mu.Unlock()
	if logs == nil || len(logs.streams) != 2 {
		return fail(ErrRuntimeUnavailable)
	}
	tick := time.NewTicker(50 * time.Millisecond)
	defer tick.Stop()
	for {
		complete := true
		for _, stream := range logs.streams {
			covered, err := stream.recoveryCoverage(journal.claim, through)
			if err != nil {
				return fail(err)
			}
			complete = complete && covered
		}
		if complete {
			if journal.release != nil {
				pair.mu.Lock()
				pair.releasedAt, pair.recoveredRelease = journal.release.ReleasedAt, journal.release
				pair.mu.Unlock()
			}
			return nil
		}
		select {
		case <-ctx.Done():
			return fail(ErrRuntimeUnavailable)
		case <-tick.C:
		}
	}
}

func postDrainBackends(nodes []pgcatObservedBackend, journal pgcatPostDrainJournal) (map[string]netip.Addr, error) {
	if len(nodes) != 4 {
		return nil, ErrRuntimeUnavailable
	}
	original := make(map[string]netip.Addr, 2)
	ids, addresses := make(map[string]bool, 4), make(map[netip.Addr]bool, 4)
	for _, node := range nodes {
		addr, err := netip.ParseAddr(node.PrivateIP)
		_, old := journal.hold.Sockets[node.InstanceID]
		if err != nil || !addr.Is4() || !addr.IsPrivate() || ids[node.InstanceID] || addresses[addr] ||
			node.Status != "Available" || (old && node.Registered) ||
			(!old && (journal.drain.FreshConnections[node.InstanceID] == 0 || !node.Registered || !node.Healthy)) {
			return nil, ErrRuntimeUnavailable
		}
		ids[node.InstanceID], addresses[addr] = true, true
		if old {
			original[node.InstanceID] = addr
		}
	}
	if len(original) != 2 {
		return nil, ErrRuntimeUnavailable
	}
	return original, nil
}
