package requests

import (
	"context"
	"encoding/json"
	"github.com/jackc/pgx/v5"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/domain"
	"reflect"
	"time"
	"unicode/utf8"
)

// PublicExecution is deliberately independent of Gateway storage/private checkpoint paths.
type PublicExecution struct {
	RequestID        string               `json:"requestId,omitempty"`
	AttemptID        string               `json:"attemptId,omitempty"`
	Status           string               `json:"status"`
	RuntimeMode      string               `json:"runtimeMode,omitempty"`
	Stage            string               `json:"stage,omitempty"`
	AppliedSkills    domain.SkillsVersion `json:"appliedSkills"`
	RecoveryNeeded   bool                 `json:"recoveryNeeded"`
	UpdatedAt        *time.Time           `json:"updatedAt,omitempty"`
	Result           *PublicResult        `json:"result,omitempty"`
	CheckpointStatus string               `json:"checkpointStatus,omitempty"`
	CleanupStatus    string               `json:"cleanupStatus,omitempty"`
	FailureCode      string               `json:"failureCode,omitempty"`
	Cleanup          string               `json:"cleanup,omitempty"`
}
type PublicResult struct {
	RequestID     string           `json:"requestId"`
	AttemptID     string           `json:"attemptId"`
	SelectedModel string           `json:"selectedModel,omitempty"`
	ActualModel   string           `json:"actualModel"`
	Answer        string           `json:"answer"`
	Evidence      []PublicEvidence `json:"evidence,omitempty"`
	GeneratedAt   *time.Time       `json:"generatedAt,omitempty"`
	Usage         *PublicUsage     `json:"usage,omitempty"`
}
type PublicUsage struct {
	InputTokens  int64 `json:"inputTokens"`
	OutputTokens int64 `json:"outputTokens"`
}
type PublicEvidence struct {
	Server       string           `json:"server"`
	Tool         string           `json:"tool"`
	ObservedAt   time.Time        `json:"observedAt"`
	EvidenceMode string           `json:"evidenceMode"`
	Identity     EvidenceIdentity `json:"identity"`
	State        json.RawMessage  `json:"state"`
}
type EvidenceIdentity struct {
	EnvCode    string `json:"envCode,omitempty"`
	ObjectCode string `json:"objectCode,omitempty"`
	AppCode    string `json:"appCode,omitempty"`
	Kind       string `json:"kind,omitempty"`
	ClusterID  string `json:"clusterId,omitempty"`
	Namespace  string `json:"namespace,omitempty"`
	Name       string `json:"name,omitempty"`
	UID        string `json:"uid,omitempty"`
}

func publicExecution(raw map[string]any, request domain.Request) (map[string]any, error) {
	b, e := json.Marshal(raw)
	if e != nil || len(b) > 65536 {
		return nil, domain.ErrUnavailable
	}
	var v PublicExecution
	if json.Unmarshal(b, &v) != nil {
		return nil, domain.ErrUnavailable
	}
	if v.RequestID != "" && v.RequestID != request.ID {
		return nil, domain.ErrUnavailable
	}
	switch v.Status {
	case "queued", "running", "waiting_approval", "waiting_review", "blocked", "interrupted", "completed", "failed", "cancelled":
	default:
		return nil, domain.ErrUnavailable
	}
	switch v.RuntimeMode {
	case "", "live", "simulated", "disabled":
	default:
		return nil, domain.ErrUnavailable
	}
	if v.RuntimeMode == "live" && (v.RequestID != request.ID || v.AttemptID == "") {
		return nil, domain.ErrUnavailable
	}
	if r := v.Result; r != nil {
		if r.RequestID != request.ID || r.AttemptID == "" || r.AttemptID != v.AttemptID || r.Answer == "" || !utf8.ValidString(r.Answer) || len(r.Answer) > 16384 || len(r.Evidence) > 32 {
			return nil, domain.ErrUnavailable
		}
		for _, o := range r.Evidence {
			if o.EvidenceMode != "live" && o.EvidenceMode != "simulated" && o.EvidenceMode != "catalog" {
				return nil, domain.ErrUnavailable
			}
			if len(o.State) > 4096 {
				return nil, domain.ErrUnavailable
			}
			if o.Identity.EnvCode != "" && o.Identity.EnvCode != request.Definition.EnvCode {
				return nil, domain.ErrUnavailable
			}
			for _, code := range []string{o.Identity.AppCode, o.Identity.ObjectCode} {
				if code != "" && code != request.Definition.Object.Code {
					return nil, domain.ErrUnavailable
				}
			}
		}
	}
	if isQuestion(request.Definition) && v.RuntimeMode == "live" && v.Status == "cancelled" && (v.CleanupStatus != "confirmed" || v.RecoveryNeeded) {
		return nil, domain.ErrUnavailable
	}
	if isQuestion(request.Definition) && v.RuntimeMode == "live" && v.Status == "completed" {
		if v.Result == nil || v.Result.ActualModel != request.Definition.Model || v.Result.SelectedModel != request.Definition.Model || v.CheckpointStatus != "verified" || v.CleanupStatus != "confirmed" || v.RecoveryNeeded {
			return nil, domain.ErrUnavailable
		}
		contextRead, identityRead, discoveryRead, statusRead := false, false, false, false
		for _, o := range v.Result.Evidence {
			if o.Server == "raptor" && o.Tool == "request_get" && o.EvidenceMode == "live" && !o.ObservedAt.IsZero() && o.Identity.EnvCode == request.Definition.EnvCode && o.Identity.ObjectCode == request.Definition.Object.Code {
				contextRead = true
			}
			if o.Server != "infra" || o.EvidenceMode != "live" || o.ObservedAt.IsZero() {
				continue
			}
			switch o.Tool {
			case "cloud_identity_get":
				identityRead = true
			case "deployments_list":
				discoveryRead = true
			case "deployment_status_get":
				statusRead = o.Identity.EnvCode == request.Definition.EnvCode && o.Identity.AppCode == request.Definition.Object.Code && o.Identity.ClusterID != "" && o.Identity.Namespace != "" && o.Identity.Name != "" && o.Identity.UID != ""
			}
		}
		if !contextRead || !identityRead || !discoveryRead || !statusRead {
			return nil, domain.ErrUnavailable
		}
	}
	b, _ = json.Marshal(v)
	var out map[string]any
	json.Unmarshal(b, &out)
	return out, nil
}
func isQuestion(in domain.RequestInput) bool {
	return in.Type == "agent" && len(in.Operations) == 1 && in.Operations[0].Name == "application.question"
}

