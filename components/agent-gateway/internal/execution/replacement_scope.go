package execution

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strings"
)

// ReplacementScope is resolved by the service, not supplied by the model.
type ReplacementScope struct {
	AccountID          string           `json:"accountId,omitempty"`
	Region             string           `json:"region,omitempty"`
	ListenerID         string           `json:"listenerId,omitempty"`
	Port               int              `json:"port,omitempty"`
	DatabaseResourceID string           `json:"databaseResourceId,omitempty"`
	ResolutionDigest   string           `json:"resolutionDigest,omitempty"`
	EnvCode            string           `json:"envCode"`
	ProxyCode          string           `json:"proxyCode"`
	GroupID            string           `json:"groupId"`
	ServerGroupID      string           `json:"serverGroupId"`
	TargetDBCode       string           `json:"targetDbCode"`
	Application        ApplicationScope `json:"application"`
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

// ResourceValidFor is required at new execution boundaries. ValidFor remains
// usable by historical result readers; missing legacy proof is never upgraded.
func (s ReplacementScope) ResourceValidFor(b AttemptBinding) bool {
	if (b.ExecutionMode != "replacement" && b.ExecutionMode != "qualification") || !s.ValidFor(b) || s.Region != "ap-southeast-1" || s.Port < 1 || s.Port > 65535 || !shaPattern.MatchString(s.ResolutionDigest) {
		return false
	}
	for _, v := range []string{s.AccountID, s.ListenerID, s.DatabaseResourceID} {
		if !scopeText(v) {
			return false
		}
	}
	// Declaration order matches Infra resources.ResolutionIdentity exactly.
	identity := struct {
		EnvCode            string           `json:"envCode"`
		AccountID          string           `json:"accountId"`
		Region             string           `json:"region"`
		ClusterID          string           `json:"clusterId"`
		ProxyCode          string           `json:"proxyCode"`
		GroupID            string           `json:"groupId"`
		ServerGroupID      string           `json:"serverGroupId"`
		ListenerID         string           `json:"listenerId"`
		Port               int              `json:"port"`
		TargetDBCode       string           `json:"targetDbCode"`
		DatabaseResourceID string           `json:"databaseResourceId"`
		Application        ApplicationScope `json:"application"`
	}{s.EnvCode, s.AccountID, s.Region, s.Application.ClusterID, s.ProxyCode, s.GroupID, s.ServerGroupID, s.ListenerID, s.Port, s.TargetDBCode, s.DatabaseResourceID, s.Application}
	raw, err := json.Marshal(identity)
	return err == nil && fmt.Sprintf("%x", sha256.Sum256(raw)) == s.ResolutionDigest
}

// A configured legacy operator fence may only narrow an issued resource proof.
// It supplies no missing proof fields and cannot authorize a legacy request.
func (s ReplacementScope) MatchesFence(fence ReplacementScope) bool {
	if fence.AccountID != "" || fence.Region != "" || fence.ListenerID != "" || fence.Port != 0 || fence.DatabaseResourceID != "" || fence.ResolutionDigest != "" {
		return s == fence
	}
	s.AccountID = ""
	s.Region = ""
	s.ListenerID = ""
	s.Port = 0
	s.DatabaseResourceID = ""
	s.ResolutionDigest = ""
	return s == fence
}
