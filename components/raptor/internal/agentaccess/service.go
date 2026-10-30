// Package agentaccess owns request-bound opaque agent credentials in the Raptor schema.
package agentaccess

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/domain"
	"net/url"
	"regexp"
	"time"
)

type Service struct {
	Pool                      *pgxpool.Pool
	RaptorMCPURL, InfraMCPURL string
	Now                       func() time.Time
	// Nil keeps replacement admission disabled. The resolver must obtain live
	// proxy/dependency identity from trusted configuration and provider reads.
	ResolveReplacement func(context.Context, Binding) (ReplacementScope, error)
}
type IssueInput struct {
	AttemptID        string    `json:"attemptId"`
	DefinitionSHA256 string    `json:"definitionSha256"`
	ExpiresAt        time.Time `json:"expiresAt"`
}
type Binding struct {
	ExecutionMode     string `json:"executionMode,omitempty"`
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
	DefinitionSHA256  string `json:"definitionSha256"`
	ClusterID         string `json:"clusterId"`
}
type Issued struct {
	Credential       string            `json:"credential"`
	ExpiresAt        time.Time         `json:"expiresAt"`
	Binding          Binding           `json:"binding"`
	RaptorMCPURL     string            `json:"raptorMcpUrl"`
	InfraMCPURL      string            `json:"infraMcpUrl"`
	ReplacementScope *ReplacementScope `json:"replacementScope,omitempty"`
}
type CheckInput struct {
	Credential string            `json:"credential"`
	Audience   string            `json:"audience"`
	Tool       string            `json:"tool"`
	Selectors  map[string]string `json:"selectors"`
}
type CheckResult struct {
	Active           bool              `json:"active"`
	Binding          *Binding          `json:"binding,omitempty"`
	ExpiresAt        *time.Time        `json:"expiresAt,omitempty"`
	ReplacementScope *ReplacementScope `json:"replacementScope,omitempty"`
}

