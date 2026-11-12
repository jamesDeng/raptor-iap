package domain

import (
	"context"
	"log/slog"
	"regexp"
	"sync/atomic"
	"time"
)

type fleetReadTraceKey struct{}

// FleetReadFailure is the sanitized diagnostic available to the observing Gateway.
type FleetReadFailure struct {
	Stage      string `json:"stage"`
	Category   string `json:"category"`
	ElapsedMS  int64  `json:"elapsedMs"`
	UpstreamID string `json:"upstreamId"`
	HTTPStatus int    `json:"httpStatus"`
}

type fleetReadTrace struct {
	requestID     string
	observationID string
	apiGatewayID  string
	firstFailure  atomic.Pointer[FleetReadFailure]
}

var requestIDFormat = regexp.MustCompile(`(?i)^(?:[0-9a-f]{32}|[0-9a-f]{8}(?:-[0-9a-f]{4}){3}-[0-9a-f]{12})$`)

func safeTraceID(id string) string {
	if requestIDFormat.MatchString(id) {
		return id
	}
	return ""
}

func WithFleetReadTrace(ctx context.Context, requestID, observationID, apiGatewayID string) context.Context {
	return context.WithValue(ctx, fleetReadTraceKey{}, &fleetReadTrace{
		requestID: safeTraceID(requestID), observationID: safeTraceID(observationID), apiGatewayID: safeTraceID(apiGatewayID),
	})
}

func FleetReadDiagnostic(ctx context.Context) *FleetReadFailure {
	trace, ok := ctx.Value(fleetReadTraceKey{}).(*fleetReadTrace)
	if !ok || trace.observationID == "" {
		return nil
	}
	return trace.firstFailure.Load()
}

// LogFleetReadFailure accepts only call-site categories, numeric status and IDs.
// The context marker limits provider tracing to the fleet read path.
func LogFleetReadFailure(ctx context.Context, stage, category string, started time.Time, upstreamID string, upstreamStatus int) {
	trace, ok := ctx.Value(fleetReadTraceKey{}).(*fleetReadTrace)
	if !ok {
		return
	}
	failure := FleetReadFailure{stage, category, time.Since(started).Milliseconds(), safeTraceID(upstreamID), upstreamStatus}
	trace.firstFailure.CompareAndSwap(nil, &failure)
	slog.WarnContext(ctx, "pgcat_fleet_read_failed", "component", "infra_api",
		"request_id", trace.requestID, "observation_id", trace.observationID, "api_gateway_request_id", trace.apiGatewayID,
		"stage", stage, "category", category, "elapsed_ms", failure.ElapsedMS,
		"upstream_request_id", failure.UpstreamID, "upstream_status", failure.HTTPStatus)
}
