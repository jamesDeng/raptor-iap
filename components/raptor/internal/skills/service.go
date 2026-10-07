package skills

import (
	"context"
	"encoding/json"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/domain"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/requests"
)

type Service struct {
	Pool   *pgxpool.Pool
	Source ReleaseSource
}
type SkillsChangeInput struct {
	Tag       string `json:"tag"`
	CommitSHA string `json:"commitSha"`
	Strategy  string `json:"strategy"`
}

func (s *Service) Change(ctx context.Context, user domain.User, id string, in SkillsChangeInput) error {
	if user.ID == "" || (in.Strategy != "interrupt" && in.Strategy != "next-pause") {
		return domain.ErrInvalid
	}
	version, e := s.Validate(ctx, domain.SkillsVersion{Tag: in.Tag, CommitSHA: in.CommitSHA})
	if e != nil {
		return e
	}
	tx, e := s.Pool.Begin(ctx)
	if e != nil {
		return domain.ErrUnavailable
	}
	defer tx.Rollback(ctx)
	var body []byte
	var status string
	if e = tx.QueryRow(ctx, "SELECT definition,status FROM raptor.requests WHERE id=$1 FOR UPDATE", id).Scan(&body, &status); e != nil {
		return domain.ErrNotFound
	}
	if status == "completed" || status == "cancelled" {
		return domain.ErrConflict
	}
	var definition domain.RequestInput
	if json.Unmarshal(body, &definition) != nil || definition.Type != "agent" {
		return domain.ErrInvalid
	}
	for _, op := range definition.Operations {
		if op.Name == "application.question" {
			return domain.ErrInvalid
		}
	}
	old, _ := json.Marshal(definition.Skills)
	next, _ := json.Marshal(version)
	definition.Skills = version
	b, _ := json.Marshal(definition)
	if _, e = tx.Exec(ctx, "INSERT INTO raptor.skills_changes(request_id,actor_id,old_version,new_version,strategy) VALUES($1,$2,$3,$4,$5)", id, user.ID, old, next, in.Strategy); e != nil {
		return domain.ErrUnavailable
	}
	if _, e = tx.Exec(ctx, "UPDATE raptor.requests SET definition=$2 WHERE id=$1", id, b); e != nil {
		return domain.ErrUnavailable
	}
	if e = requests.QueueSignal(ctx, tx, id, "skills", map[string]any{"version": version, "strategy": in.Strategy}); e != nil {
		return domain.ErrUnavailable
	}
	return tx.Commit(ctx)
}