var uuid = regexp.MustCompile(`^[a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12}$`)
var sha = regexp.MustCompile(`^[a-f0-9]{40}$`)
var tokenPattern = regexp.MustCompile(`^raptor_at_[A-Za-z0-9_-]{43}$`)

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now().UTC()
	}
	return time.Now().UTC()
}
func Fingerprint(def domain.RequestInput) (string, error) {
	b, e := json.Marshal(def)
	if e != nil {
		return "", e
	}
	var canonical any
	if e = json.Unmarshal(b, &canonical); e != nil {
		return "", e
	}
	b, e = json.Marshal(canonical)
	if e != nil {
		return "", e
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), nil
}
func digest(token string) string { h := sha256.Sum256([]byte(token)); return hex.EncodeToString(h[:]) }
func validURL(raw string) bool {
	u, e := url.Parse(raw)
	return e == nil && u.Scheme == "https" && u.Host != "" && u.User == nil && u.RawQuery == "" && u.Fragment == ""
}
func question(def domain.RequestInput) bool {
	modelValid := (def.ProviderID == "" && def.ConnectionVersion == 0 && def.Model == "gpt-5.6-luna") || (def.ProviderID == "codex" && def.ConnectionVersion > 0 && def.Model != "")
	return def.Type == "agent" && def.Object.Kind == "application" && len(def.Operations) == 1 && def.Operations[0].Name == "application.question" && modelValid && sha.MatchString(def.Skills.CommitSHA)
}
func repositoryChange(def domain.RequestInput) bool {
	modelValid := (def.ProviderID == "" && def.ConnectionVersion == 0 && def.Model == "gpt-5.6-luna") || (def.ProviderID == "codex" && def.ConnectionVersion > 0 && def.Model != "")
	return def.Type == "agent" && def.Object.Kind == "application" && len(def.Operations) == 1 && def.Operations[0].Name == "repository.change" && modelValid && sha.MatchString(def.Skills.CommitSHA)
}
func replacementModelValid(def domain.RequestInput) bool {
	if def.Model == "gpt-5.6-luna" && def.ProviderID == "" && def.ConnectionVersion == 0 {
		return true // Historical request definitions remain readable.
	}
	if def.ProviderID != "codex" || def.ConnectionVersion <= 0 {
		return false
	}
	if replacementMode(def) == "qualification" {
		return def.Model != ""
	}
	return def.Model == "gpt-5.6-luna" || (replacementMode(def) == "replacement" && (def.Model == "gpt-6-luna" || def.Model == "gpt-6.1-sol"))
}
func supported(def domain.RequestInput) bool {
	if question(def) || repositoryChange(def) {
		return true
	}
	return def.Type == "agent" && def.Object.Kind == "db-proxy" && len(def.Operations) == 1 && def.Operations[0].Name == "db-proxy.replace-nodes" && validReplacementParameters(def.Operations[0].Parameters) && replacementModelValid(def) && sha.MatchString(def.Skills.CommitSHA)
}
func (s *Service) Issue(ctx context.Context, id string, in IssueInput) (Issued, error) {
	var out Issued
	now := s.now()
	in.ExpiresAt = in.ExpiresAt.UTC().Truncate(time.Microsecond)
	if !uuid.MatchString(id) || !uuid.MatchString(in.AttemptID) || !in.ExpiresAt.After(now) || in.ExpiresAt.After(now.Add(600*time.Second)) {
		return out, domain.ErrInvalid
	}
	if !validURL(s.RaptorMCPURL) || !validURL(s.InfraMCPURL) {
		return out, domain.ErrUnavailable
	}
	tx, e := s.Pool.Begin(ctx)
	if e != nil {
		return out, domain.ErrUnavailable
	}
	defer tx.Rollback(ctx)
	var body []byte
	var status, control string
	e = tx.QueryRow(ctx, "SELECT definition,status,control_state FROM raptor.requests WHERE id=$1 FOR UPDATE", id).Scan(&body, &status, &control)
	if e == pgx.ErrNoRows {
		return out, domain.ErrNotFound
	}
	if e != nil {
		return out, domain.ErrUnavailable
	}
	var def domain.RequestInput
	if json.Unmarshal(body, &def) != nil {
		return out, domain.ErrUnavailable
	}
	fingerprint, e := Fingerprint(def)
	if e != nil {
		return out, domain.ErrUnavailable
	}
	if !supported(def) || fingerprint != in.DefinitionSHA256 {
		return out, domain.ErrInvalid
	}
	if control != "" || (status != "queued" && status != "running") {
		return out, domain.ErrConflict
	}
	var cluster string
	if e = tx.QueryRow(ctx, "SELECT CASE WHEN ack_cluster_id<>'' AND cluster_id<>'' AND ack_cluster_id<>cluster_id THEN '' ELSE coalesce(nullif(ack_cluster_id,''),nullif(cluster_id,''),(SELECT value_json::jsonb #>> '{}' FROM raptor.environment_settings WHERE environment_code=$1 AND key='ackClusterId'),'') END FROM raptor.environments WHERE code=$1", def.EnvCode).Scan(&cluster); e != nil || cluster == "" {
		return out, domain.ErrUnavailable
	}
	out.Binding = Binding{ExecutionMode: replacementMode(def), RequestID: id, AttemptID: in.AttemptID, Operation: def.Operations[0].Name, ObjectKind: def.Object.Kind, ObjectCode: def.Object.Code, EnvCode: def.EnvCode, SkillsCommit: def.Skills.CommitSHA, Model: def.Model, ProviderID: def.ProviderID, ConnectionVersion: def.ConnectionVersion, DefinitionSHA256: fingerprint, ClusterID: cluster}
	var scopeRaw []byte
	if !question(def) && !repositoryChange(def) {
		if s.ResolveReplacement == nil {
			return out, domain.ErrUnavailable
		}
		scope, err := s.ResolveReplacement(ctx, out.Binding)
		if err != nil {
			return out, domain.ErrUnavailable
		}
		if !scope.ResourceValidFor(out.Binding) {
			return out, domain.ErrConflict
		}
		// The request row lock serializes issuance across every attempt ID.
		rows, err := tx.Query(ctx, "SELECT replacement_scope FROM raptor.agent_attempt_access WHERE request_id=$1 AND (replacement_scope IS NOT NULL OR binding->>'operation'='db-proxy.replace-nodes')", id)
		if err != nil {
			return out, domain.ErrUnavailable
		}
		mismatch := false
		for rows.Next() {
			var raw []byte
			var saved ReplacementScope
			if rows.Scan(&raw) != nil || json.Unmarshal(raw, &saved) != nil || !saved.ResourceValidFor(out.Binding) || saved != scope {
				mismatch = true
			}
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return out, domain.ErrUnavailable
		}
		if mismatch {
			return out, domain.ErrConflict
		}
		out.ReplacementScope = &scope
		scopeRaw, _ = json.Marshal(scope)
	}
	binding, _ := json.Marshal(out.Binding)
	var existing, existingScope []byte
	var deadline time.Time
	var revoked bool
	e = tx.QueryRow(ctx, "SELECT binding,expires_at,revoked,replacement_scope FROM raptor.agent_attempt_access WHERE request_id=$1 AND attempt_id=$2 FOR UPDATE", id, in.AttemptID).Scan(&existing, &deadline, &revoked, &existingScope)
	if e == nil {
		var old Binding
		if json.Unmarshal(existing, &old) != nil {
			return out, domain.ErrUnavailable
		}
		if revoked || !deadline.After(now) || in.ExpiresAt.After(deadline) || old != out.Binding {
			return out, domain.ErrConflict
		}
		if out.ReplacementScope != nil {
			var oldScope ReplacementScope
			if json.Unmarshal(existingScope, &oldScope) != nil || oldScope != *out.ReplacementScope {
				return out, domain.ErrConflict
			}
		} else if len(existingScope) != 0 {
			return out, domain.ErrConflict
		}
	} else if e == pgx.ErrNoRows {
		_, e = tx.Exec(ctx, "INSERT INTO raptor.agent_attempt_access(request_id,attempt_id,binding,expires_at,replacement_scope) VALUES($1,$2,$3,$4,$5)", id, in.AttemptID, binding, in.ExpiresAt, scopeRaw)
		if e != nil {
			return out, domain.ErrConflict
		}
	} else {
		return out, domain.ErrUnavailable
	}
	if _, e = tx.Exec(ctx, "UPDATE raptor.agent_tokens SET revoked=true WHERE request_id=$1 AND attempt_id=$2", id, in.AttemptID); e != nil {
		return out, domain.ErrUnavailable
	}
	var random [32]byte
	if _, e = rand.Read(random[:]); e != nil {
		return out, domain.ErrUnavailable
	}
	out.Credential = "raptor_at_" + base64.RawURLEncoding.EncodeToString(random[:])
	out.ExpiresAt = in.ExpiresAt
	out.RaptorMCPURL = s.RaptorMCPURL
	out.InfraMCPURL = s.InfraMCPURL
	if _, e = tx.Exec(ctx, "INSERT INTO raptor.agent_tokens(token_hash,request_id,attempt_id,expires_at) VALUES($1,$2,$3,$4)", digest(out.Credential), id, in.AttemptID, in.ExpiresAt); e != nil {
		return Issued{}, domain.ErrUnavailable
	}
	if e = tx.Commit(ctx); e != nil {
		return Issued{}, domain.ErrUnavailable
	}
	return out, nil
}
func (s *Service) Revoke(ctx context.Context, id, attempt string) error {
	if !uuid.MatchString(id) || !uuid.MatchString(attempt) {
		return domain.ErrInvalid
	}
	// Tombstone even an unknown attempt, so a delayed issuance cannot resurrect it.
	tx, e := s.Pool.Begin(ctx)
	if e != nil {
		return domain.ErrUnavailable
	}
	defer tx.Rollback(ctx)
	var requestID string
	if e = tx.QueryRow(ctx, "SELECT id::text FROM raptor.requests WHERE id=$1 FOR UPDATE", id).Scan(&requestID); e == pgx.ErrNoRows {
		return domain.ErrNotFound
	} else if e != nil {
		return domain.ErrUnavailable
	}
	if _, e = tx.Exec(ctx, "INSERT INTO raptor.agent_attempt_access(request_id,attempt_id,binding,expires_at,revoked) VALUES($1,$2,'{}',now(),true) ON CONFLICT(request_id,attempt_id) DO UPDATE SET revoked=true", id, attempt); e != nil {
		return domain.ErrUnavailable
	}
	if _, e = tx.Exec(ctx, "UPDATE raptor.agent_tokens SET revoked=true WHERE request_id=$1 AND attempt_id=$2", id, attempt); e != nil {
		return domain.ErrUnavailable
	}
	if tx.Commit(ctx) != nil {
		return domain.ErrUnavailable
	}
	return nil
}
func (s *Service) Check(ctx context.Context, in CheckInput) (CheckResult, error) {
	out := CheckResult{}
	if !tokenPattern.MatchString(in.Credential) || (in.Audience != "raptor" && in.Audience != "infra") {
		return out, nil
	}
	var body, definition, scopeRaw []byte
	var expiry, deadline time.Time
	var tokenRevoked, attemptRevoked bool
	var status, control string
	e := s.Pool.QueryRow(ctx, `SELECT a.binding,t.expires_at,a.expires_at,t.revoked,a.revoked,r.definition,r.status,r.control_state,a.replacement_scope FROM raptor.agent_tokens t JOIN raptor.agent_attempt_access a ON a.request_id=t.request_id AND a.attempt_id=t.attempt_id JOIN raptor.requests r ON r.id=t.request_id WHERE t.token_hash=$1`, digest(in.Credential)).Scan(&body, &expiry, &deadline, &tokenRevoked, &attemptRevoked, &definition, &status, &control, &scopeRaw)
	if e == pgx.ErrNoRows {
		return out, nil
	}
	if e != nil {
		return out, domain.ErrUnavailable
	}
	now := s.now()
	if tokenRevoked || attemptRevoked || !expiry.After(now) || !deadline.After(now) || control != "" || (status != "queued" && status != "running") {
		return out, nil
	}
	var binding Binding
	var def domain.RequestInput
	if json.Unmarshal(body, &binding) != nil || json.Unmarshal(definition, &def) != nil {
		return out, domain.ErrUnavailable
	}
	hash, e := Fingerprint(def)
	if e != nil || hash != binding.DefinitionSHA256 || !supported(def) || binding.ExecutionMode != replacementMode(def) || binding.Operation != def.Operations[0].Name || binding.ObjectKind != def.Object.Kind || binding.ObjectCode != def.Object.Code || binding.EnvCode != def.EnvCode || binding.SkillsCommit != def.Skills.CommitSHA || binding.Model != def.Model || binding.ProviderID != def.ProviderID || binding.ConnectionVersion != def.ConnectionVersion {
		return out, nil
	}
	var cluster string
	if e = s.Pool.QueryRow(ctx, "SELECT CASE WHEN ack_cluster_id<>'' AND cluster_id<>'' AND ack_cluster_id<>cluster_id THEN '' ELSE coalesce(nullif(ack_cluster_id,''),nullif(cluster_id,''),(SELECT value_json::jsonb #>> '{}' FROM raptor.environment_settings WHERE environment_code=$1 AND key='ackClusterId'),'') END FROM raptor.environments WHERE code=$1", binding.EnvCode).Scan(&cluster); e != nil {
		return out, domain.ErrUnavailable
	}
	if cluster != binding.ClusterID {
		return out, nil
	}
	if !question(def) && !repositoryChange(def) {
		var saved ReplacementScope
		if s.ResolveReplacement == nil || json.Unmarshal(scopeRaw, &saved) != nil || !saved.ResourceValidFor(binding) || !ReplacementSelectorsAllowed(binding, saved, in.Audience, in.Tool, in.Selectors) {
			return out, nil
		}
		if in.Audience == "raptor" && in.Tool == "approval_get" {
			var found bool
			if !uuid.MatchString(in.Selectors["approvalId"]) {
				return out, nil
			}
			if err := s.Pool.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM raptor.approvals WHERE id=$1 AND request_id=$2)", in.Selectors["approvalId"], binding.RequestID).Scan(&found); err != nil {
				return out, domain.ErrUnavailable
			}
			if !found {
				return out, nil
			}
		}
		current, err := s.ResolveReplacement(ctx, binding)
		if err != nil {
			return out, domain.ErrUnavailable
		}
		if current != saved || !current.ResourceValidFor(binding) {
			return out, nil
		}

		out.ReplacementScope = &saved
	} else if repositoryChange(def) && !RepositorySelectorsAllowed(binding, in.Audience, in.Tool, in.Selectors) {
		return out, nil
	} else if question(def) && !SelectorsAllowed(binding, in.Audience, in.Tool, in.Selectors) {
		return out, nil
	}
	out.Active = true
	out.Binding = &binding
	out.ExpiresAt = &expiry
	return out, nil
}
func SelectorsAllowed(b Binding, audience, tool string, selectors map[string]string) bool {
	if tool == "" {
		return len(selectors) == 0 && (audience == "raptor" || audience == "infra")
	}
	expected := map[string]string{}
	required := []string{}
	if audience == "raptor" {
		expected["requestId"] = b.RequestID
		required = []string{"requestId"}
		switch tool {
		case "request_get":
		case "environment_get":
			expected["envCode"] = b.EnvCode
			required = append(required, "envCode")
		case "object_get", "deployments_list":
			if tool == "deployments_list" {
				expected["envCode"] = b.EnvCode
				required = append(required, "envCode")
			}
			expected["kind"] = b.ObjectKind
			expected["code"] = b.ObjectCode
			required = append(required, "kind", "code")
		default:
			return false
		}
	} else if audience == "infra" {
		expected["envCode"] = b.EnvCode
		required = []string{"envCode"}
		switch tool {
		case "deployments_list":
			expected["kind"] = b.ObjectKind
			expected["code"] = b.ObjectCode
			required = append(required, "kind", "code")
		case "deployment_status_get":
			expected["appCode"] = b.ObjectCode
			required = append(required, "appCode")
			if value, ok := selectors["workloadKey"]; ok {
				if !scopeText(value) {
					return false
				}
				expected["workloadKey"] = value
			}

		default:
			return false
		}
	} else {
		return false
	}
	for key, value := range selectors {
		want, ok := expected[key]
		if !ok || value != want || len(value) > 256 {
			return false
		}
	}
	for _, key := range required {
		if selectors[key] == "" {
			return false
		}
	}
	return true
}

