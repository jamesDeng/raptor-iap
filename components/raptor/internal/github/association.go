package github

import (
	"context"
	"fmt"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/domain"
	"regexp"
	"strings"
)

type Service struct{ Pool *pgxpool.Pool }
type PRInput struct {
	Repository string `json:"repository"`
	Number     int    `json:"number"`
	URL        string `json:"url"`
	HeadSHA    string `json:"headSha"`
}

var repoPattern = regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`)
var shaPattern = regexp.MustCompile(`^[a-f0-9]{40}$`)

func (s *Service) AttachPR(ctx context.Context, id string, in PRInput) error {
	if !repoPattern.MatchString(in.Repository) || in.Number < 1 || !shaPattern.MatchString(in.HeadSHA) {
		return domain.ErrInvalid
	}
	repo := strings.ToLower(in.Repository)
	if !strings.EqualFold(in.URL, fmt.Sprintf("https://github.com/%s/pull/%d", repo, in.Number)) {
		return domain.ErrInvalid
	}
	tx, e := s.Pool.Begin(ctx)
	if e != nil {
		return domain.ErrUnavailable
	}
	defer tx.Rollback(ctx)
	var status string
	if e = tx.QueryRow(ctx, "SELECT status FROM raptor.requests WHERE id=$1 FOR UPDATE", id).Scan(&status); e != nil {
		return domain.ErrNotFound
	}
	if status == "cancelled" || status == "completed" {
		return domain.ErrConflict
	}
	if _, e = tx.Exec(ctx, "INSERT INTO raptor.pull_requests(repository,number,request_id,url,head_sha) VALUES($1,$2,$3,$4,$5) ON CONFLICT DO NOTHING", repo, in.Number, id, fmt.Sprintf("https://github.com/%s/pull/%d", repo, in.Number), in.HeadSHA); e != nil {
		return domain.ErrUnavailable
	}
	var linked, head string
	if e = tx.QueryRow(ctx, "SELECT request_id::text,head_sha FROM raptor.pull_requests WHERE repository=$1 AND number=$2", repo, in.Number).Scan(&linked, &head); e != nil {
		return domain.ErrUnavailable
	}
	if linked != id {
		return domain.ErrConflict
	}
	if head != in.HeadSHA {
		if _, e = tx.Exec(ctx, "UPDATE raptor.pull_requests SET head_sha=$3 WHERE repository=$1 AND number=$2", repo, in.Number, in.HeadSHA); e != nil {
			return domain.ErrUnavailable
		}
	}
	return tx.Commit(ctx)
}
