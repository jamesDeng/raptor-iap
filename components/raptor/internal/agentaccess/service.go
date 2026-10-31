// Package agentaccess owns request-bound opaque agent credentials in the Raptor schema.
package agentaccess

import (
	"bytes"
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

// issueQuery lets preparation and locked validation use identical predicates;
// locked validation always uses the transaction's existing connection.
type issueQuery interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}

type issueAttempt struct {
	exists   bool
	binding  Binding
	scope    *ReplacementScope
	deadline time.Time
	revoked  bool
}

func issueBinding(ctx context.Context, q issueQuery, id string, in IssueInput, locked bool) (Binding, bool, error) {
	var body []byte
	var status, control string
	sql := "SELECT definition,status,control_state FROM raptor.requests WHERE id=$1"
	if locked {
		sql += " FOR UPDATE"
	}
	if e := q.QueryRow(ctx, sql, id).Scan(&body, &status, &control); e == pgx.ErrNoRows {
		return Binding{}, false, domain.ErrNotFound
	} else if e != nil {
		return Binding{}, false, domain.ErrUnavailable
	}
	var def domain.RequestInput
	if json.Unmarshal(body, &def) != nil {
		return Binding{}, false, domain.ErrUnavailable
	}
	fingerprint, e := Fingerprint(def)
	if e != nil {
		return Binding{}, false, domain.ErrUnavailable
	}
	if !supported(def) || fingerprint != in.DefinitionSHA256 {
		return Binding{}, false, domain.ErrInvalid
	}
	if control != "" || (status != "queued" && status != "running") {
		return Binding{}, false, domain.ErrConflict
	}
	replacement := !question(def) && !repositoryChange(def)
	if replacement && replacementMode(def) != "replacement" && replacementMode(def) != "qualification" {
		return Binding{}, false, domain.ErrConflict
	}
	var cluster string
	if e = q.QueryRow(ctx, "SELECT CASE WHEN ack_cluster_id<>'' AND cluster_id<>'' AND ack_cluster_id<>cluster_id THEN '' ELSE coalesce(nullif(ack_cluster_id,''),nullif(cluster_id,''),(SELECT value_json::jsonb #>> '{}' FROM raptor.environment_settings WHERE environment_code=$1 AND key='ackClusterId'),'') END FROM raptor.environments WHERE code=$1", def.EnvCode).Scan(&cluster); e != nil || cluster == "" {
		return Binding{}, false, domain.ErrUnavailable
	}
	return Binding{ExecutionMode: replacementMode(def), RequestID: id, AttemptID: in.AttemptID, Operation: def.Operations[0].Name, ObjectKind: def.Object.Kind, ObjectCode: def.Object.Code, EnvCode: def.EnvCode, SkillsCommit: def.Skills.CommitSHA, Model: def.Model, ProviderID: def.ProviderID, ConnectionVersion: def.ConnectionVersion, DefinitionSHA256: fingerprint, ClusterID: cluster}, replacement, nil
}

// Include expired and revoked history: neither can reset a request's frozen
// realization. Keep observed IDs to detect removal, while allowing compatible
// concurrent appends (unlike an approval's exact attempt-list snapshot).
func issueProofs(ctx context.Context, q issueQuery, b Binding) (map[string]ReplacementScope, error) {
	rows, e := q.Query(ctx, "SELECT attempt_id::text,binding,replacement_scope FROM raptor.agent_attempt_access WHERE request_id=$1 AND (replacement_scope IS NOT NULL OR binding->>'operation'='db-proxy.replace-nodes')", b.RequestID)
	if e != nil {
		return nil, domain.ErrUnavailable
	}
	defer rows.Close()
	proofs := map[string]ReplacementScope{}
	invalid := false
	var frozen *ReplacementScope
	for rows.Next() {
		var id string
		var br, sr []byte
		var old Binding
		var scope ReplacementScope
		if rows.Scan(&id, &br, &sr) != nil || json.Unmarshal(br, &old) != nil || json.Unmarshal(sr, &scope) != nil {
			invalid = true
			continue
		}
		expected := b
		expected.AttemptID = id
		if old != expected || !scope.ResourceValidFor(b) || (frozen != nil && *frozen != scope) {
			invalid = true
		}
		proofs[id] = scope
		copy := scope
		frozen = &copy
	}
	if rows.Err() != nil {
		return nil, domain.ErrUnavailable
	}
	if invalid {
		return nil, domain.ErrConflict
	}
	return proofs, nil
}

