package runtime

import (
	"bytes"
	"context"
	"encoding/json"
	"net/netip"
	"strconv"
	"strings"
	"time"

	"github.com/jamesDeng/raptor-iap/components/agent-gateway/internal/execution"
)

type pgcatRetiredRestart struct {
	action    string
	claimedAt time.Time
}

type pgcatPostRetirementJournal struct {
	drain   pgcatPostDrainJournal
	restart pgcatRetiredRestart
	scale   execution.ReplacementScaleAction
}

func postRetirementJournal(state execution.PgcatRequestJournal, b execution.AttemptBinding, scope execution.ReplacementScope, failed string, ended time.Time) (pgcatPostRetirementJournal, error) {
	var out pgcatPostRetirementJournal
	var before execution.PgcatRequestJournal
	var drainAttempt string
	var restart, scale execution.PgcatJournalAction
	var traffic pgcatTrafficEvidence
	for _, row := range state.Observations {
		if row.RequestID != b.RequestID || row.ScopeDigest != scope.ResolutionDigest {
			return out, ErrConfiguration
		}
		switch row.Kind {
		case "traffic":
			if !traffic.ObservedAt.IsZero() || row.AttemptID != failed || row.Subject != "fleet" ||
				json.Unmarshal(row.Data, &traffic) != nil || !sameJournalTime(row.ObservedAt, traffic.ObservedAt) {
				return out, ErrConfiguration
			}
		case "retirement", "final-traffic":
			return out, ErrConfiguration
		default:
			if row.Kind == "drain" {
				drainAttempt = row.AttemptID
			}
			before.Observations = append(before.Observations, row)
		}
	}
	for _, action := range state.Actions {
		if action.RequestID != b.RequestID || action.ScopeDigest != scope.ResolutionDigest || action.EnvCode != scope.EnvCode ||
			action.State != "submitted" || !transportID.MatchString(action.ActionID) ||
			action.ClaimedAt.IsZero() || action.RecordedAt.Before(action.ClaimedAt) {
			return out, ErrConfiguration
		}
		switch action.Capability.Name {
		case "application.restart":
			if restart.ActionID != "" {
				return out, ErrConfiguration
			}
			restart = action
		case "db-proxy.scale-in":
			if scale.ActionID != "" {
				return out, ErrConfiguration
			}
			scale = action
		default:
			before.Actions = append(before.Actions, action)
		}
	}
	drain, err := postDrainJournal(before, b, scope, drainAttempt, restart.ClaimedAt)
	if err != nil || drain.release == nil || drain.release.ReleasedAt.After(restart.ClaimedAt) ||
		restart.Capability.Target.Kind != "application" || restart.Capability.Target.Code != scope.Application.AppCode ||
		scale.Capability.Target.Kind != "db-proxy" || scale.Capability.Target.Code != scope.ProxyCode ||
		traffic.RestartActionID != restart.ActionID || traffic.ApplicationUID != scope.Application.UID || traffic.Generation < 1 || !traffic.NoErrors ||
		traffic.ObservedAt.Before(restart.RecordedAt) || traffic.ObservedAt.After(scale.ClaimedAt) || scale.RecordedAt.After(ended) ||
		restart.ProviderRequestID != scope.Application.UID+":"+strconv.FormatInt(traffic.Generation, 10) ||
		len(traffic.Nodes) != 2 || len(drain.drain.FreshConnections) != 2 {
		return out, ErrConfiguration
	}
	for id := range drain.drain.FreshConnections {
		if counter := traffic.Nodes[id]; counter.Queries < 1 || counter.Transactions < 1 {
			return out, ErrConfiguration
		}
	}
	var args struct {
		RequestID       string `json:"requestId"`
		EnvCode         string `json:"envCode"`
		ProxyCode       string `json:"proxyCode"`
		ActionID        string `json:"actionId"`
		DesiredCapacity int    `json:"desiredCapacity"`
	}
	provider, activity, ok := strings.Cut(scale.ProviderRequestID, ":")
	if !ok || provider == "" || !strings.HasPrefix(activity, "asa-") || !transportID.MatchString(activity) ||
		json.Unmarshal(scale.Arguments, &args) != nil || args.RequestID != b.RequestID || args.EnvCode != b.EnvCode ||
		args.ProxyCode != scope.ProxyCode || args.ActionID != scale.ActionID || args.DesiredCapacity != 2 {
		return out, ErrConfiguration
	}
	return pgcatPostRetirementJournal{drain: drain, restart: pgcatRetiredRestart{action: restart.ActionID, claimedAt: restart.ClaimedAt},
		scale: execution.ReplacementScaleAction{ActionID: scale.ActionID, Previous: 4, Desired: 2, Outcome: "submitted", ScalingActivityID: activity}}, nil
}

