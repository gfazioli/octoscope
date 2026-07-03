package github

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/shurcooL/githubv4"
)

// newTestGQLClient returns a *Client whose GraphQL endpoint is redirected to
// a local httptest server that always replies with (status, body). It reuses
// rewriteHost (pr_files_test.go) so the production githubv4 URL builder runs
// while traffic lands on the test server. This is the reusable harness for
// unit-testing any GraphQL fetch path in this package.
func newTestGQLClient(t *testing.T, status int, body string) *Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)
	httpClient := &http.Client{Transport: &rewriteHost{host: srv.URL}}
	return &Client{
		gql:           githubv4.NewClient(httpClient),
		rest:          httpClient,
		authenticated: true,
	}
}

func TestFetchWatchedRepo(t *testing.T) {
	const happyBody = `{"data":{"repository":{
		"name":"bub\u001b[31mbletea\u001b[0m",
		"nameWithOwner":"charmbracelet/bubbletea",
		"url":"https://github.com/charmbracelet/bubbletea",
		"isPrivate":false,
		"pushedAt":"2026-01-02T03:04:05Z",
		"primaryLanguage":{"name":"Go","color":"#00ADD8"},
		"stargazerCount":1234,
		"forkCount":56,
		"issues":{"totalCount":7},
		"pullRequests":{"totalCount":3},
		"defaultBranchRef":{"target":{"statusCheckRollup":{"state":"SUCCESS"}}},
		"releases":{"nodes":[{"tagName":"v1.0.0","publishedAt":"2026-01-01T00:00:00Z"}]}
	}}}`

	c := newTestGQLClient(t, http.StatusOK, happyBody)
	got, err := c.FetchWatchedRepo(context.Background(), "charmbracelet", "bubbletea")
	if err != nil {
		t.Fatalf("FetchWatchedRepo err = %v", err)
	}
	// Sanitize stripped the ANSI escape from the GitHub-sourced name.
	if got.Name != "bubbletea" {
		t.Errorf("Name = %q, want %q (ANSI must be stripped at the boundary)", got.Name, "bubbletea")
	}
	if got.PrimaryLanguage != "Go" || got.LanguageColor != "#00ADD8" {
		t.Errorf("language = (%q,%q), want (Go,#00ADD8)", got.PrimaryLanguage, got.LanguageColor)
	}
	if got.Stars != 1234 || got.Forks != 56 || got.OpenIssues != 7 || got.OpenPRs != 3 {
		t.Errorf("counts = (%d,%d,%d,%d), want (1234,56,7,3)", got.Stars, got.Forks, got.OpenIssues, got.OpenPRs)
	}
	if got.CIState != "SUCCESS" {
		t.Errorf("CIState = %q, want SUCCESS", got.CIState)
	}
	if got.LatestReleaseTag != "v1.0.0" {
		t.Errorf("LatestReleaseTag = %q, want v1.0.0", got.LatestReleaseTag)
	}
	if got.PushedAt.IsZero() {
		t.Errorf("PushedAt should be parsed, got zero")
	}
}

func TestFetchWatchedRepoNullOptionalFields(t *testing.T) {
	const nullBody = `{"data":{"repository":{
		"name":"empty-repo",
		"nameWithOwner":"owner/empty-repo",
		"url":"https://github.com/owner/empty-repo",
		"isPrivate":false,
		"pushedAt":"2026-01-02T03:04:05Z",
		"primaryLanguage":null,
		"stargazerCount":0,
		"forkCount":0,
		"issues":{"totalCount":0},
		"pullRequests":{"totalCount":0},
		"defaultBranchRef":null,
		"releases":{"nodes":[]}
	}}}`

	c := newTestGQLClient(t, http.StatusOK, nullBody)
	got, err := c.FetchWatchedRepo(context.Background(), "owner", "empty-repo")
	if err != nil {
		t.Fatalf("FetchWatchedRepo err = %v (null optional fields must not error)", err)
	}
	if got.PrimaryLanguage != "" || got.LanguageColor != "" {
		t.Errorf("null primaryLanguage should yield empty strings, got (%q,%q)", got.PrimaryLanguage, got.LanguageColor)
	}
	if got.CIState != "" {
		t.Errorf("null defaultBranchRef should yield empty CIState, got %q", got.CIState)
	}
	if got.LatestReleaseTag != "" {
		t.Errorf("no releases should yield empty tag, got %q", got.LatestReleaseTag)
	}
	if got.Name != "empty-repo" {
		t.Errorf("Name = %q, want empty-repo", got.Name)
	}
}

func TestFetchWatchedRepoGraphQLError(t *testing.T) {
	const errBody = `{"errors":[{"message":"Could not resolve to a Repository with the name 'owner/nope'."}]}`
	c := newTestGQLClient(t, http.StatusOK, errBody)
	_, err := c.FetchWatchedRepo(context.Background(), "owner", "nope")
	if err == nil {
		t.Fatal("expected an error for a GraphQL errors response, got nil")
	}
	fe, ok := err.(*FetchError)
	if !ok {
		t.Fatalf("err type = %T, want *FetchError", err)
	}
	if fe.Reason != ReasonNotFound {
		t.Errorf("Reason = %d, want ReasonNotFound (drives the #37 skipped notice)", fe.Reason)
	}
}

// TestFetchWatchedReposSplitsSkippedFromTransient pins the #37
// contract: NOT_FOUND refs and malformed entries come back in the
// skipped list (input order), transient failures (5xx) are dropped
// silently for the refresh, and resolvable entries land in the Repo
// slice.
func TestFetchWatchedReposSplitsSkippedFromTransient(t *testing.T) {
	const okBody = `{"data":{"repository":{
		"name":"good","nameWithOwner":"owner/good",
		"url":"https://github.com/owner/good","isPrivate":false,
		"pushedAt":"2026-01-02T03:04:05Z","primaryLanguage":null,
		"stargazerCount":1,"forkCount":0,
		"issues":{"totalCount":0},"pullRequests":{"totalCount":0},
		"defaultBranchRef":null,"releases":{"nodes":[]}
	}}}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.Contains(string(body), `"gone"`):
			_, _ = io.WriteString(w, `{"errors":[{"message":"Could not resolve to a Repository with the name 'owner/gone'."}]}`)
		case strings.Contains(string(body), `"flaky"`):
			w.WriteHeader(http.StatusBadGateway)
			_, _ = io.WriteString(w, "502 Bad Gateway")
		default:
			_, _ = io.WriteString(w, okBody)
		}
	}))
	t.Cleanup(srv.Close)
	c := &Client{
		gql:           githubv4.NewClient(&http.Client{Transport: &rewriteHost{host: srv.URL}}),
		authenticated: true,
	}

	repos, skipped := c.FetchWatchedRepos(context.Background(),
		[]string{"owner/good", "owner/gone", "malformed-no-slash", "owner/flaky", "evil\x1b[2Jentry"})

	if len(repos) != 1 || repos[0].Name != "good" {
		t.Errorf("repos = %+v, want just owner/good", repos)
	}
	// The hostile ref comes back sanitized: config shape-checks
	// owner/name but doesn't strip escapes, and the UI renders these.
	want := []string{"owner/gone", "malformed-no-slash", "evilentry"}
	if !reflect.DeepEqual(skipped, want) {
		t.Errorf("skipped = %v, want %v (transient 502 must NOT be flagged; ANSI must be stripped)", skipped, want)
	}
}
