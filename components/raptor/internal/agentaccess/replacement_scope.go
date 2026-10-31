package agentaccess

import (
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/adapters"
	"strings"
)

// ReplacementScope is resolved by the service, not supplied by the model.
type ReplacementScope struct {
	AccountID          string `json:"accountId,omitempty"`
	Region             string `json:"region,omitempty"`
	ListenerID         string `json:"listenerId,omitempty"`
	Port               int    `json:"port,omitempty"`
	DatabaseResourceID string `json:"databaseResourceId,omitempty"`
	ResolutionDigest   string `json:"resolutionDigest,omitempty"`

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
	if (b.ExecutionMode != "" && b.ExecutionMode != "replacement" && b.ExecutionMode != "qualification") || b.Operation != "db-proxy.replace-nodes" || b.ObjectKind != "db-proxy" || s.EnvCode != b.EnvCode || s.ProxyCode != b.ObjectCode || s.Application.EnvCode != b.EnvCode || s.Application.ClusterID != b.ClusterID {
		return false
	}
	for _, v := range []string{s.EnvCode, s.ProxyCode, s.GroupID, s.ServerGroupID, s.TargetDBCode, s.Application.AppCode, s.Application.ClusterID, s.Application.Namespace, s.Application.Name, s.Application.UID} {
		if !scopeText(v) {
			return false
		}
	}
	return true
}

// Selectors contain only logical identities. Provider realization is checked
// separately after these selectors have been authorized.
func ReplacementSelectorsAllowed(b Binding, s ReplacementScope, audience, tool string, selectors map[string]string) bool {
	if !s.ResourceValidFor(b) {
		return false
	}
	if tool == "" {
		return len(selectors) == 0 && (audience == "raptor" || audience == "infra")
	}
	expected := map[string]string{}
	refAllowed := func(kind, code string) bool {
		return (kind == b.ObjectKind && code == b.ObjectCode) || (kind == "application" && code == s.Application.AppCode) || (kind == "database" && code == s.TargetDBCode)
	}
	if audience == "raptor" {
		expected["requestId"] = b.RequestID
		switch tool {
		case "request_get", "request_pause":
		case "environment_get":
			expected["envCode"] = b.EnvCode
		case "object_get", "deployments_list":
			if !refAllowed(selectors["kind"], selectors["code"]) {
				return false
			}
			expected["kind"] = selectors["kind"]
			expected["code"] = selectors["code"]
			if tool == "deployments_list" {
				expected["envCode"] = b.EnvCode
			}
		case "approval_get":
			expected["approvalId"] = selectors["approvalId"]
		case "approval_request":
			if b.ExecutionMode == "qualification" {
				expected["interface"] = "runtime.qualification-resume"
			}
			expected["envCode"] = b.EnvCode
			expected["proxyCode"] = s.ProxyCode
		default:
			return false
		}
	} else if audience == "infra" {
		expected["envCode"] = b.EnvCode
		if b.ExecutionMode == "qualification" && (tool == "db_proxy_scale" || tool == "db_proxy_node_protection_set" || tool == "db_proxy_nodes_deregister" || tool == "deployment_restart") {
			return false
		}
		switch tool {
		case "deployments_list":
			if !refAllowed(selectors["kind"], selectors["code"]) {
				return false
			}
			expected["kind"] = selectors["kind"]
			expected["code"] = selectors["code"]
		case "proxy_observation_get", "proxy_pgcat_metrics_get", "db_proxy_scale", "db_proxy_node_protection_set", "db_proxy_nodes_deregister":
			expected["requestId"] = b.RequestID
			expected["proxyCode"] = s.ProxyCode
		case "deployment_status_get", "deployment_restart":
			expected["appCode"] = s.Application.AppCode
			if key, ok := selectors["workloadKey"]; ok {
				expected["workloadKey"] = key
			} else if tool == "deployment_restart" {
				return false
			}
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

func (s ReplacementScope) Identity() adapters.ResolutionIdentity {
	a := s.Application
	return adapters.ResolutionIdentity{EnvCode: s.EnvCode, AccountID: s.AccountID, Region: s.Region, ClusterID: a.ClusterID, ProxyCode: s.ProxyCode, GroupID: s.GroupID, ServerGroupID: s.ServerGroupID, ListenerID: s.ListenerID, Port: s.Port, TargetDBCode: s.TargetDBCode, DatabaseResourceID: s.DatabaseResourceID, Application: adapters.ApplicationIdentity{EnvCode: a.EnvCode, AppCode: a.AppCode, ClusterID: a.ClusterID, Namespace: a.Namespace, Name: a.Name, UID: a.UID}}
}
func (s ReplacementScope) ResourceValidFor(b Binding) bool {
	d := s.Identity().Digest()
	return (b.ExecutionMode == "replacement" || b.ExecutionMode == "qualification") && s.ValidFor(b) && d != "" && d == s.ResolutionDigest
}
