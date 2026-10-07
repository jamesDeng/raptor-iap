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
	"strings"
	"time"
)

type Service struct {
	Pool                      *pgxpool.Pool
	RaptorMCPURL, InfraMCPURL string
	Now                       func() time.Time
}
type IssueInput struct {
	AttemptID        string    `json:"attemptId"`
	DefinitionSHA256 string    `json:"definitionSha256"`
	ExpiresAt        time.Time `json:"expiresAt"`
}
type Binding struct {
	RequestID        string `json:"requestId"`
	AttemptID        string `json:"attemptId"`
	Operation        string `json:"operation"`
	ObjectKind       string `json:"objectKind"`
	ObjectCode       string `json:"objectCode"`
	EnvCode          string `json:"envCode"`
	SkillsCommit     string `json:"skillsCommit"`
	Model            string `json:"model"`
	DefinitionSHA256 string `json:"definitionSha256"`
	ClusterID        string `json:"clusterId"`
}
type Issued struct {
	Credential   string    `json:"credential"`
	ExpiresAt    time.Time `json:"expiresAt"`
	Binding      Binding   `json:"binding"`
	RaptorMCPURL string    `json:"raptorMcpUrl"`
	InfraMCPURL  string    `json:"infraMcpUrl"`
}
type CheckInput struct {
	Credential string            `json:"credential"`
	Audience   string            `json:"audience"`
	Tool       string            `json:"tool"`
	Selectors  map[string]string `json:"selectors"`
}
type CheckResult struct {
	Active    bool       `json:"active"`
	Binding   *Binding   `json:"binding,omitempty"`
	ExpiresAt *time.Time `json:"expiresAt,omitempty"`
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
	return def.Type == "agent" && def.Object.Kind == "application" && len(def.Operations) == 1 && def.Operations[0].Name == "application.question" && def.Model == "gpt-5.6-luna" && sha.MatchString(def.Skills.CommitSHA)
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
	if !question(def) || fingerprint != in.DefinitionSHA256 {
		return out, domain.ErrInvalid
	}
	if control != "" || (status != "queued" && status != "running") {
		return out, domain.ErrConflict
	}
	var cluster string
	if e = tx.QueryRow(ctx, "SELECT config->>'ackClusterId' FROM raptor.environments WHERE code=$1", def.EnvCode).Scan(&cluster); e != nil || cluster == "" {
		return out, domain.ErrUnavailable
	}
	out.Binding = Binding{RequestID: id, AttemptID: in.AttemptID, Operation: "application.question", ObjectKind: def.Object.Kind, ObjectCode: def.Object.Code, EnvCode: def.EnvCode, SkillsCommit: def.Skills.CommitSHA, Model: def.Model, DefinitionSHA256: fingerprint, ClusterID: cluster}
	binding, _ := json.Marshal(out.Binding)
	var existing []byte
	var deadline time.Time
	var revoked bool
	e = tx.QueryRow(ctx, "SELECT binding,expires_at,revoked FROM raptor.agent_attempt_access WHERE request_id=$1 AND attempt_id=$2 FOR UPDATE", id, in.AttemptID).Scan(&existing, &deadline, &revoked)
	if e == nil {
		var old Binding
		if json.Unmarshal(existing, &old) != nil {
			return out, domain.ErrUnavailable
		}
		if revoked || !deadline.After(now) || in.ExpiresAt.After(deadline) || old != out.Binding {
			return out, domain.ErrConflict
		}
	} else if e == pgx.ErrNoRows {
		_, e = tx.Exec(ctx, "INSERT INTO raptor.agent_attempt_access(request_id,attempt_id,binding,expires_at) VALUES($1,$2,$3,$4)", id, in.AttemptID, binding, in.ExpiresAt)
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
	var body, definition []byte
	var expiry, deadline time.Time
	var tokenRevoked, attemptRevoked bool
	var status, control string
	e := s.Pool.QueryRow(ctx, `SELECT a.binding,t.expires_at,a.expires_at,t.revoked,a.revoked,r.definition,r.status,r.control_state FROM raptor.agent_tokens t JOIN raptor.agent_attempt_access a ON a.request_id=t.request_id AND a.attempt_id=t.attempt_id JOIN raptor.requests r ON r.id=t.request_id WHERE t.token_hash=$1`, digest(in.Credential)).Scan(&body, &expiry, &deadline, &tokenRevoked, &attemptRevoked, &definition, &status, &control)
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
	if e != nil || hash != binding.DefinitionSHA256 || !question(def) {
		return out, nil
	}
	var cluster string
	if e = s.Pool.QueryRow(ctx, "SELECT config->>'ackClusterId' FROM raptor.environments WHERE code=$1", binding.EnvCode).Scan(&cluster); e != nil {
		return out, domain.ErrUnavailable
	}
	if cluster != binding.ClusterID {
		return out, nil
	}
	if !SelectorsAllowed(binding, in.Audience, in.Tool, in.Selectors) {
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
		case "object_get":
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
		case "cloud_identity_get":
		case "deployments_list":
			expected["kind"] = b.ObjectKind
			expected["code"] = b.ObjectCode
			required = append(required, "kind", "code")
		case "deployment_status_get":
			expected["appCode"] = b.ObjectCode
			expected["clusterId"] = b.ClusterID
			required = append(required, "appCode", "clusterId", "namespace", "name", "uid")
			for _, key := range []string{"namespace", "name", "uid"} {
				expected[key] = selectors[key]
				if strings.TrimSpace(selectors[key]) == "" {
					return false
				}
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
