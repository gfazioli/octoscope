package github

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
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

// TestFetchStatsExcludesArchivedFromWorkloadCounters pins the archived
// contract: issues and PRs are read-only on an archived repo, so they aren't
// workload — but stars, forks and the repo count stay inclusive
// (lifetime popularity, not work). Drives the full fetch through the routing
// harness with one active and one archived repo.
func TestFetchStatsExcludesArchivedFromWorkloadCounters(t *testing.T) {
	const rl = `"rateLimit":{"limit":5000,"remaining":4990,"resetAt":"2026-06-01T00:00:00Z"}`
	c := newRoutingGQLClient(t, func(q string) (int, string) {
		if strings.Contains(q, "$reposCursor") || strings.Contains(q, "languages(") {
			return 200, `{"data":{"viewer":{"repositories":{"totalCount":2,"nodes":[
				{"name":"proj","nameWithOwner":"octocat/proj","url":"https://github.com/octocat/proj","isPrivate":false,"isArchived":false,"stargazerCount":10,"forkCount":4,
				 "issues":{"totalCount":2},"pullRequests":{"totalCount":3}},
				{"name":"old","nameWithOwner":"octocat/old","url":"https://github.com/octocat/old","isPrivate":false,"isArchived":true,"stargazerCount":99,"forkCount":88,
				 "issues":{"totalCount":7},"pullRequests":{"totalCount":5}}],
				"pageInfo":{"hasNextPage":false}}},` + rl + `}}`
		}
		return statsRoutes(false)(q)
	})

	stats, err := c.FetchStats(context.Background())
	if err != nil {
		t.Fatalf("FetchStats: %v", err)
	}
	if stats == nil {
		t.Fatal("FetchStats returned nil stats")
	}
	if stats.OpenIssues != 2 {
		t.Errorf("OpenIssues = %d, want 2 (archived repo's 7 must not count)", stats.OpenIssues)
	}
	if stats.OpenPRs != 3 {
		t.Errorf("OpenPRs = %d, want 3 (archived repo's 5 must not count)", stats.OpenPRs)
	}
	if stats.TotalStars != 109 {
		t.Errorf("TotalStars = %d, want 109 (10 + 99, archived stays in)", stats.TotalStars)
	}
	if stats.ForksReceived != 92 {
		t.Errorf("ForksReceived = %d, want 92 (4 + 88, archived stays in)", stats.ForksReceived)
	}
	if stats.PublicRepos != 2 {
		t.Errorf("PublicRepos = %d, want 2", stats.PublicRepos)
	}
	// Both repos stay listed so the UI can label the archived one.
	if len(stats.Repositories) != 2 {
		t.Fatalf("Repositories = %d entries, want 2", len(stats.Repositories))
	}
	byName := map[string]Repo{}
	for _, r := range stats.Repositories {
		byName[r.Name] = r
	}
	if !byName["old"].IsArchived || byName["proj"].IsArchived {
		t.Errorf("IsArchived flags wrong: old=%+v proj=%+v", byName["old"], byName["proj"])
	}
}

