package execution

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"testing"
)

func TestLiveDecisionUsesFreshApprovalRatherThanWakeup(t *testing.T) {
	binding := json.RawMessage(`{"requestId":"request","actionId":"scale-three","interface":"ess.scale-in","envCode":"rdev.ali","target":{"groupId":"group","proxyCode":"proxy"},"parameters":{"desiredCapacity":3}}`)
	var normalized any
	json.Unmarshal(binding, &normalized)
	raw, _ := json.Marshal(normalized)
	digest := sha256.Sum256(raw)
	wait := LiveWait{Kind: "approval", ApprovalID: "approval", ActionID: "scale-three", BindingDigest: hex.EncodeToString(digest[:])}
	receipt := LiveApprovalDecision{RequestID: "request", ApprovalID: "approval", ActionID: "scale-three", Binding: binding, State: "pending"}
	decision, e := DecisionForLiveWait("request", "", wait, receipt)
	if e != nil || decision.Next != "waiting" {
		t.Fatal("early event authorized execution", e)
	}
	receipt.State = "approved"
	decision, e = DecisionForLiveWait("request", "", wait, receipt)
	if e != nil || decision.Next != "resume" {
		t.Fatal("matching decision refused", e)
	}
	for _, state := range []string{"submitted", "submission_unknown", "claimed"} {
		receipt.State = state
		if _, e = DecisionForLiveWait("request", "", wait, receipt); e == nil {
			t.Fatal("submitted action authorized replay")
		}
	}
	receipt.State = "approved"
	receipt.ApprovalID = "other"
	if _, e = DecisionForLiveWait("request", "", wait, receipt); e == nil {
		t.Fatal("unrelated approval accepted")
	}
	receipt.ApprovalID = "approval"
	receipt.Binding = json.RawMessage(`{"requestId":"request","parameters":{"desiredCapacity":2}}`)
	if _, e = DecisionForLiveWait("request", "", wait, receipt); e == nil {
		t.Fatal("altered target accepted")
	}
	receipt.Binding = binding
	receipt.State = "denied"
	for _, next := range []string{"block", "cancel", "continue"} {
		receipt.Guidance = json.RawMessage(`{"decision":"deny","next":"` + next + `","guidance":"inspect and propose another action"}`)
		decision, e = DecisionForLiveWait("request", "", wait, receipt)
		if e != nil || decision.Next != next {
			t.Fatal("denial lost", next, e)
		}
	}
	decision, e = DecisionForLiveWait("request", "cancelled", wait, receipt)
	if e != nil || decision.Next != "cancel" {
		t.Fatal("cancellation ignored")
	}
}
