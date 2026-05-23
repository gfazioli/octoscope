package github

import (
	"context"
	"sync"
	"time"

	"github.com/shurcooL/githubv4"
)

// singleRepoQuery mirrors the fields populated for owned repos in
// repoFields + repoCIFields, but for a single repository looked
// up by (owner, name). Used to power the Watched section under
// the Repos tab (v0.14.0): one targeted query per entry in
// `watch_repos`, run in parallel so the dashboard refresh stays
// close to the slowest sibling rather than their sum.
//
// Kept as a separate struct (rather than re-using repoFields'
// nested types) so future changes to the bulk repository list
// can evolve without dragging the watched-repo fetch with them.
type singleRepoQuery struct {
	Repository struct {
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
		DefaultBranchRef struct {
			Target struct {
				Commit struct {
					StatusCheckRollup struct {
						State githubv4.StatusState
					}
				} `graphql:"... on Commit"`
			}
		}
		Releases struct {
			Nodes []struct {
				TagName     githubv4.String
				PublishedAt githubv4.DateTime
			}
		} `graphql:"releases(first: 1, orderBy: {field: CREATED_AT, direction: DESC})"`
	} `graphql:"repository(owner: $owner, name: $name)"`
}

// FetchWatchedRepo runs one singleRepoQuery and reshapes the
// payload into a Repo, ready to drop into Stats.WatchedRepos.
// All user-controlled strings are scrubbed at the boundary via
// Sanitize — same defence-in-depth pattern as extractStats.
//
// A 404 from GitHub (repo removed, renamed, made private) is
// reported as a FetchError with ReasonServer; callers can choose
// to drop the entry silently instead of failing the whole
// dashboard. v0.14.0 takes the "drop silently" path so a single
// stale `watch_repos` entry doesn't break refresh for the user.
func (c *Client) FetchWatchedRepo(ctx context.Context, owner, name string) (Repo, error) {
	var q singleRepoQuery
	vars := map[string]interface{}{
		"owner": githubv4.String(owner),
		"name":  githubv4.String(name),
	}
	if err := c.gql.Query(ctx, &q, vars); err != nil {
		return Repo{}, &FetchError{Reason: classifyErr(ctx, err), Err: err}
	}
	r := q.Repository
	var (
		relTag string
		relAt  time.Time
	)
	if len(r.Releases.Nodes) > 0 {
		relTag = Sanitize(string(r.Releases.Nodes[0].TagName))
		relAt = r.Releases.Nodes[0].PublishedAt.Time
	}
	return Repo{
		Name:                     Sanitize(string(r.Name)),
		URL:                      Sanitize(string(r.URL)),
		PrimaryLanguage:          Sanitize(string(r.PrimaryLanguage.Name)),
		LanguageColor:            Sanitize(string(r.PrimaryLanguage.Color)),
		Stars:                    int(r.StargazerCount),
		Forks:                    int(r.ForkCount),
		OpenIssues:               int(r.Issues.TotalCount),
		OpenPRs:                  int(r.PullRequests.TotalCount),
		PushedAt:                 r.PushedAt.Time,
		IsPrivate:                bool(r.IsPrivate),
		CIState:                  Sanitize(string(r.DefaultBranchRef.Target.Commit.StatusCheckRollup.State)),
		LatestReleaseTag:         relTag,
		LatestReleasePublishedAt: relAt,
	}, nil
}

// watchedRepoConcurrency caps how many singleRepoQuery
// round-trips run in parallel inside FetchWatchedRepos. Picked
// at "enough to amortise serial latency but small enough that
// a 200-entry watch_repos list can't burst-flood GitHub or
// blow the goroutine count". Same shape any well-behaved
// fan-out client uses (kubectl, gh CLI, …).
const watchedRepoConcurrency = 10

// FetchWatchedRepos resolves a list of "owner/name" identifiers
// into Repo structs by running FetchWatchedRepo for each entry
// in parallel, bounded by watchedRepoConcurrency. Failed
// entries (404, private, network blip) are dropped silently —
// the dashboard mustn't fail over a single stale config line.
//
// Order is preserved: the returned slice matches the input
// order, so the Watched section under the Repos tab renders in
// the order the user wrote in their config file.
func (c *Client) FetchWatchedRepos(ctx context.Context, refs []string) []Repo {
	if len(refs) == 0 {
		return nil
	}
	out := make([]Repo, len(refs))
	ok := make([]bool, len(refs))

	// Buffered channel as a semaphore — each goroutine takes
	// a slot before issuing the query and releases it on exit.
	// Cap at watchedRepoConcurrency so a huge watch_repos list
	// doesn't flood GitHub with parallel requests.
	sem := make(chan struct{}, watchedRepoConcurrency)
	var wg sync.WaitGroup
	for i, ref := range refs {
		owner, name := splitOwnerNameKey(ref)
		if owner == "" || name == "" {
			continue
		}
		wg.Add(1)
		go func(idx int, owner, name string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			r, err := c.FetchWatchedRepo(ctx, owner, name)
			if err != nil {
				return // drop silently
			}
			out[idx] = r
			ok[idx] = true
		}(i, owner, name)
	}
	wg.Wait()
	// Compact the slice in input order, skipping failures.
	compact := out[:0]
	for i, r := range out {
		if !ok[i] {
			continue
		}
		compact = append(compact, r)
	}
	return compact
}

// splitOwnerNameKey parses an "owner/name" config string. Mirror
// of the more strict SanitizeRepoList in internal/config; kept
// local so the github package doesn't import config.
func splitOwnerNameKey(s string) (string, string) {
	for i := 0; i < len(s); i++ {
		if s[i] == '/' {
			if i == 0 || i == len(s)-1 {
				return "", ""
			}
			// Reject "a/b/c" style entries — must be exactly one slash.
			rest := s[i+1:]
			for j := 0; j < len(rest); j++ {
				if rest[j] == '/' {
					return "", ""
				}
			}
			return s[:i], rest
		}
	}
	return "", ""
}