// The PRs tab's stack marker (#99) comes from the eighth branch, its
// own query, and lands on both lists by URL.
func TestFetchStatsCarriesTheStackPlacement(t *testing.T) {
	base := statsRoutes(false)
	const rl = `"rateLimit":{"limit":5000,"remaining":4990,"resetAt":"2026-06-01T00:00:00Z"}`
	c := newRoutingGQLClient(t, func(q string) (int, string) {
		switch {
		case strings.Contains(q, "stackEntry"):
			return 200, `{"data":{"viewer":{"pullRequests":{"nodes":[
				{"url":"https://github.com/o/r/pull/330","stackEntry":{"position":1,"stack":{"size":3}}},
				{"url":"https://github.com/o/r/pull/9","stackEntry":null}]}},
				"search":{"nodes":[{"url":"https://github.com/g/s/pull/307","stackEntry":{"position":4,"stack":{"size":5}}}]}}}`
		case strings.Contains(q, "contributionsCollection"):
			return 200, `{"data":{"viewer":{"login":"octocat","openPRs":{"totalCount":2,"nodes":[
				{"number":330,"title":"layer","url":"https://github.com/o/r/pull/330","repository":{"nameWithOwner":"o/r"}},
				{"number":9,"title":"alone","url":"https://github.com/o/r/pull/9","repository":{"nameWithOwner":"o/r"}}]}},` + rl + `}}`
		case strings.Contains(q, "search("):
			return 200, `{"data":{"search":{"issueCount":1,"nodes":[{"__typename":"PullRequest","number":307,"title":"rr","url":"https://github.com/g/s/pull/307","repository":{"nameWithOwner":"g/s"},"author":{"login":"a"}}]}}}`
		}
		return base(q)
	})
	stats, err := c.FetchStats(context.Background())
	if err != nil {
		t.Fatalf("FetchStats: %v", err)
	}
	if len(stats.OpenPullRequests) != 2 || len(stats.ReviewRequests) != 1 {
		t.Fatalf("open PRs %+v, review requests %+v", stats.OpenPullRequests, stats.ReviewRequests)
	}
	if p := stats.OpenPullRequests[0]; p.StackPosition != 1 || p.StackSize != 3 {
		t.Errorf("own layer = %d/%d, want 1/3", p.StackPosition, p.StackSize)
	}
	if p := stats.OpenPullRequests[1]; p.StackSize != 0 {
		t.Errorf("a PR outside any stack got %d/%d", p.StackPosition, p.StackSize)
	}
	if p := stats.ReviewRequests[0]; p.StackPosition != 4 || p.StackSize != 5 {
		t.Errorf("review request = %d/%d, want 4/5", p.StackPosition, p.StackSize)
	}
}

// The point of the eighth branch: a placements query that fails costs
// the markers, never the dashboard.
func TestFetchStatsSurvivesAFailedStackPlacement(t *testing.T) {
	base := statsRoutes(false)
	for name, answer := range map[string]struct {
		status int
		body   string
	}{
		"a GraphQL error on the field": {200, `{"errors":[{"message":"Field 'stackEntry' doesn't exist on type 'PullRequest'"}]}`},
		"a 502":                        {502, `502 Bad Gateway`},
	} {
		t.Run(name, func(t *testing.T) {
			c := newRoutingGQLClient(t, func(q string) (int, string) {
				if strings.Contains(q, "stackEntry") {
					return answer.status, answer.body
				}
				return base(q)
			})
			stats, err := c.FetchStats(context.Background())
			if err != nil || stats == nil || stats.Login != "octocat" {
				t.Fatalf("got %+v, %v; want the dashboard without the markers", stats, err)
			}
		})
	}
}

// Neither mandatory query asks for the stack: an error on it there
// would fail the dashboard, which is what the eighth branch avoids.
func TestMandatoryQueriesDoNotAskForTheStack(t *testing.T) {
	var (
		mu   sync.Mutex
		sent []string
	)
	c := newRoutingGQLClient(t, func(q string) (int, string) {
		mu.Lock()
		sent = append(sent, q)
		mu.Unlock()
		return statsRoutes(false)(q)
	})
	if _, err := c.FetchStats(context.Background()); err != nil {
		t.Fatalf("FetchStats: %v", err)
	}
	asked := 0
	for _, q := range sent {
		if !strings.Contains(q, "stackEntry") {
			continue
		}
		asked++
		if strings.Contains(q, "contributionsCollection") || strings.Contains(q, "review-requested") && strings.Contains(q, "mergeable") {
			t.Errorf("a mandatory query asks for the stack:\n%s", q)
		}
	}
	if asked != 1 {
		t.Errorf("%d queries ask for the stack, want exactly the placements one", asked)
	}
}