func (s *Service) savedExecution(ctx context.Context, id string) (map[string]any, *time.Time, error) {
	var body []byte
	var synced time.Time
	e := s.Pool.QueryRow(ctx, "SELECT public_view,synced_at FROM raptor.execution_views WHERE request_id=$1", id).Scan(&body, &synced)
	if e == pgx.ErrNoRows {
		return nil, nil, nil
	}
	if e != nil {
		return nil, nil, domain.ErrUnavailable
	}
	var out map[string]any
	if json.Unmarshal(body, &out) != nil {
		return nil, nil, domain.ErrUnavailable
	}
	return out, &synced, nil
}
func (s *Service) syncExecution(ctx context.Context, r domain.Request, candidate map[string]any) (map[string]any, *time.Time, bool, error) {
	tx, e := s.Pool.Begin(ctx)
	if e != nil {
		return nil, nil, false, domain.ErrUnavailable
	}
	defer tx.Rollback(ctx)
	var status, control string
	if e = tx.QueryRow(ctx, "SELECT status,control_state FROM raptor.requests WHERE id=$1 FOR UPDATE", r.ID).Scan(&status, &control); e != nil {
		return nil, nil, false, domain.ErrUnavailable
	}
	var oldBody []byte
	var oldSync time.Time
	var old map[string]any
	e = tx.QueryRow(ctx, "SELECT public_view,synced_at FROM raptor.execution_views WHERE request_id=$1", r.ID).Scan(&oldBody, &oldSync)
	if e != nil && e != pgx.ErrNoRows {
		return nil, nil, false, domain.ErrUnavailable
	}
	if e == nil {
		if json.Unmarshal(oldBody, &old) != nil {
			return nil, nil, false, domain.ErrUnavailable
		}
		stale := false
		if status == "completed" || status == "cancelled" {
			stale = !reflect.DeepEqual(candidate, old)
		}
		oldStamp, _ := old["updatedAt"].(string)
		newStamp, _ := candidate["updatedAt"].(string)
		if oldStamp != "" {
			ot, oe := time.Parse(time.RFC3339Nano, oldStamp)
			nt, ne := time.Parse(time.RFC3339Nano, newStamp)
			if oe != nil || ne != nil || nt.Before(ot) {
				stale = true
			}
		}
		if stale {
			return old, &oldSync, false, nil
		}
	}
	body, e := json.Marshal(candidate)
	if e != nil {
		return nil, nil, false, domain.ErrUnavailable
	}
	var synced time.Time
	if e = tx.QueryRow(ctx, "INSERT INTO raptor.execution_views(request_id,public_view,synced_at) VALUES($1,$2,now()) ON CONFLICT(request_id) DO UPDATE SET public_view=excluded.public_view,synced_at=excluded.synced_at RETURNING synced_at", r.ID, body).Scan(&synced); e != nil {
		return nil, nil, false, domain.ErrUnavailable
	}
	next, _ := candidate["status"].(string)
	if (control == "" || control == "cancel_requested") && status != "completed" && status != "cancelled" {
		if _, e = tx.Exec(ctx, "UPDATE raptor.requests SET status=$2 WHERE id=$1", r.ID, next); e != nil {
			return nil, nil, false, domain.ErrUnavailable
		}
	}
	if e = tx.Commit(ctx); e != nil {
		return nil, nil, false, domain.ErrUnavailable
	}
	return candidate, &synced, true, nil
}
