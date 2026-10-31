package adapters

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/domain"
)

type Deployment struct {
	WorkloadKey  string `json:"workloadKey,omitempty"`
	ResourceID   string `json:"resourceId"`
	Kind         string `json:"kind"`
	EnvCode      string `json:"envCode"`
	ObjectCode   string `json:"objectCode"`
	ClusterID    string `json:"clusterId,omitempty"`
	Namespace    string `json:"namespace,omitempty"`
	Name         string `json:"name"`
	UID          string `json:"uid,omitempty"`
	State        string `json:"state"`
	Endpoint     string `json:"endpoint,omitempty"`
	TargetDBCode string `json:"target-db-code,omitempty"`
	EvidenceMode string `json:"evidenceMode"`
}
type InfraReader interface {
	ListDeployments(context.Context, domain.Environment, domain.Object) ([]Deployment, error)
}

// ResolutionIdentity matches Infra's canonical immutable service proof tuple.
// Field order is part of the digest contract; mutable runtime observations are excluded.
type ResolutionIdentity struct {
	EnvCode            string              `json:"envCode"`
	AccountID          string              `json:"accountId"`
	Region             string              `json:"region"`
	ClusterID          string              `json:"clusterId"`
	ProxyCode          string              `json:"proxyCode"`
	GroupID            string              `json:"groupId"`
	ServerGroupID      string              `json:"serverGroupId"`
	ListenerID         string              `json:"listenerId"`
	Port               int                 `json:"port"`
	TargetDBCode       string              `json:"targetDbCode"`
	DatabaseResourceID string              `json:"databaseResourceId"`
	Application        ApplicationIdentity `json:"application"`
}
type ApplicationIdentity struct {
	EnvCode   string `json:"envCode"`
	AppCode   string `json:"appCode"`
	ClusterID string `json:"clusterId"`
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
	UID       string `json:"uid"`
}
type ResolutionProof struct {
	Identity         ResolutionIdentity `json:"identity"`
	ResolutionDigest string             `json:"resolutionDigest"`
}

func (i ResolutionIdentity) Digest() string {
	a := i.Application
	if i.EnvCode == "" || i.AccountID == "" || i.Region != "ap-southeast-1" || i.ClusterID == "" || i.ProxyCode == "" || i.GroupID == "" || i.ServerGroupID == "" || i.ListenerID == "" || i.Port < 1 || i.Port > 65535 || i.TargetDBCode == "" || i.DatabaseResourceID == "" || a.EnvCode != i.EnvCode || a.ClusterID != i.ClusterID || a.AppCode == "" || a.Namespace == "" || a.Name == "" || a.UID == "" {
		return ""
	}
	raw, _ := json.Marshal(i)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}
