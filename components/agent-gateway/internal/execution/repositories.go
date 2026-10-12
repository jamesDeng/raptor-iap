package execution

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"strings"
	"time"
)

type RepositorySnapshot struct {
	EnvironmentCode string `json:"environmentCode"`
	Purpose         string `json:"purpose"`
	URL             string `json:"url"`
	Directory       string `json:"directory"`
	BaseBranch      string `json:"baseBranch"`
	BaseCommit      string `json:"baseCommit"`
}
type RepositoryPreparationError struct{ URL string }

func (e RepositoryPreparationError) Error() string               { return "repository unavailable: " + e.URL }
func (e RepositoryPreparationError) PublicRepositoryURL() string { return e.URL }

type BaseCommitResolver interface {
	Resolve(context.Context, string, string) (string, error)
}

type GitHubBranchResolver struct{ Client *http.Client }

func NewGitHubBranchResolver() GitHubBranchResolver {
	return GitHubBranchResolver{Client: &http.Client{Timeout: 15 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
}
func (g GitHubBranchResolver) Resolve(ctx context.Context, repoURL, branch string) (string, error) {
	if g.Client == nil || !validPublicRepo(repoURL) || !validBaseBranch(branch) {
		return "", ErrInvalid
	}
	u, _ := url.Parse(repoURL)
	parts := strings.Split(strings.TrimPrefix(strings.TrimSuffix(u.Path, ".git"), "/"), "/")
	endpoint := "https://api.github.com/repos/" + parts[0] + "/" + parts[1] + "/git/ref/heads/" + url.PathEscape(branch)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return "", ErrUnavailable
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	res, err := g.Client.Do(req)
	if err != nil {
		return "", ErrUnavailable
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return "", ErrUnavailable
	}
	var ref struct {
		Ref    string `json:"ref"`
		Object struct {
			Type string `json:"type"`
			SHA  string `json:"sha"`
		} `json:"object"`
	}
	if json.NewDecoder(io.LimitReader(res.Body, 16384)).Decode(&ref) != nil || ref.Ref != "refs/heads/"+branch || ref.Object.Type != "commit" || !commitPattern.MatchString(ref.Object.SHA) {
		return "", ErrUnavailable
	}
	return ref.Object.SHA, nil
}

var githubPath = regexp.MustCompile(`^/[A-Za-z0-9][A-Za-z0-9-]*/[A-Za-z0-9][A-Za-z0-9_.-]*(\.git)?$`)
var gitBranch = regexp.MustCompile(`^[A-Za-z0-9._/-]{1,200}$`)

func validPublicRepo(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && u.Scheme == "https" && u.Host == "github.com" && u.User == nil && u.RawQuery == "" && u.Fragment == "" && raw == "https://github.com"+u.Path && githubPath.MatchString(u.Path) && !strings.Contains(u.Path, "..")
}
func validBaseBranch(branch string) bool {
	if !gitBranch.MatchString(branch) || strings.HasPrefix(branch, "-") || strings.HasPrefix(branch, "/") || strings.HasSuffix(branch, "/") || strings.HasSuffix(branch, ".") || strings.Contains(branch, "..") || strings.Contains(branch, "@{") {
		return false
	}
	for _, part := range strings.Split(branch, "/") {
		if part == "" || part == "." || strings.HasSuffix(part, ".lock") {
			return false
		}
	}
	return true
}
func validRepoDirectory(dir string) bool {
	return dir != "" && !path.IsAbs(dir) && path.Clean(dir) == dir && dir != ".." && !strings.HasPrefix(dir, "../") && !strings.Contains(dir, "\\")
}

func BuildRepositorySnapshot(ctx context.Context, raw json.RawMessage, code string, resolver BaseCommitResolver) ([]RepositorySnapshot, error) {
	if resolver == nil {
		return nil, ErrInvalid
	}
	var env struct {
		Code         string `json:"code"`
		Repositories []struct {
			Purpose    string `json:"purpose"`
			URL        string `json:"url"`
			BaseBranch string `json:"baseBranch"`
			Directory  string `json:"directory"`
		} `json:"repositories"`
	}
	if json.Unmarshal(raw, &env) != nil || env.Code != code || len(env.Repositories) == 0 || len(env.Repositories) > 32 {
		return nil, ErrInvalid
	}
	commits := map[string]string{}
	branches := map[string]string{}
	seen := map[string]bool{}
	result := make([]RepositorySnapshot, 0, len(env.Repositories))
	for _, r := range env.Repositories {
		if (r.Purpose != "terraform" && r.Purpose != "kubernetes") || !validPublicRepo(r.URL) || !validBaseBranch(r.BaseBranch) || !validRepoDirectory(r.Directory) {
			return nil, ErrInvalid
		}
		identity := r.Purpose + "\x00" + r.URL + "\x00" + r.Directory
		if seen[identity] || (branches[r.URL] != "" && branches[r.URL] != r.BaseBranch) {
			return nil, ErrInvalid
		}
		seen[identity] = true
		branches[r.URL] = r.BaseBranch
		commit := commits[r.URL]
		if commit == "" {
			var err error
			commit, err = resolver.Resolve(ctx, r.URL, r.BaseBranch)
			if err != nil || !commitPattern.MatchString(commit) {
				return nil, RepositoryPreparationError{URL: r.URL}
			}
			commits[r.URL] = commit
		}
		result = append(result, RepositorySnapshot{EnvironmentCode: code, Purpose: r.Purpose, URL: r.URL, Directory: r.Directory, BaseBranch: r.BaseBranch, BaseCommit: commit})
	}
	return result, nil
}

func (s *Store) SaveRepositorySnapshot(ctx context.Context, attempt, owner string, rows []RepositorySnapshot) error {
	if len(rows) == 0 || len(rows) > 32 {
		return ErrInvalid
	}
	tx, _, err := s.liveTx(ctx, attempt, owner)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var existing int
	if err = tx.QueryRow(ctx, "SELECT count(*) FROM gateway.attempt_repositories WHERE attempt_id=$1", attempt).Scan(&existing); err != nil {
		return err
	}
	if existing != 0 {
		stored, readErr := tx.Query(ctx, "SELECT environment_code,purpose,repository_url,directory,base_branch,base_commit FROM gateway.attempt_repositories WHERE attempt_id=$1 ORDER BY row_index", attempt)
		if readErr != nil {
			return readErr
		}
		match := existing == len(rows)
		i := 0
		for stored.Next() {
			var r RepositorySnapshot
			if readErr = stored.Scan(&r.EnvironmentCode, &r.Purpose, &r.URL, &r.Directory, &r.BaseBranch, &r.BaseCommit); readErr != nil {
				break
			}
			if i >= len(rows) || r != rows[i] {
				match = false
			}
			i++
		}
		if readErr == nil {
			readErr = stored.Err()
		}
		stored.Close()
		if readErr != nil {
			return readErr
		}
		if !match || i != len(rows) {
			return ErrInvalid
		}
		return tx.Commit(ctx)
	}
	for i, r := range rows {
		if !journalName.MatchString(r.EnvironmentCode) || (r.Purpose != "terraform" && r.Purpose != "kubernetes") || !validPublicRepo(r.URL) || !validRepoDirectory(r.Directory) || !validBaseBranch(r.BaseBranch) || !commitPattern.MatchString(r.BaseCommit) {
			return ErrInvalid
		}
		if _, err = tx.Exec(ctx, "INSERT INTO gateway.attempt_repositories(attempt_id,row_index,environment_code,purpose,repository_url,directory,base_branch,base_commit) VALUES($1,$2,$3,$4,$5,$6,$7,$8)", attempt, i, r.EnvironmentCode, r.Purpose, r.URL, r.Directory, r.BaseBranch, r.BaseCommit); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func (s *Store) RepositorySnapshot(ctx context.Context, attempt string) ([]RepositorySnapshot, error) {
	rows, err := s.Pool.Query(ctx, "SELECT environment_code,purpose,repository_url,directory,base_branch,base_commit FROM gateway.attempt_repositories WHERE attempt_id=$1 ORDER BY row_index", attempt)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []RepositorySnapshot{}
	for rows.Next() {
		var r RepositorySnapshot
		if err = rows.Scan(&r.EnvironmentCode, &r.Purpose, &r.URL, &r.Directory, &r.BaseBranch, &r.BaseCommit); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *Store) BoundRepositorySnapshot(ctx context.Context, request, attempt string) ([]RepositorySnapshot, error) {
	var raw []byte
	if err := s.Pool.QueryRow(ctx, `SELECT a.binding FROM gateway.attempts a JOIN gateway.executions x ON x.request_id=a.request_id WHERE a.id=$1 AND a.request_id=$2 AND x.attempt_id=a.id AND a.ended_at IS NULL`, attempt, request).Scan(&raw); err != nil {
		return nil, ErrInvalid
	}
	var binding AttemptBinding
	if json.Unmarshal(raw, &binding) != nil || !binding.repositoryChangeValid() || binding.RequestID != request || binding.AttemptID != attempt {
		return nil, ErrInvalid
	}
	rows, err := s.RepositorySnapshot(ctx, attempt)
	if err != nil || len(rows) == 0 {
		return nil, ErrInvalid
	}
	return rows, nil
}
