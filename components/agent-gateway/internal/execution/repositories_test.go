package execution

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

type resolverFunc func(context.Context, string, string) (string, error)

func (f resolverFunc) Resolve(ctx context.Context, url, branch string) (string, error) {
	return f(ctx, url, branch)
}

func TestBuildRepositorySnapshotAllRowsOneResolutionPerURL(t *testing.T) {
	env := json.RawMessage(`{"code":"rdev.ali","repositories":[{"purpose":"terraform","url":"https://github.com/example/infra.git","baseBranch":"main","directory":"terraform"},{"purpose":"kubernetes","url":"https://github.com/example/infra.git","baseBranch":"main","directory":"kubernetes"},{"purpose":"kubernetes","url":"https://github.com/example/apps.git","baseBranch":"release","directory":"."}]}`)
	calls := map[string]int{}
	resolve := resolverFunc(func(_ context.Context, url, branch string) (string, error) {
		calls[url]++
		if branch == "release" {
			return "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", nil
		}
		return "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", nil
	})
	rows, err := BuildRepositorySnapshot(context.Background(), env, "rdev.ali", resolve)
	if err != nil || len(rows) != 3 || calls["https://github.com/example/infra.git"] != 1 || calls["https://github.com/example/apps.git"] != 1 {
		t.Fatalf("snapshot: %+v calls=%v err=%v", rows, calls, err)
	}
	if rows[0].BaseCommit != "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" || rows[1].BaseCommit != rows[0].BaseCommit || rows[2].BaseCommit != "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb" {
		t.Fatal("commit snapshot incorrect")
	}
}

func TestBuildRepositorySnapshotRejectsUnreachableAndUnsafeInput(t *testing.T) {
	base := `{"code":"rdev.ali","repositories":[{"purpose":"terraform","url":"https://github.com/example/infra.git","baseBranch":"main","directory":"."}]}`
	failed := resolverFunc(func(context.Context, string, string) (string, error) { return "", errors.New("unreachable") })
	if rows, err := BuildRepositorySnapshot(context.Background(), json.RawMessage(base), "rdev.ali", failed); err == nil || len(rows) != 0 {
		t.Fatal("unreachable repo accepted")
	}
	valid := resolverFunc(func(context.Context, string, string) (string, error) {
		return "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", nil
	})
	for _, env := range []string{`{"code":"other","repositories":[]}`, `{"code":"rdev.ali","repositories":[]}`, `{"code":"rdev.ali","repositories":[{"purpose":"terraform","url":"https://evil.example/repo","baseBranch":"main","directory":"."}]}`, `{"code":"rdev.ali","repositories":[{"purpose":"terraform","url":"https://github.com/example/infra.git","baseBranch":"../main","directory":"."}]}`, `{"code":"rdev.ali","repositories":[{"purpose":"terraform","url":"https://github.com/example/infra.git","baseBranch":"main","directory":"../outside"}]}`} {
		if _, err := BuildRepositorySnapshot(context.Background(), json.RawMessage(env), "rdev.ali", valid); err == nil {
			t.Fatalf("unsafe input accepted: %s", env)
		}
	}
}

func TestRepositorySnapshotPersistsAcrossEnvironmentEdit(t *testing.T) {
	ctx := context.Background()
	store := testStore(t)
	requestID := NewID()
	store.Receive(ctx, requestID)
	claim, err := store.ClaimLiveNext(ctx, "owner")
	if err != nil || claim == nil {
		t.Fatalf("claim: %v", err)
	}
	first := []RepositorySnapshot{{EnvironmentCode: "rdev.ali", Purpose: "terraform", URL: "https://github.com/example/infra.git", Directory: ".", BaseBranch: "main", BaseCommit: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}}
	if err := store.SaveRepositorySnapshot(ctx, claim.AttemptID, "owner", first); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveRepositorySnapshot(ctx, claim.AttemptID, "owner", first); err != nil {
		t.Fatalf("exact retry failed: %v", err)
	}
	second := []RepositorySnapshot{{EnvironmentCode: "rdev.ali", Purpose: "terraform", URL: "https://github.com/example/infra.git", Directory: ".", BaseBranch: "main", BaseCommit: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"}}
	if err := store.SaveRepositorySnapshot(ctx, claim.AttemptID, "owner", second); err == nil {
		t.Fatal("attempt snapshot changed")
	}
	got, err := store.RepositorySnapshot(ctx, claim.AttemptID)
	if err != nil || len(got) != 1 || got[0].BaseCommit != first[0].BaseCommit {
		t.Fatalf("snapshot mutated: %+v %v", got, err)
	}
}

type resolverTransport func(*http.Request) (*http.Response, error)

func (f resolverTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestGitHubBranchResolverFixedOriginAndCommit(t *testing.T) {
	client := &http.Client{Transport: resolverTransport(func(r *http.Request) (*http.Response, error) {
		if r.URL.Scheme != "https" || r.URL.Host != "api.github.com" || r.URL.Path != "/repos/example/infra/git/ref/heads/main" || r.Header.Get("Authorization") != "" {
			t.Fatalf("unsafe request: %s", r.URL)
		}
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"ref":"refs/heads/main","object":{"type":"commit","sha":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}}`))}, nil
	})}
	commit, err := (GitHubBranchResolver{Client: client}).Resolve(context.Background(), "https://github.com/example/infra.git", "main")
	if err != nil || commit != "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" {
		t.Fatalf("commit: %s %v", commit, err)
	}
	client.Transport = resolverTransport(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 404, Header: http.Header{}, Body: io.NopCloser(strings.NewReader("SECRET"))}, nil
	})
	_, err = (GitHubBranchResolver{Client: client}).Resolve(context.Background(), "https://github.com/example/infra.git", "main")
	if err == nil || strings.Contains(err.Error(), "SECRET") {
		t.Fatalf("response leaked: %v", err)
	}
}
