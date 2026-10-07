package requests

import (
	"context"
	"encoding/json"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/domain"
	"sort"
	"time"
)

type GatewayEvent struct {
	RequestID    string          `json:"requestId"`
	AttemptID    string          `json:"attemptId"`
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
	Request              domain.Request    `json:"request"`
	Execution            map[string]any    `json:"execution,omitempty"`
	CancellationPending  bool              `json:"cancellationPending"`
	LastSuccessfulSyncAt *time.Time        `json:"lastSuccessfulSyncAt,omitempty"`
	ExecutionAvailable   bool              `json:"executionAvailable"`
	Targets              []RestartItem     `json:"targets"`
	Approvals            []domain.Approval `json:"approvals"`
}

func (s *Service) View(ctx context.Context, id string) (RequestView, error) {
	v := RequestView{Approvals: []domain.Approval{}, Targets: []RestartItem{}}
	r, e := s.Get(ctx, id)
	if e != nil {
		return v, e
	}
	v.Request = r
	if r.Definition.Type == "agent" {
		e = domain.ErrUnavailable
		if s.Gateway != nil {
			v.Execution, e = s.Gateway.Execution(ctx, id)
		}
		if e == nil {
			v.Execution, e = publicExecution(v.Execution, r)
		}
		if e == nil {
			var accepted bool
			v.Execution, v.LastSuccessfulSyncAt, accepted, e = s.syncExecution(ctx, r, v.Execution)
			if e != nil {
				return v, e
			}
			v.ExecutionAvailable = accepted
		} else {
			v.Execution, v.LastSuccessfulSyncAt, e = s.savedExecution(ctx, id)
			if e != nil {
				return v, e
			}
		}
		v.Request, e = s.Get(ctx, id)
		if e != nil {
			return v, e
		}
		var control string
		if e = s.Pool.QueryRow(ctx, "SELECT control_state FROM raptor.requests WHERE id=$1", id).Scan(&control); e != nil {
			return v, domain.ErrUnavailable
		}
		v.CancellationPending = control == "cancel_requested" && v.Request.Status != "cancelled" && v.Request.Status != "completed"
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
				if len(events) > 200 {
					return out, domain.ErrUnavailable
				}
				sort.SliceStable(events, func(i, j int) bool { return events[i].Sequence < events[j].Sequence })
				tx, e := s.Pool.Begin(ctx)
				if e != nil {
					return out, domain.ErrUnavailable
				}
				defer tx.Rollback(ctx)
				var lockedID string
				if e = tx.QueryRow(ctx, "SELECT id::text FROM raptor.requests WHERE id=$1 FOR UPDATE", id).Scan(&lockedID); e != nil {
					return out, domain.ErrUnavailable
				}
				if e = tx.QueryRow(ctx, "SELECT COALESCE(max(source_sequence),0) FROM raptor.events WHERE request_id=$1", id).Scan(&cursor); e != nil {
					return out, domain.ErrUnavailable
				}
				for _, event := range events {
					if event.EventID == "" || event.Sequence <= 0 || len(event.Summary) > 4096 || len(event.Details) > 4096 || (event.EvidenceMode != "simulated" && event.EvidenceMode != "live") || (event.RequestID != "" && event.RequestID != id) || (event.EvidenceMode == "live" && (event.RequestID != id || event.AttemptID == "" || event.OccurredAt.IsZero())) {
						return out, domain.ErrUnavailable
					}
					if event.Sequence > cursor+1 {
						return out, domain.ErrUnavailable
					}
					if len(event.Details) == 0 {
						event.Details = json.RawMessage(`{}`)
					}
					occurred := event.OccurredAt
					if occurred.IsZero() {
						occurred = time.Now().UTC()
					}
					var savedSequence int64
					e = tx.QueryRow(ctx, `INSERT INTO raptor.events(request_id,kind,summary,details,evidence_mode,source_event_id,source_sequence,attempt_id,occurred_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)
 ON CONFLICT(request_id,source_event_id) DO UPDATE SET source_event_id=excluded.source_event_id
 WHERE raptor.events.kind=excluded.kind AND raptor.events.summary=excluded.summary AND raptor.events.details=excluded.details AND raptor.events.evidence_mode=excluded.evidence_mode AND raptor.events.source_sequence=excluded.source_sequence AND raptor.events.attempt_id=excluded.attempt_id AND ($10 OR raptor.events.occurred_at=excluded.occurred_at) RETURNING sequence`, id, event.Kind, event.Summary, event.Details, event.EvidenceMode, event.EventID, event.Sequence, event.AttemptID, occurred, event.OccurredAt.IsZero()).Scan(&savedSequence)
					if e != nil {
						return out, domain.ErrUnavailable
					}
					if event.Sequence > cursor {
						cursor = event.Sequence
					}
				}
				if e = tx.Commit(ctx); e != nil {
					return out, domain.ErrUnavailable
				}
			}
		}
	}
	rows, e := s.Pool.Query(ctx, "SELECT sequence,request_id::text,kind,summary,details,evidence_mode,occurred_at,attempt_id FROM raptor.events WHERE request_id=$1 AND sequence>$2 ORDER BY sequence LIMIT 200", id, after)
	if e != nil {
		return out, domain.ErrUnavailable
	}
	defer rows.Close()
	for rows.Next() {
		var event domain.Event
		if e = rows.Scan(&event.Sequence, &event.RequestID, &event.Kind, &event.Summary, &event.Details, &event.EvidenceMode, &event.OccurredAt, &event.AttemptID); e != nil {
			return out, domain.ErrUnavailable
		}
		out.Events = append(out.Events, event)
	}
	return out, rows.Err()
}
