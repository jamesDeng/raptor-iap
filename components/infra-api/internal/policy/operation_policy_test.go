package policy

import "testing"

func TestRetiredMonitoringWriteHasNoGrantPolicy(t *testing.T) {
	if _, ok := ApprovalPolicyForTool("pgcat_monitoring_sync", RequestGrantProtocol); ok {
		t.Fatal("retired monitoring write still has a grant policy")
	}
}
