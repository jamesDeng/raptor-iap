package commands

import (
	"context"
	"encoding/json"
	"math"
	"slices"
	"strconv"
	"time"

	"raptor-iap/infra-api/internal/approval"
	"raptor-iap/infra-api/internal/domain"
	"raptor-iap/infra-api/internal/policy"
)

type PgcatGrantInput struct {
	Target      domain.ProxyTarget
	ScopeDigest string
	ActionID    string
	Operation   any
	State       approval.PgcatRequestState
}

type PreparedPgcatGrant struct {
	Capability  string
	Constraints json.RawMessage
	Submit      func(context.Context) (any, string, error)
}

type pgcatMutationBackend interface {
	ProtectWithReceipt(context.Context, domain.Environment, string, []string, bool) (string, error)
	DrainWithReceipt(context.Context, domain.Environment, string, []string) (string, error)
	RetirePgcat(context.Context, domain.Environment, domain.PgcatRetirement) (domain.PgcatRetirementReceipt, error)
}

type pgcatSocketWitness struct {
	ID        string    `json:"id"`
	OpenedAt  time.Time `json:"openedAt"`
	LastSQLAt time.Time `json:"lastSqlAt"`
	Rounds    int       `json:"rounds"`
}

type pgcatHoldWitness struct {
	RequestID     string                        `json:"requestId"`
	ScopeDigest   string                        `json:"scopeDigest"`
	ObservationID string                        `json:"observationId"`
	ObservedAt    time.Time                     `json:"observedAt"`
	Sockets       map[string]pgcatSocketWitness `json:"sockets"`
}

type pgcatDrainWitness struct {
	pgcatHoldWitness
	DrainActionID    string         `json:"drainActionId"`
	DrainObservedAt  time.Time      `json:"drainObservedAt"`
	FreshConnections map[string]int `json:"freshConnections"`
}

type pgcatReleaseWitness struct {
	pgcatHoldWitness
	DrainActionID string    `json:"drainActionId"`
	ReleasedAt    time.Time `json:"releasedAt"`
}

type pgcatTrafficWitness struct {
	RestartActionID string `json:"restartActionId"`
	ApplicationUID  string `json:"applicationUid"`
	Generation      int64  `json:"generation"`
	NoErrors        bool   `json:"noErrors"`
	Nodes           map[string]struct {
		Queries      int64 `json:"queries"`
		Transactions int64 `json:"transactions"`
	} `json:"nodes"`
}

func pgcatFresh(at, now time.Time) bool {
	return !at.IsZero() && !at.After(now.Add(2*time.Second)) && now.Sub(at) <= 30*time.Second
}

func pgcatIDs(a, b []string) bool {
	first, second := slices.Clone(a), slices.Clone(b)
	slices.Sort(first)
	slices.Sort(second)
	return slices.Equal(first, second)
}

type pgcatPhase struct {
	hold      pgcatHoldWitness
	drain     pgcatDrainWitness
	release   pgcatReleaseWitness
	traffic   pgcatTrafficWitness
	trafficAt time.Time
	sql       map[string]time.Time
	actions   map[string]approval.GrantActionDetails
}