func (n *NativeLive) restorePostRetirement(ctx context.Context, in *execution.LiveStart) error {
	if in.DecisionContext == nil || in.DecisionContext.PostRetirementAttempt == "" {
		return nil
	}
	p := n.Observations
	if p == nil || p.ReadPgcatRequestJournal == nil || in.SessionCheckpoint == nil || in.Access.ReplacementScope == nil {
		return ErrConfiguration
	}
	ctx, cancel := context.WithTimeout(ctx, 50*time.Second)
	defer cancel()
	b, scope, failed := in.Binding, *in.Access.ReplacementScope, in.DecisionContext.PostRetirementAttempt
	archive, ended, err := n.Store.PostDrainCheckpoint(ctx, b, failed)
	if err != nil {
		return err
	}
	if archive.ArchiveKey != n.Config.BucketPrefix+"/lifecycle/"+failed+".tgz" {
		return ErrConfiguration
	}
	state, err := p.ReadPgcatRequestJournal(ctx, b, scope)
	if err != nil {
		return err
	}
	journal, err := postRetirementJournal(state, b, scope, failed, ended)
	if err != nil {
		return err
	}
	reference := *in.SessionCheckpoint
	anchorArchive, err := n.Verifier.VerifyCheckpoint(ctx, reference.Archive)
	if err != nil {
		return err
	}
	anchor, err := n.Verifier.postDrainSessionFile(ctx, anchorArchive, reference.SessionFile)
	if err != nil || digestBytes(anchor) != reference.SessionSHA256 {
		return ErrCheckpointVerification
	}
	verified, err := n.Verifier.VerifyCheckpoint(ctx, archive)
	if err != nil {
		return err
	}
	terminal, err := n.Verifier.postDrainSessionFile(ctx, verified, reference.SessionFile)
	if err != nil || len(terminal) <= len(anchor) || !bytes.HasPrefix(terminal, anchor) {
		return ErrCheckpointVerification
	}
	_, entry, ok := p.registeredEntry(*in)
	if !ok || entry.active == nil {
		return ErrConfiguration
	}
	originals := make(map[string]netip.Addr, 2)
	for id := range journal.drain.hold.Sockets {
		originals[id] = netip.Addr{}
	}
	if err := p.pgcat.installRecoveredDrain(ctx, b.RequestID, scope.ResolutionDigest, journal.drain.drain, originals, entry.active); err != nil {
		return err
	}
	pair := p.pgcat.get(b.RequestID)
	if pair == nil {
		return ErrRuntimeUnavailable
	}
	pair.mu.Lock()
	pair.releasedAt = journal.drain.release.ReleasedAt
	pair.retiredRestart = &journal.restart
	pair.mu.Unlock()
	reference.Archive, reference.SessionSHA256 = verified, digestBytes(terminal)
	in.SessionCheckpoint, in.Checkpoint = &reference, verified
	decision := *in.DecisionContext
	decision.Retirement = &journal.scale
	decision.Guidance = "Continue the original request with reads only. The restart and scale-in receipts are saved. Observe the exact retirement and two retained nodes, then verify fresh final SQL on both nodes. Use resource_wait while the provider or traffic is still settling."
	in.DecisionContext = &decision
	return nil
}
