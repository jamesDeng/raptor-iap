package github

import (
	"context"
	"encoding/json"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/db"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/domain"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/requests"
	"github.com/jamesDeng/raptor-iap/components/raptor/internal/testutil"
	"io"
	"net/http"
	"strings"
	"testing"
)

type snapshotFixture []requests.RepositorySnapshot

func (f snapshotFixture) RepositorySnapshot(_ context.Context, _, _ string) ([]requests.RepositorySnapshot, error) {
	return f, nil
}

type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func jsonClient(t *testing.T, body string) *http.Client {
	return &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host != "api.github.com" {
			t.Fatal("unexpected host", r.URL.Host)
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: http.Header{"Content-Type": []string{"application/json"}}}, nil
	})}
}

func TestBoundPRAssociationUsesAttemptSnapshotAndLiveGitHubIdentity(t *testing.T) {
	ctx := context.Background()
	p := testutil.Database(t)
	if err := db.Migrate(ctx, p); err != nil {
		t.Fatal(err)
	}
	id := domain.NewID()
	attempt := domain.NewID()
	base := strings.Repeat("a", 40)
	head := strings.Repeat("b", 40)
	p.Exec(ctx, `INSERT INTO raptor.requests(id,creator_id,idempotency_key,input_hash,definition,schema_hash,status) VALUES($1,$2,'bound-pr','hash','{}','schema','running')`, id, domain.NewID())
	service := &Service{Pool: p, Snapshots: snapshotFixture{{URL: "https://github.com/owner/repo.git", BaseBranch: "main", BaseCommit: base}}, Client: jsonClient(t, `{"html_url":"https://github.com/owner/repo/pull/7","base":{"ref":"main"},"head":{"ref":"raptor/change","sha":"`+head+`","repo":{"full_name":"owner/repo"}}}`)}
	in := BoundPRInput{PRInput: PRInput{Repository: "owner/repo", Number: 7, URL: "https://github.com/owner/repo/pull/7", HeadSHA: head}, AttemptID: attempt, BaseCommit: base, BaseBranch: "main", WorkingBranch: "raptor/change"}
	if err := service.AttachBoundPR(ctx, id, in); err != nil {
		t.Fatal(err)
	}
	if err := service.AttachBoundPR(ctx, id, in); err != nil {
		t.Fatal("retry must be idempotent", err)
	}
	var gotAttempt, gotBase string
	if err := p.QueryRow(ctx, `SELECT attempt_id::text,base_commit FROM raptor.pull_requests WHERE repository='owner/repo' AND number=7`).Scan(&gotAttempt, &gotBase); err != nil || gotAttempt != attempt || gotBase != base {
		t.Fatal(gotAttempt, gotBase, err)
	}
	wrong := in
	wrong.Repository = "other/repo"
	wrong.URL = "https://github.com/other/repo/pull/7"
	if service.AttachBoundPR(ctx, id, wrong) == nil {
		t.Fatal("unconfigured repository accepted")
	}
	wrong = in
	wrong.AttemptID = domain.NewID()
	if service.AttachBoundPR(ctx, id, wrong) == nil {
		t.Fatal("different attempt accepted")
	}
	wrong = in
	wrong.HeadSHA = strings.Repeat("c", 40)
	if service.AttachBoundPR(ctx, id, wrong) == nil {
		t.Fatal("wrong GitHub head accepted")
	}
	var count int
	if err := p.QueryRow(ctx, `SELECT count(*) FROM raptor.pull_requests WHERE request_id=$1`, id).Scan(&count); err != nil || count != 1 {
		t.Fatal(count, err)
	}
}

func TestActionStateAggregatesOnlyMatchingHead(t *testing.T) {
	sha := strings.Repeat("d", 40)
	other := strings.Repeat("e", 40)
	for _, tc := range []struct{ body, want string }{
		{`{"workflow_runs":[]}`, "unavailable"},
		{`{"workflow_runs":[{"head_sha":"` + sha + `","status":"queued","conclusion":null}]}`, "pending"},
		{`{"workflow_runs":[{"head_sha":"` + sha + `","status":"completed","conclusion":"success"}]}`, "success"},
		{`{"workflow_runs":[{"head_sha":"` + sha + `","status":"completed","conclusion":"success"},{"head_sha":"` + sha + `","status":"completed","conclusion":"failure"}]}`, "failure"},
		{`{"workflow_runs":[{"head_sha":"` + other + `","status":"completed","conclusion":"failure"}]}`, "unavailable"},
	} {
		s := &Service{Client: jsonClient(t, tc.body)}
		if got := s.actionState(context.Background(), "owner/repo", sha); got != tc.want {
			t.Fatalf("%s != %s", got, tc.want)
		}
	}
	var v ActionsState
	if json.Unmarshal([]byte(`{"state":"success"}`), &v) != nil || v.State != "success" {
		t.Fatal("state wire contract")
	}
}
