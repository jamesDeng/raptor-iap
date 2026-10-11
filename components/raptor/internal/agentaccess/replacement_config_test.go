package agentaccess

import (
	"strings"
	"testing"
)

func TestReplacementConfigIsExplicitAndStrict(t *testing.T) {
	raw := `{"envCode":"rdev.ali","proxyCode":"proxy","groupId":"group","serverGroupId":"backend","targetDbCode":"db","application":{"envCode":"rdev.ali","appCode":"client","clusterId":"cluster","namespace":"test","name":"traffic","uid":"uid"}}`
	if _, err := DecodeReplacementScope(strings.NewReader(raw)); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{`{}`, raw + raw, raw + strings.Repeat(" ", 17000), strings.Replace(raw, `"envCode":"rdev.ali"`, `"unknown":"x","envCode":"rdev.ali"`, 1), strings.Replace(raw, `"uid":"uid"`, `"uid":""`, 1)} {
		if _, err := DecodeReplacementScope(strings.NewReader(bad)); err == nil {
			t.Fatal("accepted incomplete or ambiguous scope")
		}
	}
}
