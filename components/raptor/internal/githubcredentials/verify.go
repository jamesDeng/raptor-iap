package githubcredentials

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"
)

// VerifyGitHubPAT checks the account identity through GitHub's fixed API origin.
// API response bodies are never included in errors.
func VerifyGitHubPAT(client *http.Client) func(context.Context, string) (string, error) {
	return func(ctx context.Context, token string) (string, error) {
		if client == nil {
			return "", ErrUnavailable
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.github.com/user", nil)
		if err != nil {
			return "", ErrUnavailable
		}
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Accept", "application/vnd.github+json")
		req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
		res, err := client.Do(req)
		if err != nil {
			return "", ErrUnavailable
		}
		defer res.Body.Close()
		if res.StatusCode != http.StatusOK {
			return "", ErrInvalid
		}
		var user struct {
			Login string `json:"login"`
		}
		if json.NewDecoder(io.LimitReader(res.Body, 4096)).Decode(&user) != nil || user.Login == "" {
			return "", errors.New("GitHub identity unavailable")
		}
		return user.Login, nil
	}
}

func GitHubHTTPClient() *http.Client {
	return &http.Client{Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}
