package provider

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"time"

	"github.com/alibabacloud-go/tea/tea"
	"raptor-iap/infra-api/internal/domain"
)

func logFleetESSFailure(ctx context.Context, stage string, started time.Time, sdkErr error, responseID string) {
	category, status := "invalid_provider_response", 0
	var upstream interface {
		error
		GetCode() *string
		GetRequestId() *string
		GetStatusCode() *int
	}
	var legacy *tea.SDKError
	var network net.Error
	switch {
	case errors.As(sdkErr, &upstream):
		status = tea.IntValue(upstream.GetStatusCode())
		category = fleetESSErrorCategory(tea.StringValue(upstream.GetCode()), status)
		responseID = tea.StringValue(upstream.GetRequestId())
	case errors.As(sdkErr, &legacy):
		// The ESS SDK converts its typed error to tea.SDKError. It preserves the
		// provider response in Data. Extract only the request ID from that JSON.
		status = tea.IntValue(legacy.StatusCode)
		category = fleetESSErrorCategory(tea.StringValue(legacy.Code), status)
		var response struct{ RequestID string }
		if data := tea.StringValue(legacy.Data); len(data) <= 65536 && json.Unmarshal([]byte(data), &response) == nil {
			responseID = response.RequestID
		}
	case errors.Is(ctx.Err(), context.DeadlineExceeded), errors.Is(sdkErr, context.DeadlineExceeded):
		category = "deadline"
	case errors.Is(ctx.Err(), context.Canceled), errors.Is(sdkErr, context.Canceled):
		category = "cancelled"
	case errors.As(sdkErr, &network) && network.Timeout():
		category = "upstream_timeout"
	case sdkErr != nil:
		category = "provider_call_failed"
	}
	domain.LogFleetReadFailure(ctx, stage, category, started, responseID, status)
}

func fleetESSErrorCategory(code string, status int) string {
	if status == 429 {
		return "throttled"
	}
	switch code {
	case "Throttling", "Throttling.User", "Throttling.Api", "Throttling.System", "TooManyRequests", "RequestLimitExceeded":
		return "throttled"
	case "Forbidden", "NoPermission", "AccessDenied", "Unauthorized":
		return "provider_access_denied"
	default:
		return "provider_api_error"
	}
}
