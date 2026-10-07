package domain

// Command identities come from an authenticated request, never a provider URL.
type RestartCommand struct {
	RequestID string `json:"requestId"`
	EnvCode   string `json:"envCode"`
	AppCode   string `json:"appCode"`
	ClusterID string `json:"clusterId"`
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
	UID       string `json:"uid"`
}

func (c RestartCommand) Target() Target {
	return Target{c.EnvCode, c.AppCode, c.ClusterID, c.Namespace, c.Name, c.UID}
}

type ProxyTarget struct {
	RequestID string `json:"requestId"`
	EnvCode   string `json:"envCode"`
	ProxyCode string `json:"proxyCode"`
	GroupID   string `json:"groupId"`
}
type ScaleCommand struct {
	ProxyTarget
	ActionID        string `json:"actionId"`
	DesiredCapacity int    `json:"desiredCapacity"`
}
type ProtectionCommand struct {
	ProxyTarget
	InstanceIDs []string `json:"instanceIds"`
	Protected   bool     `json:"protected"`
}
type DeregisterCommand struct {
	ProxyTarget
	ServerGroupID string   `json:"serverGroupId"`
	InstanceIDs   []string `json:"instanceIds"`
}
type RestartReceipt struct {
	Accepted     bool           `json:"accepted"`
	Target       RestartCommand `json:"target"`
	Generation   int64          `json:"generation"`
	EvidenceMode string         `json:"evidenceMode"`
}
type ScaleReceipt struct {
	Outcome                 string `json:"outcome"`
	GroupID                 string `json:"groupId"`
	PreviousDesiredCapacity int    `json:"previousDesiredCapacity"`
	DesiredCapacity         int    `json:"desiredCapacity"`
	ProviderRequestID       string `json:"providerRequestId,omitempty"`
	EvidenceMode            string `json:"evidenceMode"`
}
type NodeReceipt struct {
	Outcome      string   `json:"outcome"`
	InstanceIDs  []string `json:"instanceIds"`
	EvidenceMode string   `json:"evidenceMode"`
}

// CommandError exposes only reviewed safe codes; provider messages stay private.
type CommandError struct{ Code string }

func (e *CommandError) Error() string { return e.Code }
