package openapi

import (
	"encoding/json"
	"testing"
)

func TestRepositoryAttachmentSelectorsRequireExactRequestAndAttempt(t *testing.T) {
	raw := json.RawMessage(`{"requestId":"request","attemptId":"attempt","repository":"owner/repo","number":7,"url":"https://github.com/owner/repo/pull/7","headSha":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","baseCommit":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb","baseBranch":"main","workingBranch":"raptor/change"}`)
	selectors, err := agentToolSelectors("pull_request_attach", raw)
	if err != nil || len(selectors) != 2 || selectors["requestId"] != "request" || selectors["attemptId"] != "attempt" {
		t.Fatal(selectors, err)
	}
	if _, err = agentToolSelectors("pull_request_attach", json.RawMessage(`{"requestId":"request","attemptId":"attempt"}`)); err == nil {
		t.Fatal("incomplete attachment accepted")
	}
}
