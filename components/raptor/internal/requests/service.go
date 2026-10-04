package requests

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/catalog"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/domain"
	"time"
)

type GatewayClient interface {
	PutRequest(context.Context, string) error
}
type Service struct {
	Pool          *pgxpool.Pool
	Catalog       *catalog.Service
	ResolveSkills func(context.Context, domain.SkillsVersion) (domain.SkillsVersion, error)
	RestartNow    func() time.Time
	Gateway       GatewayReader
}

func NewService(p *pgxpool.Pool, c *catalog.Service) *Service { return &Service{Pool: p, Catalog: c} }
func scanRequest(row pgx.Row) (domain.Request, error) {
	var r domain.Request
	var b []byte
	e := row.Scan(&r.ID, &r.CreatorID, &b, &r.SchemaHash, &r.Status, &r.CreatedAt)
	if e == pgx.ErrNoRows {
		return r, domain.ErrNotFound
	}
	if e != nil {
		return r, domain.ErrUnavailable
	}
	if json.Unmarshal(b, &r.Definition) != nil {
		return r, domain.ErrUnavailable
	}
	return r, nil
}

const requestColumns = "id,creator_id,definition,schema_hash,status,created_at"

func (s *Service) Get(ctx context.Context, id string) (domain.Request, error) {
	return scanRequest(s.Pool.QueryRow(ctx, "SELECT "+requestColumns+" FROM raptor.requests WHERE id=$1", id))
}
func (s *Service) List(ctx context.Context) ([]domain.Request, error) {
	rows, e := s.Pool.Query(ctx, "SELECT id FROM raptor.requests ORDER BY created_at DESC")
	if e != nil {
		return nil, domain.ErrUnavailable
	}
	defer rows.Close()
	ids := []string{}
	for rows.Next() {
		var id string
		if rows.Scan(&id) != nil {
			return nil, domain.ErrUnavailable
		}
		ids = append(ids, id)
	}
	rows.Close()
	out := []domain.Request{}
	for _, id := range ids {
		r, e := s.Get(ctx, id)
		if e != nil {
			return nil, e
		}
		out = append(out, r)
	}
	return out, nil
}
func (s *Service) Create(ctx context.Context, u domain.User, key string, in domain.RequestInput) (domain.Request, error) {
	if u.ID == "" || key == "" || len(key) > 200 {
		return domain.Request{}, domain.ErrInvalid
	}
	original, e := json.Marshal(in)
	if e != nil {
		return domain.Request{}, domain.ErrInvalid
	}
	digest := sha256.Sum256(original)
	hash := hex.EncodeToString(digest[:])
	var existingID, existingHash string
	e = s.Pool.QueryRow(ctx, "SELECT id,input_hash FROM raptor.requests WHERE creator_id=$1 AND idempotency_key=$2", u.ID, key).Scan(&existingID, &existingHash)
	if e == nil {
		if existingHash != hash {
			return domain.Request{}, domain.ErrConflict
		}
		return s.Get(ctx, existingID)
	}
	if e != pgx.ErrNoRows {
		return domain.Request{}, domain.ErrUnavailable
	}
	var input domain.RequestInput
	if json.Unmarshal(original, &input) != nil {
		return domain.Request{}, domain.ErrInvalid
	}
	if e = s.Validate(input); e != nil {
		return domain.Request{}, e
	}
	if input.Type == "agent" {
		if _, e = s.Catalog.FindObject(ctx, input.Object.Kind, input.Object.Code); e != nil {
			return domain.Request{}, e
		}
		if _, e = s.Catalog.GetEnvironment(ctx, input.EnvCode); e != nil {
			return domain.Request{}, e
		}
		if s.ResolveSkills == nil {
			return domain.Request{}, domain.ErrUnavailable
		}
		input.Skills, e = s.ResolveSkills(ctx, input.Skills)
		if e != nil {
			return domain.Request{}, e
		}
		for _, op := range input.Operations {
			if op.Name == "db-proxy.deploy" {
				code, _ := op.Parameters["targetDbCode"].(string)
				db, e := s.Catalog.FindObject(ctx, "database", code)
				if e != nil {
					return domain.Request{}, domain.ErrInvalid
				}
				items, e := s.Catalog.Deployments(ctx, db.ID, input.EnvCode)
				if e != nil {
					return domain.Request{}, e
				}
				if len(items) == 0 {
					return domain.Request{}, domain.ErrInvalid
				}
			}
		}
		applyDefaults(&input)
	} else {
		for _, target := range input.Targets {
			o, e := s.Catalog.FindObject(ctx, "application", target.AppCode)
			if e != nil {
				return domain.Request{}, e
			}
			v, e := s.Catalog.GetEnvironment(ctx, target.EnvCode)
			if e != nil {
				return domain.Request{}, e
			}
			if v.Config["ackClusterId"] != target.ClusterID {
				return domain.Request{}, domain.ErrInvalid
			}
			items, e := s.Catalog.Deployments(ctx, o.ID, target.EnvCode)
			if e != nil {
				return domain.Request{}, e
			}
			matched := false
			for _, d := range items {
				if d.ClusterID == target.ClusterID && d.Namespace == target.Namespace && d.Name == target.Name && d.UID == target.UID {
					matched = true
				}
			}
			if !matched {
				return domain.Request{}, domain.ErrInvalid
			}
		}
	}
	body, _ := json.Marshal(input)
	r := domain.Request{ID: domain.NewID(), CreatorID: u.ID, Definition: input, SchemaHash: schemaHash(), Status: "queued"}
	tx, e := s.Pool.Begin(ctx)
	if e != nil {
		return r, domain.ErrUnavailable
	}
	defer tx.Rollback(ctx)
	tag, e := tx.Exec(ctx, "INSERT INTO raptor.requests(id,creator_id,idempotency_key,input_hash,definition,schema_hash) VALUES($1,$2,$3,$4,$5,$6) ON CONFLICT(creator_id,idempotency_key) DO NOTHING", r.ID, u.ID, key, hash, body, r.SchemaHash)
	if e != nil {
		return r, domain.ErrUnavailable
	}
	if tag.RowsAffected() == 0 {
		if e = tx.QueryRow(ctx, "SELECT id,input_hash FROM raptor.requests WHERE creator_id=$1 AND idempotency_key=$2", u.ID, key).Scan(&existingID, &existingHash); e != nil {
			return r, domain.ErrUnavailable
		}
		if existingHash != hash {
			return r, domain.ErrConflict
		}
		if e = tx.Commit(ctx); e != nil {
			return r, domain.ErrUnavailable
		}
		return s.Get(ctx, existingID)
	}
	if input.Type == "agent" {
		_, e = tx.Exec(ctx, "INSERT INTO raptor.outbox(id,topic,entity_id,payload) VALUES($1,'dispatch',$2,'{}')", domain.NewID(), r.ID)
	} else {
		for _, t := range input.Targets {
			b, _ := json.Marshal(t)
			_, e = tx.Exec(ctx, "INSERT INTO raptor.targets(request_id,target_key,target) VALUES($1,$2,$3)", r.ID, TargetKey(t), b)
			if e != nil {
				break
			}
		}
	}
	if e != nil {
		return r, domain.ErrUnavailable
	}
	if e = tx.Commit(ctx); e != nil {
		return r, domain.ErrUnavailable
	}
	return s.Get(ctx, r.ID)
}
