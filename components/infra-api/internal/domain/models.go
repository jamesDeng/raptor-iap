package domain

import "time"

type Environment struct {
	Code      string         `json:"code"`
	AccountID string         `json:"accountId"`
	Region    string         `json:"region"`
	ClusterID string         `json:"clusterId,omitempty"`
	Proxies   []ProxyMapping `json:"proxies,omitempty"`
}
type ProxyMapping struct {
	Code          string `json:"code"`
	GroupID       string `json:"groupId"`
	ServerGroupID string `json:"serverGroupId"`
	ListenerID    string `json:"listenerId"`
	Port          int    `json:"port"`
	TargetDBCode  string `json:"targetDbCode"`
}
type Target struct{ EnvCode, AppCode, ClusterID, Namespace, Name, UID string }
type Deployment struct {
	ObservedAt   time.Time `json:"observedAt"`
	ResourceID   string    `json:"resourceId"`
	Kind         string    `json:"kind"`
	EnvCode      string    `json:"envCode"`
	ObjectCode   string    `json:"objectCode"`
	ClusterID    string    `json:"clusterId,omitempty"`
	Namespace    string    `json:"namespace,omitempty"`
	Name         string    `json:"name"`
	UID          string    `json:"uid,omitempty"`
	State        string    `json:"state"`
	Endpoint     string    `json:"endpoint,omitempty"`
	TargetDBCode string    `json:"target-db-code,omitempty"`
	EvidenceMode string    `json:"evidenceMode"`
}
type Pod struct {
	Name  string `json:"name"`
	UID   string `json:"uid"`
	State string `json:"state"`
	Ready bool   `json:"ready"`
}
type Status struct {
	ObservedAt         time.Time `json:"observedAt"`
	EnvCode            string    `json:"envCode"`
	ObjectCode         string    `json:"objectCode"`
	ClusterID          string    `json:"clusterId"`
	Namespace          string    `json:"namespace"`
	Name               string    `json:"name"`
	UID                string    `json:"uid"`
	Generation         int64     `json:"generation"`
	ObservedGeneration int64     `json:"observedGeneration"`
	Replicas           int       `json:"replicas"`
	DesiredReplicas    int       `json:"desiredReplicas"`
	AvailableReplicas  int       `json:"availableReplicas"`
	UpdatedReplicas    int       `json:"updatedReplicas"`
	ReadyReplicas      int       `json:"readyReplicas"`
	EvidenceMode       string    `json:"evidenceMode"`
	Pods               []Pod     `json:"pods"`
}

// ResourceContext is the catalog-only, credential-free service projection.
type ResourceRef struct {
	Kind string `json:"kind"`
	Code string `json:"code"`
}
type ResourceContext struct {
	EnvCode          string        `json:"envCode"`
	Kind             string        `json:"kind"`
	Code             string        `json:"code"`
	Cloud            string        `json:"cloud"`
	Region           string        `json:"region"`
	AccountID        string        `json:"accountId"`
	ClusterID        string        `json:"clusterId"`
	RelatedResources []ResourceRef `json:"relatedResources"`
}

// Workload keys are opaque references scoped to the logical resource.
type ApplicationWorkload struct {
	Type        string `json:"type"`
	WorkloadKey string `json:"workloadKey"`
	Target      Target `json:"-"`
	ClusterID   string `json:"clusterId"`
	Namespace   string `json:"namespace"`
	Name        string `json:"name"`
	UID         string `json:"uid"`
	State       string `json:"state"`
}
type DatabaseWorkload struct {
	Type       string `json:"type"`
	Code       string `json:"code"`
	ResourceID string `json:"resourceId"`
	Name       string `json:"name"`
	State      string `json:"state"`
	Endpoint   string `json:"endpoint,omitempty"`
}
type PgproxyWorkload struct {
	Type         string `json:"type"`
	Code         string `json:"code"`
	WorkloadKey  string `json:"workloadKey"`
	State        string `json:"state"`
	TargetDBCode string `json:"targetDbCode"`
}

// ResolvedResource is request-local. Provider pins are never public selectors.
// ResolutionDigest is empty for ordinary reads; resources.Proof supplies the complete digest.
type ResolvedResource struct {
	Context              ResourceContext       `json:"-"`
	Environment          Environment           `json:"-"`
	Proxy                *ProxyMapping         `json:"-"`
	ApplicationWorkloads []ApplicationWorkload `json:"applicationWorkloads,omitempty"`
	DatabaseWorkloads    []DatabaseWorkload    `json:"databaseWorkloads,omitempty"`
	PgproxyWorkload      *PgproxyWorkload      `json:"pgproxyWorkload,omitempty"`
	ResolutionDigest     string                `json:"-"`
}
