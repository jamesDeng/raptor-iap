package openapi

import (
	"encoding/json"
	"testing"
)

func TestQualificationApprovalHasDistinctInterfaceAndStrictParameters(t *testing.T) {
	good := `{"requestId":"r","actionId":"a","interface":"runtime.qualification-resume","envCode":"dev","target":{"proxyCode":"p"},"parameters":{"phase":"resume"}}`
	s, e := agentToolSelectors("approval_request", json.RawMessage(good))
	if e != nil || s["interface"] != "runtime.qualification-resume" {
		t.Fatal("qualification approval rejected", e)
	}
	for _, raw := range []string{`{"requestId":"r","actionId":"a","interface":"runtime.qualification-resume","envCode":"dev","target":{"proxyCode":"p"},"parameters":{"desiredCapacity":1}}`, `{"requestId":"r","actionId":"a","interface":"runtime.qualification-resume","envCode":"dev","target":{"proxyCode":"p"},"parameters":{"phase":"other"}}`} {
		if _, e = agentToolSelectors("approval_request", json.RawMessage(raw)); e == nil {
			t.Fatal("invalid qualification approved")
		}
	}
}
