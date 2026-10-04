package adapters

import (
	"context"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/domain"
)

type Deployment struct {
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
