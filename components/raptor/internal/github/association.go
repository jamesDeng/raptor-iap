package github

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/domain"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/requests"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"
)

type SnapshotReader interface {
	RepositorySnapshot(context.Context, string, string) ([]requests.RepositorySnapshot, error)
}
type TokenReader interface {
	Load(context.Context) ([]byte, error)
}
type Service struct {
	Pool      *pgxpool.Pool
	Snapshots SnapshotReader
	Client    *http.Client
	Tokens    TokenReader
}

func (s *Service) authorize(ctx context.Context, req *http.Request) error {
	if s.Tokens == nil {
		return nil
	}
	token, err := s.Tokens.Load(ctx)
	if err != nil || len(token) == 0 {
		return domain.ErrUnavailable
	}
	req.Header.Set("Authorization", "Bearer "+string(token))
	for i := range token {
		token[i] = 0
	}
	return nil
}

type PRInput struct {
	Repository string `json:"repository"`
	Number     int    `json:"number"`
	URL        string `json:"url"`
	HeadSHA    string `json:"headSha"`
}
type BoundPRInput struct {
	PRInput
	AttemptID     string `json:"attemptId"`
	BaseCommit    string `json:"baseCommit"`
	BaseBranch    string `json:"baseBranch"`
	WorkingBranch string `json:"workingBranch"`
}

func (s *Service) AttachBoundPR(ctx context.Context, request string, in BoundPRInput) error {
	if s.Snapshots == nil || !repoPattern.MatchString(in.Repository) || !shaPattern.MatchString(in.HeadSHA) || !shaPattern.MatchString(in.BaseCommit) || in.Number < 1 || in.AttemptID == "" || in.BaseBranch == "" || in.WorkingBranch == "" || len(in.WorkingBranch) > 255 || in.WorkingBranch == in.BaseBranch {
		return domain.ErrInvalid
	}
	canonical := fmt.Sprintf("https://github.com/%s/pull/%d", strings.ToLower(in.Repository), in.Number)
	if !strings.EqualFold(in.URL, canonical) {
		return domain.ErrInvalid
	}
	rows, err := s.Snapshots.RepositorySnapshot(ctx, request, in.AttemptID)
	if err != nil {
		return domain.ErrUnavailable
	}
	matched := false
	for _, row := range rows {
		if strings.EqualFold(strings.TrimSuffix(row.URL, ".git"), "https://github.com/"+in.Repository) && row.BaseCommit == in.BaseCommit && row.BaseBranch == in.BaseBranch {
			matched = true
			break
		}
	}
	if !matched {
		return domain.ErrInvalid
	}
	client := s.Client
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	}
	req, err := http.NewRequestWithContext(ctx, "GET", "https://api.github.com/repos/"+in.Repository+fmt.Sprintf("/pulls/%d", in.Number), nil)
	if err != nil {
		return domain.ErrUnavailable
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	if err = s.authorize(ctx, req); err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return domain.ErrUnavailable
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return domain.ErrUnavailable
	}
	var live struct {
		HTMLURL string `json:"html_url"`
		Base    struct {
			Ref string `json:"ref"`
		} `json:"base"`
		Head struct {
			Ref  string `json:"ref"`
			SHA  string `json:"sha"`
			Repo struct {
				FullName string `json:"full_name"`
			} `json:"repo"`
		} `json:"head"`
	}
	if json.NewDecoder(io.LimitReader(resp.Body, 65536)).Decode(&live) != nil {
		return domain.ErrUnavailable
	}
	if !strings.EqualFold(live.HTMLURL, canonical) || live.Base.Ref != in.BaseBranch || live.Head.Ref != in.WorkingBranch || live.Head.SHA != in.HeadSHA || !strings.EqualFold(live.Head.Repo.FullName, in.Repository) {
		return domain.ErrInvalid
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return domain.ErrUnavailable
	}
	defer tx.Rollback(ctx)
	var status string
	if err = tx.QueryRow(ctx, `SELECT status FROM raptor.requests WHERE id=$1 FOR UPDATE`, request).Scan(&status); err != nil {
		return domain.ErrNotFound
	}
	if status == "cancelled" || status == "completed" {
		return domain.ErrConflict
	}
	repository := strings.ToLower(in.Repository)
	if _, err = tx.Exec(ctx, `INSERT INTO raptor.pull_requests(repository,number,request_id,url,head_sha,attempt_id,base_commit,base_branch,working_branch) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9) ON CONFLICT DO NOTHING`, repository, in.Number, request, canonical, in.HeadSHA, in.AttemptID, in.BaseCommit, in.BaseBranch, in.WorkingBranch); err != nil {
		return domain.ErrUnavailable
	}
	var linked, attempt, base, branch, working string
	if err = tx.QueryRow(ctx, `SELECT request_id::text,COALESCE(attempt_id::text,''),COALESCE(base_commit,''),COALESCE(base_branch,''),COALESCE(working_branch,'') FROM raptor.pull_requests WHERE repository=$1 AND number=$2 FOR UPDATE`, repository, in.Number).Scan(&linked, &attempt, &base, &branch, &working); err != nil {
		return domain.ErrUnavailable
	}
	if linked != request || attempt != in.AttemptID || base != in.BaseCommit || branch != in.BaseBranch || working != in.WorkingBranch {
		return domain.ErrConflict
	}
	if _, err = tx.Exec(ctx, `UPDATE raptor.pull_requests SET head_sha=$3 WHERE repository=$1 AND number=$2`, repository, in.Number, in.HeadSHA); err != nil {
		return domain.ErrUnavailable
	}
	if err = tx.Commit(ctx); err != nil {
		return domain.ErrUnavailable
	}
	return nil
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
