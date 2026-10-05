package domain

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"time"
)

func NewID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	b[6] = (b[6] & 15) | 64
	b[8] = (b[8] & 63) | 128
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[:4], b[4:6], b[6:8], b[8:10], b[10:])
}

type Object struct {
	ID          string `json:"id"`
	Kind        string `json:"kind"`
	Code        string `json:"code"`
	Name        string `json:"name"`
	Description string `json:"description"`
}
type Environment struct {
	Code      string         `json:"code"`
	GroupCode string         `json:"groupCode"`
	Stage     string         `json:"stage"`
	Config    map[string]any `json:"config"`
}
type Operation struct {
	Name       string         `json:"name"`
	Parameters map[string]any `json:"parameters"`
}
type ObjectRef struct {
	Kind string `json:"kind"`
	Code string `json:"code"`
}
type SkillsVersion struct {
	Tag       string `json:"tag"`
	CommitSHA string `json:"commitSha"`
}
type RestartTarget struct {
	AppCode   string `json:"appCode"`
	EnvCode   string `json:"envCode"`
	ClusterID string `json:"clusterId"`
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
	UID       string `json:"uid"`
}
type RequestInput struct {
	Type       string          `json:"type"`
	Object     ObjectRef       `json:"object"`
	EnvCode    string          `json:"envCode"`
	Operations []Operation     `json:"operations"`
	Skills     SkillsVersion   `json:"skills"`
	Operation  string          `json:"operation,omitempty"`
	Targets    []RestartTarget `json:"targets,omitempty"`
}
type Request struct {
	ID         string       `json:"id"`
	CreatorID  string       `json:"creatorId"`
	Definition RequestInput `json:"definition"`
	Status     string       `json:"status"`
	SchemaHash string       `json:"schemaHash"`
	CreatedAt  time.Time    `json:"createdAt"`
}
type User struct {
	ID       string `json:"id"`
	Username string `json:"username"`
	Role     string `json:"role"`
}
type Approval struct {
	ID        string          `json:"approvalId"`
	RequestID string          `json:"requestId"`
	ActionID  string          `json:"actionId"`
	Binding   json.RawMessage `json:"binding"`
	State     string          `json:"state"`
	Guidance  json.RawMessage `json:"guidance"`
}
type Event struct {
	Sequence     int64           `json:"sequence"`
	RequestID    string          `json:"requestId"`
	Kind         string          `json:"kind"`
	Summary      string          `json:"summary"`
	Details      json.RawMessage `json:"details"`
	EvidenceMode string          `json:"evidenceMode"`
	OccurredAt   time.Time       `json:"occurredAt"`
}
