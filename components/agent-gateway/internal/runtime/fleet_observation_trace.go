package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"regexp"
	"time"

	"github.com/jamesDeng/raptor-iap/components/agent-gateway/internal/execution"
)

type fleetObservationDiagnostic struct {
	Stage         string
	Category      string
	UpstreamID    string
	HTTPStatus    int
	ObservationID string
	ElapsedMS     int64
	InfraFailure  *fleetInfraDiagnostic
}

type fleetInfraDiagnostic struct {
	Stage      string
	Category   string
	ElapsedMS  int64
	UpstreamID string
	HTTPStatus int
}

var functionRequestID = regexp.MustCompile(`(?i)^1-[0-9a-f]{8}-(?:[0-9a-f]{24}|[0-9a-f]{8}-[0-9a-f]{12})$`)
var fleetTraceID = regexp.MustCompile(`(?i)^(?:[0-9a-f]{32}|[0-9a-f]{8}(?:-[0-9a-f]{4}){3}-[0-9a-f]{12})$`)

func safeFleetTraceID(value string) string {
	if fleetTraceID.MatchString(value) {
		return value
	}
	return ""
}

func isFleetObservation(tool string, args json.RawMessage) bool {
	var cloud struct{ Kind string }
	return tool == "cloud_read" && json.Unmarshal(args, &cloud) == nil && cloud.Kind == "fleet"
}

// Treat subprocess output as untrusted. Log only known fields and categories.
func parseFleetObservationDiagnostic(raw []byte) (fleetObservationDiagnostic, bool) {
	var d fleetObservationDiagnostic
	if json.Unmarshal(raw, &d) != nil {
		return d, false
	}
	switch d.Stage {
	case "input_validation", "mcp_connect", "mcp_call", "observation_validation":
	default:
		return d, false
	}
	switch d.Category {
	case "invalid_input", "upstream_transport", "upstream_http", "upstream_response", "upstream_tool_error", "invalid_observation", "deadline":
	default:
		return d, false
	}
	if !functionRequestID.MatchString(d.UpstreamID) {
		d.UpstreamID = safeFleetTraceID(d.UpstreamID)
	}
	d.ObservationID = safeFleetTraceID(d.ObservationID)
	if d.InfraFailure != nil && !d.InfraFailure.sanitize() {
		d.InfraFailure = nil
	}
	return d, diagnosticStatus(d.HTTPStatus) && d.ElapsedMS >= 0 && d.ElapsedMS <= 20000
}

func diagnosticStatus(status int) bool {
	return status == 0 || status >= 100 && status <= 599
}

func (d *fleetInfraDiagnostic) sanitize() bool {
	switch d.Stage {
	case "input_validation", "authorization", "resource_resolution", "resolution_proof", "provider_observation",
		"ess_scaling_groups_by_tags", "ess_scaling_group_by_id", "ess_scaling_instances",
		"resolve_ess_scaling_groups", "resolve_ess_scaling_group_by_id":
	default:
		return false
	}
	switch d.Category {
	case "throttled", "provider_api_error", "provider_access_denied", "deadline", "cancelled",
		"upstream_timeout", "provider_call_failed", "invalid_provider_response",
		"InvalidInput", "ProviderUnavailable", "NotConfigured", "ScopeMismatch", "IdentityChanged",
		"Unauthorized", "Forbidden", "NotFound":
	default:
		return false
	}
	d.UpstreamID = safeFleetTraceID(d.UpstreamID)
	return diagnosticStatus(d.HTTPStatus) && d.ElapsedMS >= 0 && d.ElapsedMS <= 25000
}

func fleetExecutionFailure(ctx context.Context, raw []byte, started time.Time) fleetObservationDiagnostic {
	if d, ok := parseFleetObservationDiagnostic(raw); ok {
		return d
	}
	d := fleetObservationDiagnostic{Stage: "observation_executor", Category: "runner_failed", ElapsedMS: time.Since(started).Milliseconds()}
	switch {
	case errors.Is(ctx.Err(), context.DeadlineExceeded):
		d.Category = "deadline"
	case errors.Is(ctx.Err(), context.Canceled):
		d.Category = "cancelled"
	}
	return d
}

func logFleetObservationFailure(ctx context.Context, b execution.AttemptBinding, d fleetObservationDiagnostic) {
	fields := []any{"component", "gateway",
		"request_id", safeFleetTraceID(b.RequestID), "attempt_id", safeFleetTraceID(b.AttemptID), "observation_id", d.ObservationID,
		"stage", d.Stage, "category", d.Category, "elapsed_ms", d.ElapsedMS,
		"upstream_request_id", d.UpstreamID, "upstream_status", d.HTTPStatus}
	if infra := d.InfraFailure; infra != nil {
		fields = append(fields, "infra_stage", infra.Stage, "infra_category", infra.Category, "infra_elapsed_ms", infra.ElapsedMS,
			"provider_request_id", infra.UpstreamID, "provider_status", infra.HTTPStatus)
	}
	slog.WarnContext(ctx, "pgcat_fleet_read_failed", fields...)
}
