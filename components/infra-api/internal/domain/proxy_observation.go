package domain

import (
	"encoding/json"
	"time"
)

type ProxyReadTarget struct {
	RequestID     string `json:"requestId"`
	EnvCode       string `json:"envCode"`
	ProxyCode     string `json:"proxyCode"`
	GroupID       string `json:"groupId"`
	ServerGroupID string `json:"serverGroupId"`
}

// ProxyObservation is internal: the MCP adapter exports only the selected branch.
type ProxyObservation struct {
	Fleet    *FleetObservation
	Backend  *BackendObservation
	Capacity *CapacityObservation
}

type FleetObservation struct {
	GroupID    string      `json:"groupId"`
	ObservedAt time.Time   `json:"observedAt"`
	Instances  []FleetNode `json:"instances"`
}
type FleetNode struct {
	InstanceID           string `json:"instanceId"`
	ProtectedFromScaleIn bool   `json:"protectedFromScaleIn"`
	LifecycleState       string `json:"lifecycleState"`
	HealthStatus         string `json:"healthStatus"`
}
type BackendObservation struct {
	ServerGroupID string        `json:"serverGroupId"`
	ListenerID    string        `json:"listenerId"`
	ObservedAt    time.Time     `json:"observedAt"`
	Nodes         []BackendNode `json:"nodes"`
}
type BackendNode struct {
	InstanceID string `json:"instanceId"`
	PrivateIP  string `json:"privateIp"`
	Status     string `json:"status"`
	Registered bool   `json:"registered"`
	Healthy    bool   `json:"healthy"`
}
type CapacityObservation struct {
	GroupID         string    `json:"groupId"`
	DesiredCapacity int       `json:"desiredCapacity"`
	MinSize         int       `json:"minSize"`
	MaxSize         int       `json:"maxSize"`
	ObservedAt      time.Time `json:"observedAt"`
}
type PgcatSeriesObservation struct {
	Series     string             `json:"series"`
	ObservedAt time.Time          `json:"observedAt"`
	Rows       []PrometheusSample `json:"rows"`
}
type PrometheusSample struct {
	Metric map[string]string `json:"metric"`
	Value  []json.RawMessage `json:"value"`
}
