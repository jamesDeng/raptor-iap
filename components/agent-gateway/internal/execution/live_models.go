package execution

import (
	"encoding/json"
	"regexp"
	"strings"
	"time"
)

type AttemptBinding struct {
	DefinitionSHA256  string `json:"definitionSha256,omitempty"`
	ClusterID         string `json:"clusterId,omitempty"`
	RequestID         string `json:"requestId"`
	AttemptID         string `json:"attemptId"`
	Operation         string `json:"operation"`
	ObjectKind        string `json:"objectKind"`
	ObjectCode        string `json:"objectCode"`
	EnvCode           string `json:"envCode"`
	SkillsCommit      string `json:"skillsCommit"`
	Model             string `json:"model"`
	ProviderID        string `json:"providerId,omitempty"`
	ConnectionVersion int64  `json:"connectionVersion,omitempty"`
}
type RuntimeIntent struct {
	Kind string `json:"kind"`
	Name string `json:"name"`
}
type RuntimeResource struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
}
type RuntimeEvent struct {
	MessageID       string    `json:"messageId,omitempty"`
	InputSequence   int64     `json:"inputSequence,omitempty"`
	TurnID          string    `json:"turnId,omitempty"`
	SessionID       string    `json:"sessionId,omitempty"`
	SessionFile     string    `json:"sessionFile,omitempty"`
	Summary         string    `json:"summary,omitempty"`
	RuntimeSequence int64     `json:"runtimeSequence"`
	Kind            string    `json:"kind"`
	Tool            string    `json:"tool,omitempty"`
	Outcome         string    `json:"outcome"`
	OccurredAt      time.Time `json:"occurredAt"`
}
type EvidenceIdentity struct {
	RequestID  string `json:"requestId,omitempty"`
	EnvCode    string `json:"envCode"`
	ObjectCode string `json:"objectCode,omitempty"`
	ClusterID  string `json:"clusterId,omitempty"`
	Namespace  string `json:"namespace,omitempty"`
	Name       string `json:"name,omitempty"`
	UID        string `json:"uid,omitempty"`
}
type ToolEvidence struct {
	Server       string           `json:"server"`
	Tool         string           `json:"tool"`
	ObservedAt   time.Time        `json:"observedAt"`
	EvidenceMode string           `json:"evidenceMode"`
	Identity     EvidenceIdentity `json:"identity"`
	State        json.RawMessage  `json:"state"`
}
type Usage struct {
	Input       int64 `json:"input"`
	Output      int64 `json:"output"`
	TotalTokens int64 `json:"totalTokens"`
}
type LiveResult struct {
	Replacement      *ReplacementConvergence `json:"replacement,omitempty"`
	RequestID        string                  `json:"requestId"`
	AttemptID        string                  `json:"attemptId"`
	SelectedModel    string                  `json:"selectedModel"`
	ActualModel      string                  `json:"actualModel"`
	SelectedProvider string                  `json:"selectedProvider,omitempty"`
	ActualProvider   string                  `json:"actualProvider,omitempty"`
	Answer           string                  `json:"answer"`
	Evidence         []ToolEvidence          `json:"evidence"`
	Usage            []Usage                 `json:"usage,omitempty"`
	GeneratedAt      time.Time               `json:"generatedAt"`
}
type VerifiedCheckpoint struct {
	ArchiveKey  string    `json:"archiveKey"`
	ChecksumKey string    `json:"checksumKey"`
	SHA256      string    `json:"sha256"`
	Bytes       int64     `json:"bytes"`
	PiVersion   string    `json:"piVersion"`
	Encryption  string    `json:"encryption"`
	VerifiedAt  time.Time `json:"verifiedAt"`
}
type LiveOutcome struct {
	Status        string             `json:"status"`
	Checkpoint    VerifiedCheckpoint `json:"checkpoint"`
	SandboxAbsent bool               `json:"sandboxAbsent"`
	KeyAbsent     bool               `json:"keyAbsent"`
	AccessRevoked bool               `json:"accessRevoked"`
	FailureCode   string             `json:"failureCode,omitempty"`
}

var shaPattern = regexp.MustCompile(`^[a-f0-9]{64}$`)
var commitPattern = regexp.MustCompile(`^[a-f0-9]{40}$`)
var journalName = regexp.MustCompile(`^[a-zA-Z0-9_.:-]{1,256}$`)
var liveTools = map[string]bool{"mcp__raptor__request_get": true, "mcp__raptor__environment_get": true, "mcp__raptor__object_get": true, "mcp__infra__cloud_identity_get": true, "mcp__infra__deployments_list": true, "mcp__infra__deployment_status_get": true}

func ProgressToolAllowed(operation, tool string) bool {
	if operation == "application.question" {
		return liveTools[tool]
	}
	if operation != "db-proxy.replace-nodes" {
		return false
	}
	if liveTools[tool] {
		return true
	}
	switch tool {
	case "resource_wait", "cloud_read", "deployment_read", "metrics_read", "db_connection_probe", "mcp__raptor__approval_request", "mcp__raptor__approval_get", "mcp__raptor__request_pause", "mcp__infra__db_proxy_scale", "mcp__infra__db_proxy_node_protection_set", "mcp__infra__db_proxy_nodes_deregister", "mcp__infra__deployment_restart":
		return true
	}
	return false
}

