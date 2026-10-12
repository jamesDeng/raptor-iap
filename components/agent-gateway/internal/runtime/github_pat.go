package runtime

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

type PATClient interface {
	Load(context.Context) ([]byte, error)
}
type PrivatePATClient struct {
	BaseURL, Username, Password string
	AllowClusterHTTP            bool
}

var githubPATPattern = regexp.MustCompile(`^[A-Za-z0-9_]+$`)

func validGitHubPAT(token []byte) bool {
	return len(token) >= 16 && len(token) <= 1024 && githubPATPattern.Match(token)
}

func (c PrivatePATClient) Load(ctx context.Context) ([]byte, error) {
	u, err := url.Parse(c.BaseURL)
	if err != nil || u.Host == "" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" || c.Username == "" || c.Password == "" || (u.Scheme != "https" && !(u.Scheme == "http" && (u.Hostname() == "127.0.0.1" || u.Hostname() == "localhost" || (c.AllowClusterHTTP && u.Hostname() == "raptor-backend")))) {
		return nil, ErrConfiguration
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(c.BaseURL, "/")+"/v1/private/github-pat", nil)
	if err != nil {
		return nil, ErrConfiguration
	}
	req.SetBasicAuth(c.Username, c.Password)
	client := &http.Client{Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	res, err := client.Do(req)
	if err != nil {
		return nil, ErrRuntimeUnavailable
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, ErrRuntimeUnavailable
	}
	var envelope struct {
		Data struct {
			Token string `json:"token"`
		} `json:"data"`
	}
	if json.NewDecoder(io.LimitReader(res.Body, 4096)).Decode(&envelope) != nil || !validGitHubPAT([]byte(envelope.Data.Token)) {
		return nil, ErrRuntimeUnavailable
	}
	return []byte(envelope.Data.Token), nil
}

func privatePATFiles(token []byte) (map[string][]byte, error) {
	if !validGitHubPAT(token) {
		return nil, ErrConfiguration
	}
	files := map[string][]byte{
		"/tmp/raptor-private/github-pat":            append([]byte(nil), token...),
		"/tmp/raptor-private/git-credential-helper": []byte("#!/bin/sh\nset -eu\nprintf 'username=x-access-token\\npassword='\ncat /tmp/raptor-private/github-pat\nprintf '\\n'\n"),
		"/tmp/raptor-private/gitconfig":             []byte("[credential \"https://github.com\"]\n\thelper = \"!sh /tmp/raptor-private/git-credential-helper\"\n"),
		"/tmp/raptor-private/gh/hosts.yml":          []byte("github.com:\n  oauth_token: " + string(token) + "\n  git_protocol: https\n"),
	}
	return files, nil
}