func RepositorySelectorsAllowed(b Binding, audience, tool string, selectors map[string]string) bool {
	if b.Operation != "repository.change" || audience != "raptor" {
		return false
	}
	if tool == "" {
		return len(selectors) == 0
	}
	switch tool {
	case "request_get", "environment_get", "object_get":
		return SelectorsAllowed(b, audience, tool, selectors)
	case "pull_request_attach":
		return len(selectors) == 2 && selectors["requestId"] == b.RequestID && selectors["attemptId"] == b.AttemptID
	default:
		return false
	}
}

// VerifyRequestReplacement uses the request's existing immutable execution pins.
// Callers hold the request row lock when creating/claiming an approval.
func (s *Service) VerifyRequestReplacement(ctx context.Context, request, env, proxy string, mutation bool) error {
	if s.ResolveReplacement == nil {
		return domain.ErrUnavailable
	}
	rows, e := s.Pool.Query(ctx, `SELECT a.binding,a.replacement_scope,r.definition FROM raptor.agent_attempt_access a JOIN raptor.requests r ON r.id=a.request_id WHERE a.request_id=$1 AND a.replacement_scope IS NOT NULL`, request)
	if e != nil {
		return domain.ErrUnavailable
	}
	var frozen ReplacementScope
	var binding Binding
	count := 0
	invalid := false
	for rows.Next() {
		var br, sr, dr []byte
		var b Binding
		var scope ReplacementScope
		var def domain.RequestInput
		if rows.Scan(&br, &sr, &dr) != nil || json.Unmarshal(br, &b) != nil || json.Unmarshal(sr, &scope) != nil || json.Unmarshal(dr, &def) != nil {
			invalid = true
			continue
		}
		fingerprint, err := Fingerprint(def)
		if err != nil || fingerprint != b.DefinitionSHA256 || !supported(def) || b.RequestID != request || b.EnvCode != env || b.ObjectCode != proxy || !scope.ResourceValidFor(b) || (mutation && b.ExecutionMode != "replacement") || (!mutation && b.ExecutionMode != "qualification") {
			invalid = true
			continue
		}
		if count > 0 && scope != frozen {
			invalid = true
		}
		frozen = scope
		binding = b
		count++
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return domain.ErrUnavailable
	}
	if invalid || count == 0 {
		return domain.ErrConflict
	}
	current, e := s.ResolveReplacement(ctx, binding)
	if e != nil {
		return domain.ErrUnavailable
	}
	if current != frozen || !current.ResourceValidFor(binding) {
		return domain.ErrConflict
	}
	return nil
}