func (b AttemptBinding) valid() bool {
	modelValid := (b.ProviderID == "" && b.ConnectionVersion == 0 && b.Model == "gpt-5.6-luna") || (b.ProviderID == "codex" && b.ConnectionVersion > 0 && journalName.MatchString(b.Model))
	return b.RequestID != "" && b.AttemptID != "" && b.Operation == "application.question" && b.ObjectKind == "application" && journalName.MatchString(b.ObjectCode) && journalName.MatchString(b.EnvCode) && commitPattern.MatchString(b.SkillsCommit) && modelValid
}

// Journal persistence supports both implemented profiles; live admission remains
// separately gated by ParseLiveQuestion until replacement rollout is qualified.
func (b AttemptBinding) journalValid() bool {
	if b.valid() {
		return true
	}
	return b.RequestID != "" && b.AttemptID != "" && b.Operation == "db-proxy.replace-nodes" && b.ObjectKind == "db-proxy" && journalName.MatchString(b.ObjectCode) && journalName.MatchString(b.EnvCode) && journalName.MatchString(b.ClusterID) && shaPattern.MatchString(b.DefinitionSHA256) && commitPattern.MatchString(b.SkillsCommit) && b.Model == "gpt-5.6-luna" && ((b.ProviderID == "" && b.ConnectionVersion == 0) || (b.ProviderID == "codex" && b.ConnectionVersion > 0))
}
func validJournalKind(k string) bool {
	return k == "provider" || k == "probe_cleanup" || k == "prepare" || k == "restore" || (strings.HasPrefix(k, "reconcile_key:") && journalName.MatchString(k)) || k == "key" || k == "sandbox" || k == "command" || k == "access" || k == "checkpoint" || k == "terminate" || k == "revoke_key" || k == "revoke_access"
}
func (c VerifiedCheckpoint) valid() bool {
	if c.PiVersion != "0.99.2" || c.Encryption != "AES256" || c.VerifiedAt.IsZero() || !shaPattern.MatchString(c.SHA256) || c.Bytes < 1 || c.Bytes > 16*1024*1024 || !strings.HasSuffix(c.ArchiveKey, ".tgz") || c.ChecksumKey != strings.TrimSuffix(c.ArchiveKey, ".tgz")+".sha256" {
		return false
	}
	for _, key := range []string{c.ArchiveKey, c.ChecksumKey} {
		if strings.HasPrefix(key, "/") || len(key) > 256 {
			return false
		}
		for _, part := range strings.Split(key, "/") {
			if part == "" || part == "." || part == ".." {
				return false
			}
		}
	}
	return true
}
func ValidateLiveResult(r LiveResult, b AttemptBinding, known ...string) error {
	raw, e := json.Marshal(r)
	if e != nil || len(raw) > 65536 || containsSensitive(string(raw), known) || !b.journalValid() || r.RequestID != b.RequestID || r.AttemptID != b.AttemptID || r.SelectedModel != b.Model || r.ActualModel != b.Model || (b.ProviderID == "codex" && (r.SelectedProvider != "codex" || r.ActualProvider != "openai-codex")) || strings.TrimSpace(r.Answer) == "" || len(r.Answer) > 16384 || r.GeneratedAt.IsZero() || len(r.Evidence) > 32 {
		return ErrInvalid
	}
	if b.Operation == "db-proxy.replace-nodes" {
		return validateReplacementResult(r, b)
	}
	if r.Replacement != nil {
		return ErrInvalid
	}
	seen := map[string]bool{}
	discovered := map[EvidenceIdentity]bool{}
	for _, v := range r.Evidence {
		name := "mcp__" + v.Server + "__" + v.Tool
		if !liveTools[name] || v.ObservedAt.IsZero() || v.Identity.EnvCode != b.EnvCode || len(v.State) > 4096 || !json.Valid(v.State) {
			return ErrInvalid
		}
		if v.Server == "raptor" {
			if v.EvidenceMode != "catalog" || v.Identity.RequestID != b.RequestID || v.Identity.ObjectCode != b.ObjectCode {
				return ErrInvalid
			}
		} else {
			if v.EvidenceMode != "live" {
				return ErrInvalid
			}
			if v.Tool == "cloud_identity_get" {
				var state struct {
					AccountMatches bool `json:"accountMatches"`
				}
				if json.Unmarshal(v.State, &state) != nil || !state.AccountMatches {
					return ErrInvalid
				}
			} else {
				id := v.Identity
				if id.ObjectCode != b.ObjectCode || (b.ClusterID != "" && id.ClusterID != b.ClusterID) || id.ClusterID == "" || id.Namespace == "" || id.Name == "" || id.UID == "" {
					return ErrInvalid
				}
				if v.Tool == "deployments_list" {
					discovered[id] = true
				}
			}
		}
		seen[name] = true
	}
	for _, v := range r.Evidence {
		if v.Server == "infra" && v.Tool == "deployment_status_get" && !discovered[v.Identity] {
			return ErrInvalid
		}
	}
	for _, name := range []string{"mcp__raptor__request_get", "mcp__infra__cloud_identity_get", "mcp__infra__deployments_list", "mcp__infra__deployment_status_get"} {
		if !seen[name] {
			return ErrInvalid
		}
	}
	return validateUsage(r.Usage)
}