func readPgcatPhase(in PgcatGrantInput) (pgcatPhase, error) {
	out := pgcatPhase{sql: map[string]time.Time{}, actions: map[string]approval.GrantActionDetails{}}
	for _, action := range in.State.Actions {
		if action.RequestID != in.Target.RequestID || action.EnvCode != in.Target.EnvCode || action.ScopeDigest != in.ScopeDigest {
			return out, domain.ErrScope
		}
		if action.State != "submitted" {
			continue
		}
		if action.ProviderRequestID == "" || action.RecordedAt == nil || action.ClaimedAt.IsZero() || action.RecordedAt.Before(action.ClaimedAt) {
			return out, failure("MissingEvidence")
		}
		out.actions[action.Capability.Capability] = action
	}
	for _, observation := range in.State.Observations {
		if observation.RequestID != in.Target.RequestID || observation.ScopeDigest != in.ScopeDigest {
			return out, domain.ErrScope
		}
		var target any
		switch observation.Kind {
		case "hold":
			target = &out.hold
		case "drain":
			target = &out.drain
		case "probe-release":
			target = &out.release
		case "traffic":
			target = &out.traffic
			out.trafficAt = observation.ObservedAt
		case "sql":
			var sql struct {
				InstanceID string `json:"instanceId"`
				Connected  bool   `json:"connected"`
			}
			if json.Unmarshal(observation.Data, &sql) != nil || !sql.Connected || sql.InstanceID != observation.Subject {
				return out, failure("MissingEvidence")
			}
			out.sql[sql.InstanceID] = observation.ObservedAt
		}
		if target != nil && json.Unmarshal(observation.Data, target) != nil {
			return out, failure("MissingEvidence")
		}
	}
	if out.hold.RequestID != in.Target.RequestID || out.hold.ScopeDigest != in.ScopeDigest || out.hold.ObservationID == "" ||
		len(out.hold.Sockets) != 2 || out.hold.ObservedAt.IsZero() {
		return out, failure("MissingEvidence")
	}
	for id, socket := range out.hold.Sockets {
		if id == "" || socket.ID == "" || socket.OpenedAt.IsZero() || socket.LastSQLAt.Before(socket.OpenedAt) || socket.Rounds < 1 {
			return out, failure("MissingEvidence")
		}
	}
	return out, nil
}

func (p pgcatPhase) cohort(s policy.ProxySnapshot) (old, added []string, err error) {
	for _, node := range s.Nodes {
		if _, original := p.hold.Sockets[node.ID]; original {
			old = append(old, node.ID)
		} else {
			added = append(added, node.ID)
		}
	}
	if len(old) != 2 || len(added) != s.Desired-2 || len(s.Nodes) != s.Desired || s.Min != 2 || s.Max != 4 {
		return nil, nil, failure("TargetChanged")
	}
	return old, added, nil
}

func (p pgcatPhase) verifiesDrain(originals, replacements []string, now time.Time) bool {
	action, ok := p.actions["db-proxy.drain"]
	d := p.drain
	if !ok || d.DrainActionID != action.ActionID || d.DrainObservedAt.Before(*action.RecordedAt) ||
		d.ObservedAt.Before(d.DrainObservedAt) || !pgcatFresh(d.ObservedAt, now) ||
		d.RequestID != p.hold.RequestID || d.ScopeDigest != p.hold.ScopeDigest || d.ObservationID != p.hold.ObservationID ||
		len(d.FreshConnections) != 2 || len(d.Sockets) != 2 {
		return false
	}
	for _, id := range originals {
		start, end := p.hold.Sockets[id], d.Sockets[id]
		if start.ID != end.ID || !start.OpenedAt.Equal(end.OpenedAt) || end.Rounds <= start.Rounds ||
			end.LastSQLAt.Before(d.DrainObservedAt) || !pgcatFresh(end.LastSQLAt, now) {
			return false
		}
	}
	total := 0
	for _, id := range replacements {
		if d.FreshConnections[id] <= 0 {
			return false
		}
		total += d.FreshConnections[id]
	}
	return total == 24
}

func (p pgcatPhase) releasedProbes(before time.Time) bool {
	release := p.release
	return p.drain.DrainActionID != "" && release.DrainActionID == p.drain.DrainActionID &&
		release.RequestID == p.hold.RequestID && release.ScopeDigest == p.hold.ScopeDigest &&
		release.ObservationID == p.hold.ObservationID && !p.drain.ObservedAt.IsZero() &&
		!release.ReleasedAt.Before(p.drain.ObservedAt) && !release.ReleasedAt.After(before)
}

