package runtime

import (
	"context"
	"encoding/json"
	"net"
	"net/netip"
	"net/url"
	"regexp"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jamesDeng/raptor-iap/components/agent-gateway/internal/execution"
)

const pgcatContinuityTool = "pgcat_connection_continuity"

var pgcatHostPattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9.-]{1,251}[a-zA-Z0-9]$`)

func validPgcatHost(host string) bool {
	if addr, err := netip.ParseAddr(host); err == nil {
		return addr.Is4() && addr.IsPrivate()
	}
	return pgcatHostPattern.MatchString(host)
}

type pgcatContinuityArgs struct {
	Phase           string `json:"phase"`
	ObservationID   string `json:"observationId,omitempty"`
	DrainActionID   string `json:"drainActionId,omitempty"`
	RestartActionID string `json:"restartActionId,omitempty"`
}

func validPgcatContinuityArgs(raw json.RawMessage) bool {
	var args pgcatContinuityArgs
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &args) != nil || json.Unmarshal(raw, &fields) != nil || fields["phase"] == nil {
		return false
	}
	switch args.Phase {
	case "hold":
		return len(fields) == 1
	case "verify-drain", "release-probes":
		return len(fields) == 3 && fields["observationId"] != nil && fields["drainActionId"] != nil &&
			transportID.MatchString(args.ObservationID) && transportID.MatchString(args.DrainActionID)
	case "verify-traffic", "verify-final-traffic":
		return len(fields) == 3 && fields["observationId"] != nil && fields["restartActionId"] != nil &&
			transportID.MatchString(args.ObservationID) && transportID.MatchString(args.RestartActionID)
	}
	return false
}

type pgcatSQLSocket struct{ conn *pgx.Conn }

func (s pgcatSQLSocket) backendAddress(ctx context.Context) (netip.Addr, error) {
	var addr string
	// The database sees the backend PgCat address as its client. This query
	// identifies the target selected by the NLB without relying on load metrics.
	if err := s.conn.QueryRow(ctx, "SELECT host(inet_client_addr())").Scan(&addr); err != nil {
		return netip.Addr{}, err
	}
	return netip.ParseAddr(addr)
}

func (s pgcatSQLSocket) close() {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_ = s.conn.Close(ctx)
}

func privatePgcatConnector(material ObservationMaterial, port int) (pgcatConnector, error) {
	var source struct {
		PgcatHost string `json:"pgcatHost"`
		Private   struct {
			SQL struct {
				User, Password, Database, SSLMode string
			} `json:"sql"`
		} `json:"private"`
	}
	if json.Unmarshal(material.Config, &source) != nil || !validPgcatHost(source.PgcatHost) || port != 6432 {
		return nil, ErrConfiguration
	}
	sql := source.Private.SQL
	if !transportID.MatchString(sql.User) || !transportID.MatchString(sql.Database) || sql.Password == "" ||
		(sql.SSLMode != "disable" && sql.SSLMode != "verify-full") {
		return nil, ErrConfiguration
	}
	endpoint := &url.URL{Scheme: "postgresql", Host: net.JoinHostPort(source.PgcatHost, strconv.Itoa(port)),
		User: url.UserPassword(sql.User, sql.Password), Path: "/" + sql.Database}
	endpoint.RawQuery = url.Values{"sslmode": {sql.SSLMode}, "connect_timeout": {"5"}}.Encode()
	cfg, err := pgx.ParseConfig(endpoint.String())
	if err != nil || len(cfg.Fallbacks) != 0 {
		return nil, ErrConfiguration
	}
	cfg.DefaultQueryExecMode = pgx.QueryExecModeExec
	cfg.RuntimeParams = map[string]string{"options": "-c default_transaction_read_only=on -c statement_timeout=5000"}
	cfg.LookupFunc = func(ctx context.Context, hostname string) ([]string, error) {
		if hostname != source.PgcatHost {
			return nil, ErrConfiguration
		}
		addresses, err := net.DefaultResolver.LookupNetIP(ctx, "ip4", hostname)
		if err != nil || len(addresses) == 0 {
			return nil, ErrRuntimeUnavailable
		}
		out := make([]string, 0, len(addresses))
		for _, addr := range addresses {
			if !addr.Is4() || !addr.IsPrivate() {
				return nil, ErrConfiguration
			}
			out = append(out, addr.String())
		}
		return out, nil
	}
	return func(ctx context.Context) (pgcatSocket, error) {
		conn, err := pgx.ConnectConfig(ctx, cfg.Copy())
		if err != nil {
			return nil, ErrRuntimeUnavailable
		}
		return pgcatSQLSocket{conn: conn}, nil
	}, nil
}

type pgcatObservedBackend struct {
	InstanceID string `json:"instanceId"`
	PrivateIP  string `json:"privateIp"`
	Status     string `json:"status"`
	Registered bool   `json:"registered"`
	Healthy    bool   `json:"healthy"`
}

func (p *ObservationProxy) pgcatObservation(ctx context.Context, entry observationEntry, raw json.RawMessage) (json.RawMessage, error) {
	var args pgcatContinuityArgs
	if json.Unmarshal(raw, &args) != nil || !validPgcatContinuityArgs(raw) ||
		entry.start.Binding.ApprovalProtocol != "request-grants-v1" || entry.active == nil {
		return nil, ErrConfiguration
	}
	binding, scope := entry.start.Binding, entry.start.Access.ReplacementScope
	if args.Phase == "release-probes" {
		release, err := p.pgcat.releaseProbes(ctx, binding.RequestID, scope.ResolutionDigest, args.ObservationID, args.DrainActionID)
		if err != nil {
			return nil, err
		}
		return json.Marshal(release)
	}
	connect, err := privatePgcatConnector(entry.material, scope.Port)
	if err != nil {
		return nil, err
	}
	var drain execution.PgcatDrainAction
	if args.Phase == "verify-drain" {
		if p.ReadPgcatDrainAction == nil {
			return nil, ErrConfiguration
		}
		drain, err = p.ReadPgcatDrainAction(ctx, binding.RequestID, scope.EnvCode, scope.ProxyCode, scope.ResolutionDigest, args.DrainActionID)
		if err != nil || !p.pgcat.matchesOriginalNodes(binding.RequestID, drain.NodeIDs) {
			return nil, ErrRuntimeUnavailable
		}
	}
	var backend struct {
		ServerGroupID string                 `json:"serverGroupId"`
		ListenerID    string                 `json:"listenerId"`
		ObservedAt    time.Time              `json:"observedAt"`
		Nodes         []pgcatObservedBackend `json:"nodes"`
	}
	read := func(kind string, out any) error {
		input, _ := json.Marshal(map[string]string{"kind": kind})
		result, err := p.execute(ctx, entry.start, entry.material, "cloud_read", input)
		if err != nil || json.Unmarshal(result, out) != nil {
			return ErrRuntimeUnavailable
		}
		return nil
	}
	if err := read("backend", &backend); err != nil || backend.ServerGroupID != scope.ServerGroupID ||
		backend.ListenerID != scope.ListenerID || backend.ObservedAt.IsZero() {
		return nil, ErrRuntimeUnavailable
	}
	all := make(map[string]netip.Addr, len(backend.Nodes))
	routed := make(map[string]netip.Addr, 2)
	for _, node := range backend.Nodes {
		addr, err := netip.ParseAddr(node.PrivateIP)
		if err != nil || !addr.Is4() || !addr.IsPrivate() || !transportID.MatchString(node.InstanceID) ||
			all[node.InstanceID].IsValid() || node.Status != "Available" || (node.Registered && !node.Healthy) {
			return nil, ErrRuntimeUnavailable
		}
		all[node.InstanceID] = addr
		if node.Registered {
			routed[node.InstanceID] = addr
		}
	}
	switch args.Phase {
	case "hold":
		var capacity struct {
			DesiredCapacity int       `json:"desiredCapacity"`
			GroupID         string    `json:"groupId"`
			ObservedAt      time.Time `json:"observedAt"`
		}
		if len(all) != 2 || len(routed) != 2 || read("capacity", &capacity) != nil || capacity.GroupID != scope.GroupID ||
			capacity.DesiredCapacity != 2 || capacity.ObservedAt.IsZero() {
			return nil, ErrRuntimeUnavailable
		}
		evidence, err := p.pgcat.hold(ctx, binding.RequestID, scope.ResolutionDigest, routed, backend.ObservedAt, connect, entry.active)
		if err != nil {
			return nil, err
		}
		if err := p.startPgcatLogWatch(ctx, entry, evidence); err != nil {
			return nil, err
		}
		return json.Marshal(evidence)
	case "verify-drain":
		if len(all) != 4 || len(routed) != 2 || backend.ObservedAt.Before(drain.RecordedAt) ||
			!p.pgcat.matchesOriginalBackends(binding.RequestID, all) {
			return nil, ErrRuntimeUnavailable
		}
		evidence, err := p.pgcat.verifyDrain(ctx, binding.RequestID, scope.ResolutionDigest, args.ObservationID,
			args.DrainActionID, drain.ClaimedAt, backend.ObservedAt, routed, connect)
		if err != nil {
			return nil, err
		}
		return json.Marshal(evidence)
	case "verify-traffic", "verify-final-traffic":
		if args.Phase == "verify-traffic" {
			if len(all) != 4 || !p.pgcat.matchesOriginalBackends(binding.RequestID, all) {
				return nil, ErrRuntimeUnavailable
			}
		} else {
			var retired execution.PgcatResultRetirement
			if len(all) != 2 || read("retirement", &retired) != nil || retired.Status != "Successful" ||
				retired.ObservedAt.IsZero() || !p.pgcat.matchesOriginalNodes(binding.RequestID, retired.DestroyedIDs) ||
				p.RecordPgcatObservation == nil {
				return nil, ErrRuntimeUnavailable
			}
			raw, err := json.Marshal(retired)
			if err != nil {
				return nil, ErrRuntimeUnavailable
			}
			if err := p.RecordPgcatObservation(ctx, execution.PgcatObservationRecord{
				RequestID: binding.RequestID, AttemptID: binding.AttemptID, ScopeDigest: scope.ResolutionDigest,
				Kind: "retirement", Subject: "fleet", ObservedAt: retired.ObservedAt, Data: raw,
			}); err != nil {
				return nil, err
			}
		}
		return p.pgcatTraffic(ctx, entry, args, routed)
	}
	return nil, ErrConfiguration
}

func (p *ObservationProxy) recordPgcatObservation(ctx context.Context, entry observationEntry, tool string, args, result json.RawMessage) error {
	binding, scope := entry.start.Binding, entry.start.Access.ReplacementScope
	row := execution.PgcatObservationRecord{RequestID: binding.RequestID, AttemptID: binding.AttemptID,
		ScopeDigest: scope.ResolutionDigest, Subject: "fleet", Data: result}
	switch tool {
	case pgcatContinuityTool:
		var in pgcatContinuityArgs
		var proof pgcatHoldEvidence
		if json.Unmarshal(args, &in) != nil || json.Unmarshal(result, &proof) != nil {
			return ErrRuntimeUnavailable
		}
		switch in.Phase {
		case "hold":
			row.Kind, row.ObservedAt = "hold", proof.ObservedAt
		case "verify-drain":
			row.Kind, row.ObservedAt = "drain", proof.ObservedAt
		case "release-probes":
			var released pgcatProbeRelease
			if json.Unmarshal(result, &released) != nil {
				return ErrRuntimeUnavailable
			}
			row.Kind, row.ObservedAt = "probe-release", released.ReleasedAt
		case "verify-traffic", "verify-final-traffic":
			var traffic pgcatTrafficEvidence
			if json.Unmarshal(result, &traffic) != nil {
				return ErrRuntimeUnavailable
			}
			if !traffic.NoErrors {
				return nil
			}
			row.Kind, row.ObservedAt = "traffic", traffic.ObservedAt
			if in.Phase == "verify-final-traffic" {
				row.Kind = "final-traffic"
			}
		}
	case "db_connection_probe":
		var proof struct {
			InstanceID string `json:"instanceId"`
			Connected  bool   `json:"connected"`
		}
		if json.Unmarshal(result, &proof) != nil {
			return ErrRuntimeUnavailable
		}
		if !proof.Connected {
			return nil
		}
		row.Kind, row.Subject, row.ObservedAt = "sql", proof.InstanceID, time.Now().UTC()
	default:
		return nil
	}
	if p.RecordPgcatObservation == nil {
		return ErrConfiguration
	}
	return p.RecordPgcatObservation(ctx, row)
}

func validPgcatInternalObservation(binding execution.AttemptBinding, tool string, args json.RawMessage) bool {
	var fields map[string]json.RawMessage
	if binding.ApprovalProtocol != "request-grants-v1" || tool != "pgcat_client_pods" || json.Unmarshal(args, &fields) != nil || fields == nil {
		return false
	}
	if len(fields) == 0 {
		return true
	}
	var ids []string
	return len(fields) == 1 && json.Unmarshal(fields["retiringPodUids"], &ids) == nil && len(ids) == 2 &&
		ids[0] != ids[1] && transportID.MatchString(ids[0]) && transportID.MatchString(ids[1])
}