func readIssueAttempt(ctx context.Context, q issueQuery, b Binding, in IssueInput, now time.Time, locked bool) (issueAttempt, error) {
	var a issueAttempt
	var raw, sr []byte
	sql := "SELECT binding,expires_at,revoked,replacement_scope FROM raptor.agent_attempt_access WHERE request_id=$1 AND attempt_id=$2"
	if locked {
		sql += " FOR UPDATE"
	}
	e := q.QueryRow(ctx, sql, b.RequestID, b.AttemptID).Scan(&raw, &a.deadline, &a.revoked, &sr)
	if e == pgx.ErrNoRows {
		return a, nil
	}
	if e != nil {
		return a, domain.ErrUnavailable
	}
	a.exists = true
	// Revoke's unknown-attempt tombstone deliberately has an empty binding.
	if a.revoked || !a.deadline.After(now) || in.ExpiresAt.After(a.deadline) {
		return a, domain.ErrConflict
	}
	if json.Unmarshal(raw, &a.binding) != nil {
		return a, domain.ErrUnavailable
	}
	if a.binding != b {
		return a, domain.ErrConflict
	}
	if len(sr) != 0 {
		var scope ReplacementScope
		if json.Unmarshal(sr, &scope) != nil || !scope.ResourceValidFor(b) {
			return a, domain.ErrConflict
		}
		a.scope = &scope
	} else if b.Operation == "db-proxy.replace-nodes" {
		return a, domain.ErrConflict
	}
	return a, nil
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
	// Phase A: finish every SQL read before catalog/provider resolution. Both
	// resolver callbacks may themselves borrow this same pool, even at MaxConns=1.
	binding, replacement, e := issueBinding(ctx, s.Pool, id, in, false)
	if e != nil {
		return out, e
	}
	var observed map[string]ReplacementScope
	if replacement {
		if s.ResolveReplacement == nil {
			return out, domain.ErrUnavailable
		}
		observed, e = issueProofs(ctx, s.Pool, binding)
		if e != nil {
			return out, e
		}
	}
	selected, e := readIssueAttempt(ctx, s.Pool, binding, in, now, false)
	if e != nil {
		return out, e
	}
	var scopeRaw []byte
	if replacement {
		scope, err := s.ResolveReplacement(ctx, binding)
		if err != nil {
			return out, domain.ErrUnavailable
		}
		if !scope.ResourceValidFor(binding) {
			return out, domain.ErrConflict
		}
		for _, saved := range observed {
			if saved != scope {
				return out, domain.ErrConflict
			}
		}
		out.ReplacementScope = &scope
		scopeRaw, _ = json.Marshal(scope)
	}
	// Phase B: request-first serialization, with all authorization reads on this
	// one connection. No catalog or external callback runs while holding its lock.
	tx, e := s.Pool.Begin(ctx)
	if e != nil {
		return Issued{}, domain.ErrUnavailable
	}
	defer tx.Rollback(ctx)
	locked, _, e := issueBinding(ctx, tx, id, in, true)
	if e != nil {
		return Issued{}, e
	}
	if locked != binding {
		return Issued{}, domain.ErrConflict
	}
	if replacement {
		current, err := issueProofs(ctx, tx, locked)
		if err != nil {
			return Issued{}, err
		}
		for _, saved := range current {
			if saved != *out.ReplacementScope {
				return Issued{}, domain.ErrConflict
			}
		}
		for attempt := range observed {
			if _, ok := current[attempt]; !ok {
				return Issued{}, domain.ErrConflict
			}
		}
	}
	current, e := readIssueAttempt(ctx, tx, locked, in, s.now(), true)
	if e != nil {
		return Issued{}, e
	}
	if selected.exists && (!current.exists || selected.binding != current.binding || !selected.deadline.Equal(current.deadline)) {
		return Issued{}, domain.ErrConflict
	}
	if current.scope != nil && (out.ReplacementScope == nil || *current.scope != *out.ReplacementScope) {
		return Issued{}, domain.ErrConflict
	}
	out.Binding = locked
	raw, _ := json.Marshal(locked)
	if !current.exists {
		if _, e = tx.Exec(ctx, "INSERT INTO raptor.agent_attempt_access(request_id,attempt_id,binding,expires_at,replacement_scope) VALUES($1,$2,$3,$4,$5)", id, in.AttemptID, raw, in.ExpiresAt, scopeRaw); e != nil {
			return Issued{}, domain.ErrConflict
		}
	}
	if _, e = tx.Exec(ctx, "UPDATE raptor.agent_tokens SET revoked=true WHERE request_id=$1 AND attempt_id=$2", id, in.AttemptID); e != nil {
		return Issued{}, domain.ErrUnavailable
	}
	// Slow proof/lock waits must not mint an expired token or extend absolute TTL.
	finalNow := s.now()
	if ctx.Err() != nil {
		return Issued{}, domain.ErrUnavailable
	}
	if !in.ExpiresAt.After(finalNow) || (current.exists && !current.deadline.After(finalNow)) {
		return Issued{}, domain.ErrConflict
	}
	var random [32]byte
	if _, e = rand.Read(random[:]); e != nil {
		return Issued{}, domain.ErrUnavailable
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

// replacementRows releases its query connection before any catalog/provider proof.
type replacementQuery interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
}

func replacementRows(ctx context.Context, query replacementQuery, request, env, proxy string, mutation bool) (ReplacementScope, Binding, []byte, error) {
	rows, e := query.Query(ctx, `SELECT a.binding,a.replacement_scope,r.definition FROM raptor.agent_attempt_access a JOIN raptor.requests r ON r.id=a.request_id WHERE a.request_id=$1 AND (a.replacement_scope IS NOT NULL OR a.binding->>'operation'='db-proxy.replace-nodes') ORDER BY a.attempt_id`, request)
	if e != nil {
		return ReplacementScope{}, Binding{}, nil, domain.ErrUnavailable
	}
	var frozen ReplacementScope
	var binding Binding
	var snapshot bytes.Buffer
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
		snapshot.Write(br)
		snapshot.WriteByte(0)
		snapshot.Write(sr)
		snapshot.WriteByte(0)
		snapshot.Write(dr)
		snapshot.WriteByte(0)
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
		return ReplacementScope{}, Binding{}, nil, domain.ErrUnavailable
	}
	if invalid || count == 0 {
		return ReplacementScope{}, Binding{}, nil, domain.ErrConflict
	}
	return frozen, binding, snapshot.Bytes(), nil
}