func (r Runtime) pgcatClients(ctx context.Context, snapshot policy.ProxySnapshot, now time.Time, requireZero []string) error {
	if r.Metrics == nil {
		return domain.ErrNotConfigured
	}
	rows, err := r.Metrics.ConnectedClients(ctx, snapshot)
	if err != nil {
		return err
	}
	if len(rows) != len(snapshot.Nodes) {
		return failure("MetricsInvalid")
	}
	counts := map[string]float64{}
	for _, row := range rows {
		if _, duplicate := counts[row.InstanceID]; duplicate || !pgcatFresh(row.ObservedAt, now) ||
			math.IsNaN(row.Clients) || math.IsInf(row.Clients, 0) || row.Clients < 0 || math.Trunc(row.Clients) != row.Clients {
			return failure("MetricsInvalid")
		}
		counts[row.InstanceID] = row.Clients
	}
	for _, node := range snapshot.Nodes {
		if _, ok := counts[node.ID]; !ok {
			return failure("MetricsInvalid")
		}
	}
	for _, id := range requireZero {
		if count, ok := counts[id]; !ok || count != 0 {
			return failure("ConnectedClientsPresent")
		}
	}
	return nil
}

func (r Runtime) PreparePgcatGrant(ctx context.Context, env domain.Environment, in PgcatGrantInput) (PreparedPgcatGrant, error) {
	var out PreparedPgcatGrant
	backend, ok := r.Backend.(pgcatMutationBackend)
	if !ok || in.ActionID == "" {
		return out, domain.ErrNotConfigured
	}
	phase, err := readPgcatPhase(in)
	if err != nil {
		return out, err
	}
	snapshot, err := r.snapshot(ctx, env, in.Target)
	if err != nil {
		return out, err
	}
	originals, replacements, err := phase.cohort(snapshot)
	if err != nil {
		return out, err
	}
	now := time.Now()
	if r.Now != nil {
		now = r.Now()
	}
	expanded := phase.actions["db-proxy.scale-out"]
	var invoke func(context.Context) (any, string, error)
	set := func(capability string, constraints string) {
		out.Capability = capability
		out.Constraints = json.RawMessage(constraints)
	}
	switch command := in.Operation.(type) {
	case domain.ScaleCommand:
		variant, err := policy.ScaleApprovalVariant(snapshot, command.DesiredCapacity)
		if err != nil {
			return out, err
		}
		if variant.Capability == "db-proxy.scale-out" && snapshot.Desired == 2 && command.DesiredCapacity == 4 {
			if expanded.ActionID != "" || !pgcatFresh(phase.hold.ObservedAt, now) {
				return out, failure("MissingEvidence")
			}
			for _, node := range snapshot.Nodes {
				if !node.Protected || !node.Healthy || !node.Registered || node.Drained ||
					!pgcatFresh(phase.hold.Sockets[node.ID].LastSQLAt, now) {
					return out, failure("MissingEvidence")
				}
			}
			set(variant.Capability, `{"desiredCapacity":4}`)
			invoke = func(call context.Context) (any, string, error) {
				ack, err := r.Backend.Scale(call, env, snapshot.GroupID, 4)
				return domain.ScaleReceipt{Outcome: "submitted", GroupID: snapshot.GroupID, PreviousDesiredCapacity: 2, DesiredCapacity: 4,
					ProviderRequestID: ack, EvidenceMode: r.evidence()}, ack, err
			}
			break
		}
		if variant.Capability != "db-proxy.scale-in" || command.DesiredCapacity != 2 ||
			policy.TargetedPgcatRetirement(snapshot, originals, replacements) != nil {
			return out, failure("TargetChanged")
		}
		restart := phase.actions["application.restart"]
		traffic := phase.traffic
		if restart.ActionID == "" || !phase.releasedProbes(restart.ClaimedAt) || traffic.RestartActionID != restart.ActionID || traffic.ApplicationUID == "" ||
			restart.ProviderRequestID != traffic.ApplicationUID+":"+strconv.FormatInt(traffic.Generation, 10) ||
			!traffic.NoErrors || !pgcatFresh(phase.trafficAt, now) || phase.trafficAt.Before(*restart.RecordedAt) || len(traffic.Nodes) != 2 {
			return out, failure("MissingEvidence")
		}
		for _, id := range replacements {
			delta := traffic.Nodes[id]
			if delta.Queries < 1 || delta.Transactions < 1 {
				return out, failure("MissingEvidence")
			}
		}
		if err := r.pgcatClients(ctx, snapshot, now, originals); err != nil {
			return out, err
		}
		set(variant.Capability, `{"desiredCapacity":2}`)
		invoke = func(call context.Context) (any, string, error) {
			receipt, err := backend.RetirePgcat(call, env, domain.PgcatRetirement{ProxyTarget: in.Target, ActionID: in.ActionID,
				OriginalIDs: originals, ReplacementIDs: replacements})
			data := domain.ScaleReceipt{Outcome: "submitted", GroupID: snapshot.GroupID, PreviousDesiredCapacity: 4, DesiredCapacity: 2,
				ProviderRequestID: receipt.ProviderRequestID, ScalingActivityID: receipt.ScalingActivityID, EvidenceMode: r.evidence()}
			if err != nil {
				return nil, "", err
			}
			return data, receipt.ProviderRequestID + ":" + receipt.ScalingActivityID, nil
		}
	case domain.ProtectionCommand, domain.DeregisterCommand:
		if expanded.ActionID == "" || snapshot.Desired != 4 {
			return out, failure("MissingEvidence")
		}
		for _, node := range snapshot.Nodes {
			if !node.Registered || !node.Healthy || node.Drained {
				return out, failure("MissingEvidence")
			}
		}
		for _, id := range replacements {
			if at := phase.sql[id]; !pgcatFresh(at, now) || at.Before(*expanded.RecordedAt) {
				return out, failure("MissingEvidence")
			}
		}
		if err := r.pgcatClients(ctx, snapshot, now, nil); err != nil {
			return out, err
		}
		if protection, ok := command.(domain.ProtectionCommand); ok {
			if !protection.Protected || !pgcatIDs(protection.InstanceIDs, replacements) {
				return out, domain.ErrScope
			}
			set("db-proxy.node-protection", `{"protected":true}`)
			invoke = func(call context.Context) (any, string, error) {
				ack, err := backend.ProtectWithReceipt(call, env, snapshot.GroupID, replacements, true)
				return domain.NodeReceipt{Outcome: "submitted", InstanceIDs: replacements, EvidenceMode: r.evidence()}, ack, err
			}
		} else {
			drain := command.(domain.DeregisterCommand)
			if !pgcatIDs(drain.InstanceIDs, originals) || drain.ServerGroupID != snapshot.ServerGroupID {
				return out, domain.ErrScope
			}
			if err := policy.DeregistrationAllowed(snapshot, drain.ServerGroupID, originals); err != nil {
				return out, err
			}
			set("db-proxy.drain", `{"weight":0}`)
			invoke = func(call context.Context) (any, string, error) {
				ack, err := backend.DrainWithReceipt(call, env, snapshot.ServerGroupID, originals)
				if err == nil && ack == "" {
					return nil, "", failure("TargetChanged")
				}
				return domain.NodeReceipt{Outcome: "submitted", InstanceIDs: originals, EvidenceMode: r.evidence()}, ack, err
			}
		}
	case domain.RestartCommand:
		if snapshot.Desired != 4 || r.Restarter == nil || phase.actions["application.restart"].ActionID != "" ||
			policy.TargetedPgcatRetirement(snapshot, originals, replacements) != nil || !phase.verifiesDrain(originals, replacements, now) || !phase.releasedProbes(now) {
			return out, failure("MissingEvidence")
		}
		set("application.restart", `{}`)
		invoke = func(call context.Context) (any, string, error) {
			receipt, err := r.Restarter.Restart(call, env, command)
			if err != nil {
				return nil, "", err
			}
			if !receipt.Accepted || receipt.Target != command || receipt.Generation < 1 {
				return nil, "", failure("SubmissionUnknown")
			}
			return receipt, command.UID + ":" + strconv.FormatInt(receipt.Generation, 10), nil
		}
	default:
		return out, domain.ErrScope
	}
	out.Submit = func(call context.Context) (any, string, error) {
		if err := r.unchanged(call, env, in.Target, snapshot); err != nil {
			return nil, "", err
		}
		return invoke(call)
	}
	return out, nil
}
