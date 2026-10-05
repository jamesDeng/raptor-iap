package execution

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"time"
)

func NewID() string {
	var b [16]byte
	if _, e := rand.Read(b[:]); e != nil {
		panic(e)
	}
	b[6] = (b[6] & 15) | 64
	b[8] = (b[8] & 63) | 128
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[:4], b[4:6], b[6:8], b[8:10], b[10:])
}

type SkillsVersion struct {
	Tag       string `json:"tag"`
	CommitSHA string `json:"commitSha"`
}
type ExecutionInput struct {
	RequestID   string          `json:"requestId"`
	Definition  json.RawMessage `json:"definition"`
	Environment json.RawMessage `json:"environment"`
	Skills      SkillsVersion   `json:"skills"`
}
type Execution struct {
	RequestID      string         `json:"requestId"`
	Status         string         `json:"status"`
	AttemptID      string         `json:"attemptId"`
	Input          ExecutionInput `json:"input"`
	AppliedSkills  SkillsVersion  `json:"appliedSkills"`
	Checkpoint     CheckpointRef  `json:"checkpoint"`
	Cleanup        string         `json:"cleanup"`
	RecoveryNeeded bool           `json:"recoveryNeeded"`
	UpdatedAt      time.Time      `json:"updatedAt"`
}
type ProgressEvent struct {
	EventID      string          `json:"eventId"`
	RequestID    string          `json:"requestId"`
	AttemptID    string          `json:"attemptId"`
	Sequence     int64           `json:"sequence"`
	OccurredAt   time.Time       `json:"occurredAt"`
	Kind         string          `json:"kind"`
	Summary      string          `json:"summary"`
	Details      json.RawMessage `json:"details"`
	EvidenceMode string          `json:"evidenceMode"`
}
type Signal struct {
	ID        string          `json:"signalId"`
	RequestID string          `json:"requestId"`
	Kind      string          `json:"kind"`
	Payload   json.RawMessage `json:"payload"`
}
type CheckpointRef struct {
	Path      string `json:"path"`
	RequestID string `json:"requestId"`
}
type CleanupOutcome struct {
	Confirmed bool
	Reason    string
}
