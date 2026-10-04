package catalog

import (
	"context"
	"encoding/json"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/adapters"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/domain"
	"regexp"
	"strings"
)

type Service struct {
	Pool  *pgxpool.Pool
	Infra adapters.InfraReader
}
type CreateObjectInput struct {
	Kind        string `json:"kind"`
	Name        string `json:"name"`
	Description string `json:"description"`
}
type UpdateObjectInput struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

func NewService(p *pgxpool.Pool, i adapters.InfraReader) *Service { return &Service{p, i} }
func storeError(e error) error {
	if e == nil {
		return nil
	}
	if e == pgx.ErrNoRows {
		return domain.ErrNotFound
	}
	if pe, ok := e.(*pgconn.PgError); ok && (pe.Code == "23505" || pe.Code == "23503") {
		return domain.ErrConflict
	}
	return domain.ErrUnavailable
}
func validName(s string) bool { return strings.TrimSpace(s) != "" && len(s) <= 200 }
func (s *Service) CreateObject(ctx context.Context, in CreateObjectInput) (domain.Object, error) {
	if (in.Kind != "application" && in.Kind != "database" && in.Kind != "db-proxy") || !validName(in.Name) || len(in.Description) > 4000 {
		return domain.Object{}, domain.ErrInvalid
	}
	o := domain.Object{ID: domain.NewID(), Kind: in.Kind, Code: domain.NewID(), Name: in.Name, Description: in.Description}
	_, e := s.Pool.Exec(ctx, "INSERT INTO raptor.objects(id,kind,code,name,description) VALUES($1,$2,$3,$4,$5)", o.ID, o.Kind, o.Code, o.Name, o.Description)
	return o, storeError(e)
}
func (s *Service) UpdateObject(ctx context.Context, id string, in UpdateObjectInput) (domain.Object, error) {
	if !validName(in.Name) || len(in.Description) > 4000 {
		return domain.Object{}, domain.ErrInvalid
	}
	tag, e := s.Pool.Exec(ctx, "UPDATE raptor.objects SET name=$2,description=$3 WHERE id=$1", id, in.Name, in.Description)
	if e != nil {
		return domain.Object{}, storeError(e)
	}
	if tag.RowsAffected() == 0 {
		return domain.Object{}, domain.ErrNotFound
	}
	return s.GetObject(ctx, id)
}
func (s *Service) GetObject(ctx context.Context, id string) (domain.Object, error) {
	var o domain.Object
	e := s.Pool.QueryRow(ctx, "SELECT id,kind,code,name,description FROM raptor.objects WHERE id=$1", id).Scan(&o.ID, &o.Kind, &o.Code, &o.Name, &o.Description)
	return o, storeError(e)
}
func (s *Service) FindObject(ctx context.Context, kind, code string) (domain.Object, error) {
	var o domain.Object
	e := s.Pool.QueryRow(ctx, "SELECT id,kind,code,name,description FROM raptor.objects WHERE kind=$1 AND code=$2", kind, code).Scan(&o.ID, &o.Kind, &o.Code, &o.Name, &o.Description)
	return o, storeError(e)
}
func (s *Service) ListObjects(ctx context.Context) ([]domain.Object, error) {
	rows, e := s.Pool.Query(ctx, "SELECT id,kind,code,name,description FROM raptor.objects ORDER BY name,id")
	if e != nil {
		return nil, storeError(e)
	}
	defer rows.Close()
	out := []domain.Object{}
	for rows.Next() {
		var o domain.Object
		if e = rows.Scan(&o.ID, &o.Kind, &o.Code, &o.Name, &o.Description); e != nil {
			return nil, storeError(e)
		}
		out = append(out, o)
	}
	return out, storeError(rows.Err())
}

var codePattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]{0,79}$`)

func (s *Service) CreateGroup(ctx context.Context, code, name string) error {
	if !codePattern.MatchString(code) || !validName(name) {
		return domain.ErrInvalid
	}
	_, e := s.Pool.Exec(ctx, "INSERT INTO raptor.environment_groups(code,name) VALUES($1,$2)", code, name)
	return storeError(e)
}
func (s *Service) ListGroups(ctx context.Context) ([]map[string]string, error) {
	rows, e := s.Pool.Query(ctx, "SELECT code,name FROM raptor.environment_groups ORDER BY code")
	if e != nil {
		return nil, storeError(e)
	}
	defer rows.Close()
	out := []map[string]string{}
	for rows.Next() {
		var code, name string
		if e = rows.Scan(&code, &name); e != nil {
			return nil, storeError(e)
		}
		out = append(out, map[string]string{"code": code, "name": name})
	}
	return out, storeError(rows.Err())
}
func validConfig(v any) bool {
	switch x := v.(type) {
	case map[string]any:
		for k, val := range x {
			n := strings.ToLower(k)
			if n == "password" || n == "token" || n == "accesskey" || n == "accesskeysecret" || n == "apikey" || n == "authorization" || n == "secret" {
				return false
			}
			if !validConfig(val) {
				return false
			}
		}
	case []any:
		for _, val := range x {
			if !validConfig(val) {
				return false
			}
		}
	}
	return true
}
func (s *Service) SaveEnvironment(ctx context.Context, v domain.Environment, update bool) error {
	if !codePattern.MatchString(v.Code) || !validName(v.Stage) || v.GroupCode == "" || v.Config == nil || !validConfig(v.Config) {
		return domain.ErrInvalid
	}
	b, e := json.Marshal(v.Config)
	if e != nil || len(b) > 16384 {
		return domain.ErrInvalid
	}
	if update {
		tag, e := s.Pool.Exec(ctx, "UPDATE raptor.environments SET group_code=$2,stage=$3,config=$4 WHERE code=$1", v.Code, v.GroupCode, v.Stage, b)
		if e != nil {
			return storeError(e)
		}
		if tag.RowsAffected() == 0 {
			return domain.ErrNotFound
		}
		return nil
	}
	_, e = s.Pool.Exec(ctx, "INSERT INTO raptor.environments(code,group_code,stage,config) VALUES($1,$2,$3,$4)", v.Code, v.GroupCode, v.Stage, b)
	return storeError(e)
}
func (s *Service) GetEnvironment(ctx context.Context, code string) (domain.Environment, error) {
	var v domain.Environment
	var b []byte
	e := s.Pool.QueryRow(ctx, "SELECT code,group_code,stage,config FROM raptor.environments WHERE code=$1", code).Scan(&v.Code, &v.GroupCode, &v.Stage, &b)
	if e != nil {
		return v, storeError(e)
	}
	e = json.Unmarshal(b, &v.Config)
	return v, storeError(e)
}
func (s *Service) ListEnvironments(ctx context.Context) ([]domain.Environment, error) {
	rows, e := s.Pool.Query(ctx, "SELECT code,group_code,stage,config FROM raptor.environments ORDER BY group_code,code")
	if e != nil {
		return nil, storeError(e)
	}
	defer rows.Close()
	out := []domain.Environment{}
	for rows.Next() {
		var v domain.Environment
		var b []byte
		if e = rows.Scan(&v.Code, &v.GroupCode, &v.Stage, &b); e != nil {
			return nil, storeError(e)
		}
		if e = json.Unmarshal(b, &v.Config); e != nil {
			return nil, domain.ErrUnavailable
		}
		out = append(out, v)
	}
	return out, storeError(rows.Err())
}
func (s *Service) Deployments(ctx context.Context, id, env string) ([]adapters.Deployment, error) {
	o, e := s.GetObject(ctx, id)
	if e != nil {
		return nil, e
	}
	v, e := s.GetEnvironment(ctx, env)
	if e != nil {
		return nil, e
	}
	if s.Infra == nil {
		return nil, domain.ErrUnavailable
	}
	out, e := s.Infra.ListDeployments(ctx, v, o)
	if e != nil {
		return nil, domain.ErrUnavailable
	}
	if out == nil {
		out = []adapters.Deployment{}
	}
	for _, d := range out {
		if d.EnvCode != env || d.ObjectCode != o.Code || d.Kind != o.Kind {
			return nil, domain.ErrConflict
		}
	}
	return out, nil
}
func ValidateSameEnvironment(a, b string) error {
	if a == "" || a != b {
		return domain.ErrInvalid
	}
	return nil
}
