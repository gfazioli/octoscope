package github

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/shurcooL/githubv4"
)

// newRoutingGQLClient is the multi-query variant of newTestGQLClient
// (watched_repo_fetch_test.go): it dispatches each GraphQL request to a
// canned (status, body) chosen by inspecting the query text. FetchStats
// fires up to five distinct queries in parallel, so a single fixed body
// can't exercise it — this routes each branch to its own response.
// Hermetic: no network, no token.
func newRoutingGQLClient(t *testing.T, route func(query string) (int, string)) *Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		status, resp := route(string(body))
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, resp)
	}))
	t.Cleanup(srv.Close)
	httpClient := &http.Client{Transport: &rewriteHost{host: srv.URL}}
	return &Client{gql: githubv4.NewClient(httpClient), rest: httpClient, authenticated: true}
}

// statsRoutes serves a valid minimal response for every FetchStats
// branch, keyed off a marker unique to each query. watchFail makes the
// watched single-repo query 500 so the best-effort branch degrades
// instead of aborting the whole fetch.
func statsRoutes(watchFail bool) func(string) (int, string) {
	const rl = `"rateLimit":{"limit":5000,"remaining":4990,"resetAt":"2026-06-01T00:00:00Z"}`
	return func(q string) (int, string) {
		switch {
		case strings.Contains(q, "statusCheckRollup"):
			return 200, `{"data":{"viewer":{"repositories":{"nodes":[{"nameWithOwner":"octocat/proj"}],"pageInfo":{"hasNextPage":false}}},` + rl + `}}`
		case strings.Contains(q, "contributionsCollection"):
			return 200, `{"data":{"viewer":{"login":"octocat","name":"Octo Cat"},` + rl + `}}`
		case strings.Contains(q, "$reposCursor"), strings.Contains(q, "languages("):
			return 200, `{"data":{"viewer":{"repositories":{"totalCount":1,"nodes":[{"name":"proj","nameWithOwner":"octocat/proj","url":"https://github.com/octocat/proj","isPrivate":false,"stargazerCount":10}],"pageInfo":{"hasNextPage":false}}},` + rl + `}}`
		case strings.Contains(q, "search("):
			return 200, `{"data":{"search":{"nodes":[]}}}`
		case strings.Contains(q, "repository("):
			if watchFail {
				return 500, `{"errors":[{"message":"boom"}]}`
			}
			return 200, `{"data":{"repository":{"name":"bubbletea","nameWithOwner":"charmbracelet/bubbletea","url":"https://github.com/charmbracelet/bubbletea","isPrivate":false}}}`
		default:
			return 500, `{"errors":[{"message":"unrouted query"}]}`
		}
	}
}

// TestFetchStatsHappyPath drives the whole dashboard fetch through the
// routing harness: profile, repo list, CI rollup and the review-requests
// search all resolve, and FetchStats returns a renderable payload.
func TestFetchStatsHappyPath(t *testing.T) {
	c := newRoutingGQLClient(t, statsRoutes(false))

	stats, err := c.FetchStats(context.Background())
	if err != nil {
		t.Fatalf("FetchStats err = %v", err)
	}
	if stats == nil {
		t.Fatal("FetchStats returned nil stats")
	}
	if stats.Login != "octocat" {
		t.Errorf("Login = %q, want octocat", stats.Login)
	}
	if len(stats.Repositories) != 1 || stats.Repositories[0].Name != "proj" {
		t.Errorf("Repositories = %+v, want one repo named proj", stats.Repositories)
	}
}

// TestFetchStatsBestEffortBranchDegrades pins the resilience contract: a
// failing watched-repo branch (best-effort) must not abort the fetch —
// the mandatory branches still produce a renderable payload.
func TestFetchStatsBestEffortBranchDegrades(t *testing.T) {
	c := newRoutingGQLClient(t, statsRoutes(true))
	c.SetWatchRepos([]string{"charmbracelet/bubbletea"})

	stats, err := c.FetchStats(context.Background())
	if err != nil {
		t.Fatalf("a best-effort branch failure must not fail FetchStats, got %v", err)
	}
	if stats == nil || stats.Login != "octocat" {
		t.Fatalf("mandatory branches should still render, got %+v", stats)
	}
	// The failed watched repo resolved to nothing — the dashboard renders
	// without it rather than erroring the whole refresh.
	if len(stats.WatchedRepos) != 0 {
		t.Errorf("a failed watched repo should not appear, got %+v", stats.WatchedRepos)
	}
}
