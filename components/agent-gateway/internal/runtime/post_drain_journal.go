package runtime

import (
	"encoding/json"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/jamesDeng/raptor-iap/components/agent-gateway/internal/execution"
)

type pgcatPostDrainJournal struct {
	hold           pgcatHoldEvidence
	drain          pgcatDrainEvidence
	claim          time.Time
	release        *pgcatProbeRelease
	releaseAttempt string
}

func postDrainJournal(state execution.PgcatRequestJournal, b execution.AttemptBinding, scope execution.ReplacementScope, failed string, ended time.Time) (pgcatPostDrainJournal, error) {
	var out pgcatPostDrainJournal
	var drainedAt time.Time
	for _, row := range state.Observations {
		if row.RequestID != b.RequestID || row.ScopeDigest != scope.ResolutionDigest {
			return out, ErrConfiguration
		}
		switch row.Kind {
		case "hold":
			if !out.hold.ObservedAt.IsZero() || row.Subject != "fleet" || json.Unmarshal(row.Data, &out.hold) != nil || !sameJournalTime(row.ObservedAt, out.hold.ObservedAt) {
				return out, ErrConfiguration
			}
		case "drain":
			if !drainedAt.IsZero() || row.Subject != "fleet" || row.AttemptID != failed ||
				json.Unmarshal(row.Data, &out.drain) != nil || !sameJournalTime(row.ObservedAt, out.drain.ObservedAt) {
				return out, ErrConfiguration
			}
			drainedAt = out.drain.ObservedAt
		case "probe-release":
			var receipt pgcatProbeRelease
			if out.release != nil || row.Subject != "fleet" || row.AttemptID == failed || row.AttemptID == b.AttemptID ||
				json.Unmarshal(row.Data, &receipt) != nil || !sameJournalTime(row.ObservedAt, receipt.ReleasedAt) {
				return out, ErrConfiguration
			}
			out.release, out.releaseAttempt = &receipt, row.AttemptID
		case "sql":
		default:
			return out, ErrConfiguration
		}
	}
	if out.hold.RequestID != b.RequestID || out.hold.ScopeDigest != scope.ResolutionDigest ||
		!transportID.MatchString(out.hold.ObservationID) || out.hold.ObservedAt.IsZero() ||
		out.drain.RequestID != b.RequestID || out.drain.ScopeDigest != scope.ResolutionDigest ||
		out.drain.ObservationID != out.hold.ObservationID || drainedAt.IsZero() || drainedAt.After(ended) ||
		out.drain.DrainObservedAt.IsZero() || drainedAt.Before(out.drain.DrainObservedAt) ||
		len(out.hold.Sockets) != 2 || len(out.drain.Sockets) != 2 || len(out.drain.FreshConnections) != 2 {
		return out, ErrConfiguration
	}
	if out.release != nil && !matchesSavedProbeRelease(*out.release, out.drain) {
		return out, ErrConfiguration
	}
	var expectedSockets []string
	total := 0
	for id, count := range out.drain.FreshConnections {
		if count <= 0 || count > 24 || !transportID.MatchString(id) || out.hold.Sockets[id].ID != "" {
			return out, ErrConfiguration
		}
		total += count
	}
	for id, start := range out.hold.Sockets {
		end := out.drain.Sockets[id]
		if !transportID.MatchString(id) || !transportID.MatchString(start.ID) || !start.OpenedAt.Equal(end.OpenedAt) ||
			start.ID != end.ID || start.OpenedAt.IsZero() || start.LastSQLAt.Before(start.OpenedAt) ||
			start.LastSQLAt.After(out.hold.ObservedAt) || start.Rounds < 1 || end.Rounds <= start.Rounds ||
			end.LastSQLAt.Before(out.drain.DrainObservedAt) || end.LastSQLAt.After(drainedAt) {
			return out, ErrConfiguration
		}
		expectedSockets = append(expectedSockets, start.ID)
	}
	if total != 24 || expectedSockets[0] == expectedSockets[1] || len(state.Actions) != 3 {
		return out, ErrConfiguration
	}
	var expandAt, protectAt time.Time
	for _, action := range state.Actions {
		if action.RequestID != b.RequestID || action.ScopeDigest != scope.ResolutionDigest ||
			action.EnvCode != scope.EnvCode || action.Capability.Target.Kind != "db-proxy" ||
			action.Capability.Target.Code != scope.ProxyCode || action.State != "submitted" ||
			!transportID.MatchString(action.ActionID) || action.ProviderRequestID == "" || action.ClaimedAt.IsZero() || action.RecordedAt.Before(action.ClaimedAt) {
			return out, ErrConfiguration
		}
		var args struct {
			RequestID       string   `json:"requestId"`
			EnvCode         string   `json:"envCode"`
			ProxyCode       string   `json:"proxyCode"`
			ActionID        string   `json:"actionId"`
			DesiredCapacity int      `json:"desiredCapacity"`
			NodeIDs         []string `json:"nodeIds"`
			Protected       bool     `json:"protected"`
		}
		if json.Unmarshal(action.Arguments, &args) != nil || args.RequestID != b.RequestID || args.EnvCode != b.EnvCode ||
			args.ProxyCode != scope.ProxyCode {
			return out, ErrConfiguration
		}
		switch action.Capability.Name {
		case "db-proxy.scale-out":
			provider, activity, ok := strings.Cut(action.ProviderRequestID, ":")
			if !expandAt.IsZero() || !ok || provider == "" || !strings.HasPrefix(activity, "asa-") || args.DesiredCapacity != 4 || args.ActionID != action.ActionID {
				return out, ErrConfiguration
			}
			expandAt = action.RecordedAt
		case "db-proxy.node-protection":
			if !protectAt.IsZero() || !args.Protected || !slices.Equal(sortedStrings(args.NodeIDs), slices.Sorted(maps.Keys(out.drain.FreshConnections))) {
				return out, ErrConfiguration
			}
			protectAt = action.RecordedAt
		case "db-proxy.drain":
			if !out.claim.IsZero() || action.ActionID != out.drain.DrainActionID ||
				!slices.Equal(sortedStrings(args.NodeIDs), slices.Sorted(maps.Keys(out.hold.Sockets))) ||
				action.ClaimedAt.Before(out.hold.ObservedAt) || action.RecordedAt.After(out.drain.DrainObservedAt) {
				return out, ErrConfiguration
			}
			out.claim = action.ClaimedAt
		default:
			return out, ErrConfiguration
		}
	}
	if expandAt.IsZero() || protectAt.Before(expandAt) || out.claim.Before(protectAt) {
		return out, ErrConfiguration
	}
	return out, nil
}