// PrepareRequestReplacement resolves current proof without holding an approval
// transaction. The returned validator rechecks the exact DB snapshot through the
// locked transaction's existing connection, never borrowing another pool slot.
func (s *Service) PrepareRequestReplacement(ctx context.Context, request, env, proxy string, mutation bool) (func(context.Context, pgx.Tx) error, error) {
	if s.ResolveReplacement == nil {
		return nil, domain.ErrUnavailable
	}
	frozen, binding, snapshot, e := replacementRows(ctx, s.Pool, request, env, proxy, mutation)
	if e != nil {
		return nil, e
	}
	current, e := s.ResolveReplacement(ctx, binding)
	if e != nil {
		return nil, domain.ErrUnavailable
	}
	if current != frozen || !current.ResourceValidFor(binding) {
		return nil, domain.ErrConflict
	}
	return func(ctx context.Context, tx pgx.Tx) error {
		_, _, locked, e := replacementRows(ctx, tx, request, env, proxy, mutation)
		if e != nil {
			return e
		}
		if !bytes.Equal(snapshot, locked) {
			return domain.ErrConflict
		}
		return nil
	}, nil
}
func (s *Service) VerifyRequestReplacement(ctx context.Context, request, env, proxy string, mutation bool) error {
	_, e := s.PrepareRequestReplacement(ctx, request, env, proxy, mutation)
	return e
}
