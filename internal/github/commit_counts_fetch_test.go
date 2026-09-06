package github

import (
	"context"
	"strings"
	"testing"
)

// commitRoutes wraps statsRoutes with the two queries the commit-count
// branch (#70) adds: the viewer-id lookup ensureViewerID runs first, and
// the paged commit query. pageTwoFails makes the second page 500 so the
// best-effort branch degrades; the first page always succeeds and is
// charged cost 2.
//
// The returned counter is how many times the commit query was served.
// The cost assertions below are only meaningful together with it: the
// base routes report no cost at all, so a cost of 2 or 4 can come from
// nowhere but these pages — but that is a property of the fixture, and
// the counter is what pins the branch to having actually run (Codex,
// PR #154, second pass).
func commitRoutes(pageTwoFails bool) (func(string) (int, string), *int) {
	base := statsRoutes(false)
	calls := new(int)
	return func(q string) (int, string) {
		switch {
		case strings.Contains(q, "viewer{id}"), strings.Contains(q, "viewer { id }"):
			return 200, `{"data":{"viewer":{"id":"MDQ6VXNlcjE="}}}`
		case strings.Contains(q, "$commitsCursor"):
			*calls++
			if *calls == 1 {
				return 200, `{"data":{"viewer":{"repositories":{"nodes":[{"nameWithOwner":"octocat/proj","defaultBranchRef":{"target":{"authoredYear":{"totalCount":7}}}}],"pageInfo":{"hasNextPage":true,"endCursor":"c1"}}},"rateLimit":{"limit":5000,"remaining":4980,"cost":2,"resetAt":"2026-06-01T00:00:00Z"}}}`
			}
			if pageTwoFails {
				return 500, `{"errors":[{"message":"boom"}]}`
			}
			return 200, `{"data":{"viewer":{"repositories":{"nodes":[],"pageInfo":{"hasNextPage":false}}},"rateLimit":{"limit":5000,"remaining":4978,"cost":2,"resetAt":"2026-06-01T00:00:00Z"}}}`
		}
		return base(q)
	}, calls
}

func TestFetchStatsCommitCountsMergeAndApply(t *testing.T) {
	routes, calls := commitRoutes(false)
	c := newRoutingGQLClient(t, routes)
	c.commitCounts = true

	stats, err := c.FetchStats(context.Background())
	if err != nil {
		t.Fatalf("FetchStats: %v", err)
	}
	if !stats.CommitsLastYearApplied {
		t.Fatal("both pages succeeded — CommitsLastYearApplied should be true")
	}
	if got := stats.Repositories[0].CommitsLastYear; got != 7 {
		t.Errorf("octocat/proj CommitsLastYear = %d, want 7 (merged by NameWithOwner)", got)
	}
	if *calls != 2 {
		t.Errorf("commit query served %d times, want 2 (one page with hasNextPage, one without)", *calls)
	}
	if stats.RateLimit.Cost != 4 {
		t.Errorf("cost = %d, want 4 (two commit pages at 2 each; the other branches report no cost)", stats.RateLimit.Cost)
	}
}

// TestFetchStatsCommitCountsDegradeKeepsCost pins the Codex finding on
// PR #154: when page two fails, page one was still charged, and its
// cost must reach the footer even though the branch delivered nothing.
func TestFetchStatsCommitCountsDegradeKeepsCost(t *testing.T) {
	routes, calls := commitRoutes(true)
	c := newRoutingGQLClient(t, routes)
	c.commitCounts = true

	stats, err := c.FetchStats(context.Background())
	if err != nil {
		t.Fatalf("a best-effort branch failure must not fail FetchStats, got %v", err)
	}
	if stats.CommitsLastYearApplied {
		t.Error("page two failed — CommitsLastYearApplied must be false")
	}
	if got := stats.Repositories[0].CommitsLastYear; got != 0 {
		t.Errorf("partial pages must not leak counts: got %d", got)
	}
	if *calls != 2 {
		t.Errorf("commit query served %d times, want 2 (page one ok, page two 500)", *calls)
	}
	if stats.RateLimit.Cost != 2 {
		t.Errorf("cost = %d, want 2 — the successful first page was charged and must be reported", stats.RateLimit.Cost)
	}
}

// And the gate: with no viewer to attribute commits to, the branch never
// runs at all — no query, no cost, no column.
func TestFetchStatsCommitCountsSkippedUnauthenticated(t *testing.T) {
	commitCalls := 0
	c := newRoutingGQLClient(t, func(q string) (int, string) {
		if strings.Contains(q, "$commitsCursor") {
			commitCalls++
			return 500, `{"errors":[{"message":"the commit branch must not run unauthenticated"}]}`
		}
		return statsRoutes(false)(q)
	})
	c.commitCounts = true
	c.authenticated = false

	stats, err := c.FetchStats(context.Background())
	if err != nil {
		t.Fatalf("FetchStats: %v", err)
	}
	if stats.CommitsLastYearApplied {
		t.Error("unauthenticated: the branch is gated off, Applied must be false")
	}
	// Applied=false alone would also be true of a branch that ran and
	// failed. The counter is the assertion: no viewer, no query.
	if commitCalls != 0 {
		t.Errorf("commit query served %d times unauthenticated, want 0", commitCalls)
	}
}
