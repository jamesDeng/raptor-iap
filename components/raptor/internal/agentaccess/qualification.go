package agentaccess

import "github.com/jamesDeng/raptor-iap/components/raptor/internal/domain"

func validReplacementParameters(p map[string]any) bool {
	if p == nil {
		return false
	}
	if len(p) == 0 {
		return true
	}
	if len(p) != 1 {
		return false
	}
	m, ok := p["mode"].(string)
	return ok && (m == "qualification" || m == "replacement")
}
func replacementMode(d domain.RequestInput) string {
	if len(d.Operations) != 1 || d.Operations[0].Name != "db-proxy.replace-nodes" {
		return ""
	}
	m, _ := d.Operations[0].Parameters["mode"].(string)
	return m
}
