package requests

import (
	"context"
	"encoding/json"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/domain"
	"time"
)

type GatewayEvent struct {
	EventID      string          `json:"eventId"`
	Sequence     int64           `json:"sequence"`
	Kind         string          `json:"kind"`
	Summary      string          `json:"summary"`
	Details      json.RawMessage `json:"details"`
	EvidenceMode string          `json:"evidenceMode"`
	OccurredAt   time.Time       `json:"occurredAt"`
}
type GatewayReader interface {
	Execution(context.Context, string) (map[string]any, error)
	Progress(context.Context, string, int64) ([]GatewayEvent, error)
}
type RequestView struct {
	Request            domain.Request    `json:"request"`
	Execution          map[string]any    `json:"execution,omitempty"`
	ExecutionAvailable bool              `json:"executionAvailable"`
	Targets            []RestartItem     `json:"targets"`
	Approvals          []domain.Approval `json:"approvals"`
}

func (s *Service) View(ctx context.Context, id string) (RequestView, error) {
	v := RequestView{Approvals: []domain.Approval{}, Targets: []RestartItem{}}
	r, e := s.Get(ctx, id)
	if e != nil {
		return v, e
	}
	v.Request = r
	if r.Definition.Type == "agent" && s.Gateway != nil {
		v.Execution, e = s.Gateway.Execution(ctx, id)
		v.ExecutionAvailable = e == nil
		if e == nil {
			status, _ := v.Execution["status"].(string)
			switch status {
			case "queued", "running", "waiting_approval", "waiting_review", "blocked", "interrupted", "completed", "failed", "cancelled":
				if r.Status != "cancelled" {
					if _, e = s.Pool.Exec(ctx, "UPDATE raptor.requests SET status=$2 WHERE id=$1 AND status<>'cancelled'", id, status); e != nil {
						return v, domain.ErrUnavailable
					}
					v.Request.Status = status
				}
			}
		}
	} else if r.Definition.Type == "direct" {
		v.ExecutionAvailable = true
		v.Targets, e = s.RestartItems(ctx, id)
		if e != nil {
			return v, e
		}
	}
	rows, e := s.Pool.Query(ctx, "SELECT id::text,request_id::text,action_id,binding,state,guidance FROM raptor.approvals WHERE request_id=$1 ORDER BY id", id)
	if e != nil {
		return v, domain.ErrUnavailable
	}
	defer rows.Close()
	for rows.Next() {
		var a domain.Approval
		if e = rows.Scan(&a.ID, &a.RequestID, &a.ActionID, &a.Binding, &a.State, &a.Guidance); e != nil {
			return v, domain.ErrUnavailable
		}
		v.Approvals = append(v.Approvals, a)
	}
	return v, rows.Err()
}

type Timeline struct {
	Events          []domain.Event `json:"events"`
	SyncUnavailable bool           `json:"syncUnavailable"`
}

func (s *Service) Timeline(ctx context.Context, id string, after int64) (Timeline, error) {
	out := Timeline{Events: []domain.Event{}}
	request, e := s.Get(ctx, id)
	if e != nil {
		return out, e
	}
	if after < 0 {
		return out, domain.ErrInvalid
	}
	if request.Definition.Type == "agent" {
		if s.Gateway == nil {
			out.SyncUnavailable = true
		} else {
			var cursor int64
			if e = s.Pool.QueryRow(ctx, "SELECT COALESCE(max(source_sequence),0) FROM raptor.events WHERE request_id=$1", id).Scan(&cursor); e != nil {
				return out, domain.ErrUnavailable
			}
			events, e := s.Gateway.Progress(ctx, id, cursor)
			if e != nil {
				out.SyncUnavailable = true
			} else {
				for _, event := range events {
					if event.EventID == "" || event.Sequence <= 0 || len(event.Summary) > 4096 || len(event.Details) > 4096 || event.EvidenceMode != "simulated" {
						return out, domain.ErrUnavailable
					}
					if len(event.Details) == 0 {
						event.Details = json.RawMessage(`{}`)
					}
					if _, e = s.Pool.Exec(ctx, "INSERT INTO raptor.events(request_id,kind,summary,details,evidence_mode,source_event_id,source_sequence) VALUES($1,$2,$3,$4,$5,$6,$7) ON CONFLICT(request_id,source_event_id) DO NOTHING", id, event.Kind, event.Summary, event.Details, event.EvidenceMode, event.EventID, event.Sequence); e != nil {
						return out, domain.ErrUnavailable
					}
				}
			}
		}
	}
	rows, e := s.Pool.Query(ctx, "SELECT sequence,request_id::text,kind,summary,details,evidence_mode,occurred_at FROM raptor.events WHERE request_id=$1 AND sequence>$2 ORDER BY sequence LIMIT 200", id, after)
	if e != nil {
		return out, domain.ErrUnavailable
	}
	defer rows.Close()
	for rows.Next() {
		var event domain.Event
		if e = rows.Scan(&event.Sequence, &event.RequestID, &event.Kind, &event.Summary, &event.Details, &event.EvidenceMode, &event.OccurredAt); e != nil {
			return out, domain.ErrUnavailable
		}
		out.Events = append(out.Events, event)
	}
	return out, rows.Err()
}