func matchesSavedProbeRelease(receipt pgcatProbeRelease, drain pgcatDrainEvidence) bool {
	if receipt.RequestID != drain.RequestID || receipt.ScopeDigest != drain.ScopeDigest || receipt.ObservationID != drain.ObservationID ||
		receipt.DrainActionID != drain.DrainActionID || receipt.Recovery != "previous-controller-cleanup" ||
		!receipt.ObservedAt.Equal(receipt.ReleasedAt) || receipt.ReleasedAt.Before(drain.ObservedAt) || len(receipt.Sockets) != len(drain.Sockets) {
		return false
	}
	for id, socket := range drain.Sockets {
		got := receipt.Sockets[id]
		if got.ID != socket.ID || got.Rounds != socket.Rounds || !got.OpenedAt.Equal(socket.OpenedAt) || !got.LastSQLAt.Equal(socket.LastSQLAt) {
			return false
		}
	}
	return true
}

func sortedStrings(values []string) []string {
	result := slices.Clone(values)
	slices.Sort(result)
	return result
}

// PostgreSQL stores the observation column at microsecond resolution. The
// authenticated JSON evidence preserves the original nanosecond timestamp.
func sameJournalTime(a, b time.Time) bool {
	return a.Truncate(time.Microsecond).Equal(b.Truncate(time.Microsecond))
}
