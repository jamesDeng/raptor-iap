package execution

import "strings"

// ReplacementScope is resolved by the service, not supplied by the model.
type ReplacementScope struct {
	EnvCode       string           `json:"envCode"`
	ProxyCode     string           `json:"proxyCode"`
	GroupID       string           `json:"groupId"`
	ServerGroupID string           `json:"serverGroupId"`
	TargetDBCode  string           `json:"targetDbCode"`
	Application   ApplicationScope `json:"application"`
}
type ApplicationScope struct {
	EnvCode   string `json:"envCode"`
	AppCode   string `json:"appCode"`
	ClusterID string `json:"clusterId"`
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
	UID       string `json:"uid"`
}

func scopeText(s string) bool { return s != "" && len(s) <= 256 && strings.TrimSpace(s) == s }
func (s ReplacementScope) ValidFor(b AttemptBinding) bool {
	if b.Operation != "db-proxy.replace-nodes" || b.ObjectKind != "db-proxy" || s.EnvCode != b.EnvCode || s.ProxyCode != b.ObjectCode || s.Application.EnvCode != b.EnvCode || s.Application.ClusterID != b.ClusterID {
		return false
	}
	for _, v := range []string{s.EnvCode, s.ProxyCode, s.GroupID, s.ServerGroupID, s.TargetDBCode, s.Application.AppCode, s.Application.ClusterID, s.Application.Namespace, s.Application.Name, s.Application.UID} {
		if !scopeText(v) {
			return false
		}
	}
	return true
}
