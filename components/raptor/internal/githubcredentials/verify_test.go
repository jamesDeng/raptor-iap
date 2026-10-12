package githubcredentials

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestVerifyGitHubPATUsesFixedOriginAndHidesToken(t *testing.T) {
	const token = "github_pat_private_fixture"
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.String() != "https://api.github.com/user" || r.Header.Get("Authorization") != "Bearer "+token {
			t.Fatalf("unexpected request: %s", r.URL)
		}
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"login":"pat-owner"}`))}, nil
	})}
	owner, err := VerifyGitHubPAT(client)(context.Background(), token)
	if err != nil || owner != "pat-owner" {
		t.Fatalf("verification: %q %v", owner, err)
	}
	client.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 401, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(token))}, nil
	})
	_, err = VerifyGitHubPAT(client)(context.Background(), token)
	if err == nil || strings.Contains(err.Error(), token) {
		t.Fatalf("token leaked in error: %v", err)
	}
}
