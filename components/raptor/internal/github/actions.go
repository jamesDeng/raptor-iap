package github

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/domain"
	"io"
	"net/http"
	"net/url"
	"time"
)

type ActionsState struct {
	Repository string    `json:"repository"`
	Number     int       `json:"number"`
	URL        string    `json:"url"`
	HeadSHA    string    `json:"headSha"`
	State      string    `json:"state"`
	CheckedAt  time.Time `json:"checkedAt"`
}

func (s *Service) PRActionStates(ctx context.Context, request string) ([]ActionsState, error) {
	rows, err := s.Pool.Query(ctx, `SELECT repository,number,url,head_sha FROM raptor.pull_requests WHERE request_id=$1 AND attempt_id IS NOT NULL ORDER BY repository,number`, request)
	if err != nil {
		return nil, domain.ErrUnavailable
	}
	defer rows.Close()
	out := []ActionsState{}
	for rows.Next() {
		var v ActionsState
		if rows.Scan(&v.Repository, &v.Number, &v.URL, &v.HeadSHA) != nil {
			return nil, domain.ErrUnavailable
		}
		v.State = s.actionState(ctx, v.Repository, v.HeadSHA)
		v.CheckedAt = time.Now().UTC()
		out = append(out, v)
	}
	if rows.Err() != nil {
		return nil, domain.ErrUnavailable
	}
	return out, nil
}

func (s *Service) actionState(ctx context.Context, repository, sha string) string {
	if !repoPattern.MatchString(repository) || !shaPattern.MatchString(sha) {
		return "unavailable"
	}
	client := s.Client
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	}
	endpoint := fmt.Sprintf("https://api.github.com/repos/%s/actions/runs?head_sha=%s&per_page=100", repository, url.QueryEscape(sha))
	req, err := http.NewRequestWithContext(ctx, "GET", endpoint, nil)
	if err != nil {
		return "unavailable"
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := client.Do(req)
	if err != nil {
		return "unavailable"
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return "unavailable"
	}
	var body struct {
		Runs []struct {
			HeadSHA    string  `json:"head_sha"`
			Status     string  `json:"status"`
			Conclusion *string `json:"conclusion"`
		} `json:"workflow_runs"`
	}
	if json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&body) != nil {
		return "unavailable"
	}
	seen, pending, failed := false, false, false
	for _, run := range body.Runs {
		if run.HeadSHA != sha {
			continue
		}
		seen = true
		if run.Status != "completed" || run.Conclusion == nil {
			pending = true
			continue
		}
		switch *run.Conclusion {
		case "success", "neutral", "skipped":
		case "failure", "timed_out", "cancelled", "action_required", "startup_failure":
			failed = true
		default:
			pending = true
		}
	}
	if !seen {
		return "unavailable"
	}
	if failed {
		return "failure"
	}
	if pending {
		return "pending"
	}
	return "success"
}
