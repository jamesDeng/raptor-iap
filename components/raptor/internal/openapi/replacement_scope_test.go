package openapi

import (
	"encoding/json"
	"testing"
)

func TestReplacementApprovalSelectorsNormalizeNestedTarget(t *testing.T) {
	raw := json.RawMessage(`{"requestId":"request","actionId":"action","interface":"db-proxy.scale-in","envCode":"dev","target":{"proxyCode":"proxy"},"parameters":{"desiredCapacity":3}}`)
	selectors, err := agentToolSelectors("approval_request", raw)
	if err != nil || selectors["proxyCode"] != "proxy" || selectors["requestId"] != "request" {
		t.Fatal("nested selectors rejected", err)
	}
	for _, bad := range []string{
		`{"requestId":"request","actionId":"action","interface":"ecs.delete","envCode":"dev","target":{"proxyCode":"proxy"},"parameters":{"desiredCapacity":3}}`,
		`{"requestId":"request","actionId":"action","interface":"db-proxy.scale-in","envCode":"dev","target":{"proxyCode":"proxy","extra":true},"parameters":{"desiredCapacity":3}}`,
		`{"requestId":"request","actionId":"action","interface":"db-proxy.scale-in","envCode":"dev","target":{"proxyCode":"proxy"},"parameters":{"desiredCapacity":3,"extra":true}}`,
		`{"requestId":"request","requestId":"other"}`,
	} {
		if _, err := agentToolSelectors("approval_request", json.RawMessage(bad)); err == nil {
			t.Fatal("invalid approval accepted")
		}
	}
}
func TestPauseSelectorsRequireSupportedWaitReason(t *testing.T) {
	if _, err := agentToolSelectors("request_pause", json.RawMessage(`{"requestId":"request","reason":"waiting_approval"}`)); err != nil {
		t.Fatal(err)
	}
	if _, err := agentToolSelectors("request_pause", json.RawMessage(`{"requestId":"request","reason":"completed"}`)); err == nil {
		t.Fatal("unsupported pause accepted")
	}
}
