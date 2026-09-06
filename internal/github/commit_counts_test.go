package github

import (
	"reflect"
	"strings"
	"testing"

	"github.com/shurcooL/githubv4"
)

// The commit-count branch (#70) is opt-in, best-effort and keyed by
// NameWithOwner like the CI branch. These tests pin the three things a
// refactor could quietly break: the merge, the rate-limit fold and the
// query's shape.

func TestExtractStatsMergesCommitCountsByNameWithOwner(t *testing.T) {
	var r repoFields
	for _, nwo := range []string{"alice/alpha", "alice/beta"} {
		var n struct {
			Name            githubv4.String
			NameWithOwner   githubv4.String
			URL             githubv4.String `graphql:"url"`
			IsPrivate       githubv4.Boolean
			PushedAt        githubv4.DateTime
			PrimaryLanguage struct {
				Name  githubv4.String
				Color githubv4.String
			}
			StargazerCount githubv4.Int
			ForkCount      githubv4.Int
			Issues         struct {
				TotalCount githubv4.Int
			} `graphql:"issues(states: OPEN)"`
			PullRequests struct {
				TotalCount githubv4.Int
			} `graphql:"pullRequests(states: OPEN)"`
			Languages struct {
				Edges []struct {
					Size githubv4.Int
					Node struct {
						Name  githubv4.String
						Color githubv4.String
					}
				}
			} `graphql:"languages(first: 10, orderBy: {field: SIZE, direction: DESC})"`
		}
		n.Name = githubv4.String(strings.SplitN(nwo, "/", 2)[1])
		n.NameWithOwner = githubv4.String(nwo)
		r.Repositories.Nodes = append(r.Repositories.Nodes, n)
	}

	var c repoCommitFields
	var cn struct {
		NameWithOwner    githubv4.String
		DefaultBranchRef struct {
			Target struct {
				Commit struct {
					AuthoredYear struct {
						TotalCount githubv4.Int
					} `graphql:"authoredYear: history(since: $since, author: $authorFilter)"`
				} `graphql:"... on Commit"`
			}
		}
	}
	cn.NameWithOwner = "alice/alpha"
	cn.DefaultBranchRef.Target.Commit.AuthoredYear.TotalCount = 42
	c.Repositories.Nodes = append(c.Repositories.Nodes, cn)

	stats := (&Client{}).extractStats(profileFields{}, r, repoCIFields{}, c)
	if len(stats.Repositories) != 2 {
		t.Fatalf("repos = %d, want 2", len(stats.Repositories))
	}
	byName := map[string]int{}
	for _, repo := range stats.Repositories {
		byName[repo.Name] = repo.CommitsLastYear
	}
	if byName["alpha"] != 42 {
		t.Errorf("alpha CommitsLastYear = %d, want 42 (merged by NameWithOwner)", byName["alpha"])
	}
	if byName["beta"] != 0 {
		t.Errorf("beta CommitsLastYear = %d, want 0 (absent from the commit branch)", byName["beta"])
	}
	if stats.CommitsLastYearApplied {
		t.Error("extractStats must not set CommitsLastYearApplied — only FetchStats knows whether the branch ran")
	}
}

func TestMergeRateLimitAllIgnoresUnusedBranches(t *testing.T) {
	// A gated-off branch contributes its zero value. It must neither
	// drag Remaining to 0 nor be counted as the pessimistic envelope.
	got := mergeRateLimitAll(envelope(5000, 4800, 1), envelope(5000, 4500, 2), rateLimitFields{}, envelope(5000, 4700, 3))
	if got.Remaining != 4500 || got.Limit != 5000 {
		t.Errorf("remaining/limit = %d/%d, want 4500/5000", got.Remaining, got.Limit)
	}
	if got.Cost != 6 {
		t.Errorf("cost = %d, want 6 (sums across every envelope)", got.Cost)
	}
	if all := mergeRateLimitAll(); all.Cost != 0 || all.Limit != 0 {
		t.Errorf("no envelopes: %+v, want zero RateLimit", all)
	}
	// The 3-way helper is now a thin wrapper; its table test elsewhere
	// keeps passing, and this pins that it really delegates.
	a, b, c := envelope(5000, 4900, 3), envelope(5000, 4800, 5), envelope(5000, 4950, 2)
	if x, y := mergeRateLimit3(a, b, c), mergeRateLimitAll(a, b, c); *x != *y {
		t.Errorf("mergeRateLimit3 diverged from mergeRateLimitAll: %+v vs %+v", *x, *y)
	}
}

// TestRepoCommitQueryShape pins the parts of the query that were chosen
// by measurement rather than taste: it pages at 50 (a 100-repo page sat
// at 44–62 % of GitHub's 10-second limit on a 91-repo account), it
// carries its own cursor variable, and the history field is filtered by
// $since and $authorFilter. A drive-by "make it 100 like the others"
// would fail here, with the reason in the tag.
func TestRepoCommitQueryShape(t *testing.T) {
	f, ok := reflect.TypeOf(repoCommitFields{}).FieldByName("Repositories")
	if !ok {
		t.Fatal("repoCommitFields.Repositories missing")
	}
	tag := f.Tag.Get("graphql")
	for _, want := range []string{"first: 50", "after: $commitsCursor", "ownerAffiliations: OWNER", "isFork: false"} {
		if !strings.Contains(tag, want) {
			t.Errorf("repositories tag %q lacks %q", tag, want)
		}
	}
	nodes := f.Type.Field(0)
	if nodes.Name != "Nodes" {
		t.Fatalf("first field = %s, want Nodes", nodes.Name)
	}
	commit, _ := nodes.Type.Elem().FieldByName("DefaultBranchRef")
	target, _ := commit.Type.FieldByName("Target")
	commitT, _ := target.Type.FieldByName("Commit")
	year, _ := commitT.Type.FieldByName("AuthoredYear")
	if got := year.Tag.Get("graphql"); !strings.Contains(got, "since: $since") || !strings.Contains(got, "author: $authorFilter") {
		t.Errorf("authoredYear tag %q must filter by $since and $authorFilter", got)
	}
	if maxRepoCommitPages != 2*maxRepoPages {
		t.Errorf("maxRepoCommitPages = %d, want 2×maxRepoPages so both walks cover the same 500 repos", maxRepoCommitPages)
	}
}
