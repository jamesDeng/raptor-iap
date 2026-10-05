package requests

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/domain"
)

type SignalClient interface {
	SendSignal(context.Context, string, json.RawMessage) error
}

func (s *Service) DispatchPending(ctx context.Context, c GatewayClient) error {
	for i := 0; i < 50; i++ {
		tx, e := s.Pool.Begin(ctx)
		if e != nil {
			return domain.ErrUnavailable
		}
		var id, topic, entity string
		var payload []byte
		e = tx.QueryRow(ctx, "SELECT id,topic,entity_id,payload FROM raptor.outbox WHERE NOT delivered AND failed_reason='' ORDER BY created_at,id FOR UPDATE SKIP LOCKED LIMIT 1").Scan(&id, &topic, &entity, &payload)
		if e == pgx.ErrNoRows {
			_ = tx.Rollback(ctx)
			return nil
		}
		if e != nil {
			_ = tx.Rollback(ctx)
			return domain.ErrUnavailable
		}
		if topic == "dispatch" {
			e = c.PutRequest(ctx, entity)
		} else if sc, ok := c.(SignalClient); ok {
			e = sc.SendSignal(ctx, entity, payload)
		} else {
			e = domain.ErrUnavailable
		}
		if errors.Is(e, domain.ErrInvalid) {
			if _, e = tx.Exec(ctx, "UPDATE raptor.outbox SET failed_reason='delivery rejected as invalid' WHERE id=$1", id); e != nil {
				tx.Rollback(ctx)
				return domain.ErrUnavailable
			}
			if _, e = tx.Exec(ctx, "INSERT INTO raptor.events(request_id,kind,summary,evidence_mode) VALUES($1,'status','Dispatch rejected; operator attention required','simulated')", entity); e != nil {
				tx.Rollback(ctx)
				return domain.ErrUnavailable
			}
			if e = tx.Commit(ctx); e != nil {
				return domain.ErrUnavailable
			}
			continue
		}
		if e != nil {
			_ = tx.Rollback(ctx)
			return domain.ErrUnavailable
		}
		_, e = tx.Exec(ctx, "UPDATE raptor.outbox SET delivered=true WHERE id=$1", id)
		if e == nil {
			e = tx.Commit(ctx)
		} else {
			_ = tx.Rollback(ctx)
		}
		if e != nil {
			return domain.ErrUnavailable
		}
	}
	return nil
}
func QueueSignal(ctx context.Context, tx pgx.Tx, requestID, kind string, payload any) error {
	wire, e := json.Marshal(payload)
	if e != nil || len(wire) > 4096 {
		return domain.ErrInvalid
	}
	id := domain.NewID()
	b, e := json.Marshal(map[string]any{"signalId": id, "kind": kind, "payload": payload})
	if e != nil {
		return e
	}
	_, e = tx.Exec(ctx, "INSERT INTO raptor.outbox(id,topic,entity_id,payload) VALUES($1,'signal',$2,$3)", id, requestID, b)
	return e
}
