package agentaccess

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
func (s ReplacementScope) ValidFor(b Binding) bool {
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

// Selectors are normalized identities, not the full raw MCP payload. Typed
// handlers validate parameters; Infra independently checks current membership.
func ReplacementSelectorsAllowed(b Binding, s ReplacementScope, audience, tool string, selectors map[string]string) bool {
	if !s.ValidFor(b) {
		return false
	}
	expected := map[string]string{}
	if tool == "" {
		return len(selectors) == 0 && (audience == "raptor" || audience == "infra")
	}
	if audience == "raptor" {
		expected["requestId"] = b.RequestID
		switch tool {
		case "request_get":
		case "environment_get":
			expected["envCode"] = b.EnvCode
		case "object_get":
			expected["kind"] = b.ObjectKind
			expected["code"] = b.ObjectCode
		case "request_pause":
		case "approval_get":
			if !scopeText(selectors["approvalId"]) {
				return false
			}
			expected["approvalId"] = selectors["approvalId"]
		case "approval_request":
			expected["envCode"] = b.EnvCode
			expected["proxyCode"] = s.ProxyCode
			expected["groupId"] = s.GroupID
		default:
			return false
		}
	} else if audience == "infra" {
		expected["envCode"] = b.EnvCode
		switch tool {
		case "cloud_identity_get":
		case "deployments_list":
			expected["kind"] = b.ObjectKind
			expected["code"] = b.ObjectCode
		case "db_proxy_scale", "db_proxy_node_protection_set", "db_proxy_nodes_deregister":
			expected["requestId"] = b.RequestID
			expected["proxyCode"] = s.ProxyCode
			expected["groupId"] = s.GroupID
			if tool == "db_proxy_nodes_deregister" {
				expected["serverGroupId"] = s.ServerGroupID
			}
		case "deployment_status_get", "deployment_restart":
			a := s.Application
			expected["appCode"] = a.AppCode
			expected["clusterId"] = a.ClusterID
			expected["namespace"] = a.Namespace
			expected["name"] = a.Name
			expected["uid"] = a.UID
			if tool == "deployment_restart" {
				expected["requestId"] = b.RequestID
			}
		default:
			return false
		}
	} else {
		return false
	}
	if len(expected) != len(selectors) {
		return false
	}
	for key, want := range expected {
		if !scopeText(selectors[key]) || selectors[key] != want {
			return false
		}
	}
	return true
}
