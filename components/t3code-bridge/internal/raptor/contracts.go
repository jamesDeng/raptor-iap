package raptor

import (
	"encoding/json"
	"time"
)

type ObjectRef struct {
	Kind string `json:"kind"`
	Code string `json:"code"`
}
type Operation struct {
	Name       string            `json:"name"`
	Parameters map[string]string `json:"parameters"`
}
type Definition struct {
	Type       string      `json:"type"`
	Model      string      `json:"model"`
	Object     ObjectRef   `json:"object"`
	EnvCode    string      `json:"envCode"`
	Operations []Operation `json:"operations"`
}
type Request struct {
	ID         string     `json:"id"`
	CreatorID  string     `json:"creatorId"`
	Definition Definition `json:"definition"`
	Status     string     `json:"status"`
}
type Cleanup struct {
	SandboxAbsent bool `json:"sandboxAbsent"`
	KeyAbsent     bool `json:"keyAbsent"`
	AccessRevoked bool `json:"accessRevoked"`
}

func (c *Cleanup) Confirmed() bool {
	return c != nil && c.SandboxAbsent && c.KeyAbsent && c.AccessRevoked
}

type Identity struct {
	RequestID  string `json:"requestId"`
	EnvCode    string `json:"envCode"`
	ObjectCode string `json:"objectCode"`
	AppCode    string `json:"appCode"`
}
type Evidence struct {
	Server       string          `json:"server"`
	Tool         string          `json:"tool"`
	ObservedAt   time.Time       `json:"observedAt"`
	EvidenceMode string          `json:"evidenceMode"`
	Identity     Identity        `json:"identity"`
	State        json.RawMessage `json:"state"`
}
type Usage struct {
	Input  int64 `json:"input"`
	Output int64 `json:"output"`
	Total  int64 `json:"totalTokens"`
}
type Result struct {
	RequestID     string     `json:"requestId"`
	AttemptID     string     `json:"attemptId"`
	SelectedModel string     `json:"selectedModel"`
	ActualModel   string     `json:"actualModel"`
	Answer        string     `json:"answer"`
	Evidence      []Evidence `json:"evidence"`
	GeneratedAt   time.Time  `json:"generatedAt"`
	Usage         []Usage    `json:"usage"`
}
type Execution struct {
	RequestID        string    `json:"requestId"`
	AttemptID        string    `json:"attemptId"`
	Status           string    `json:"status"`
	RuntimeMode      string    `json:"runtimeMode"`
	Stage            string    `json:"stage"`
	RecoveryNeeded   bool      `json:"recoveryNeeded"`
	UpdatedAt        time.Time `json:"updatedAt"`
	Result           *Result   `json:"result"`
	CheckpointStatus string    `json:"checkpointStatus"`
	CleanupStatus    *Cleanup  `json:"cleanupStatus"`
	FailureCode      string    `json:"failureCode"`
}
type RequestView struct {
	Request             Request    `json:"request"`
	Execution           *Execution `json:"execution"`
	ExecutionAvailable  bool       `json:"executionAvailable"`
	CancellationPending bool       `json:"cancellationPending"`
}
type Event struct {
	AttemptID string `json:"attemptId"`
	Sequence  int64  `json:"sequence"`
	RequestID string `json:"requestId"`
	Kind      string `json:"kind"`
	Summary   string `json:"summary"`
}
type Timeline struct {
	Events          []Event `json:"events"`
	SyncUnavailable bool    `json:"syncUnavailable"`
}
