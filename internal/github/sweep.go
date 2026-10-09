package github

import (
	"context"
	"sync"
	"time"
)

// SweepTarget is one repository the sweep scans.
type SweepTarget struct {
	Owner string
	Name  string
	URL   string
}

// SweepResult is what became of one repository in a sweep: a scan of
// its default branch, or the reason there is none. Exactly one of Scan
// and NotScanned is set — a repository is never left out of the report,
// and never counted as clean without having been read.
type SweepResult struct {
	Target     SweepTarget
	Scan       *RepoScan
	NotScanned string
}

// sweepAttemptTimeout bounds one repository's scan attempt. Measured on
// the maintainer's 92 owned repositories (2026-09-17, #66): 0.77 s to
// 2.99 s per repository, one GraphQL query, one tree and a blob per
// matched file each; the budget leaves room for a slow moment without
// letting one repository hold the sweep.
const sweepAttemptTimeout = 30 * time.Second

// sweepBackoff is the first wait between retries of a repository's
// transient 5xx: TransientBackoff, as a variable for the tests.
var sweepBackoff = TransientBackoff

// SweepScan scans the default branch of every target, at most
// watchedRepoConcurrency at a time, and returns one result per target
// in the order given (#66). It answers the question the on-demand scan
// cannot — is anything of mine compromised — at the cost the issue
// measured: one bounded probe per repository, never every branch.
//
// accountRepos is the caller's repository list, for the push-burst
// context the scan reads from PushedAt timestamps it already holds.
// No baseline is read or written: a default-branch-only fingerprint
// would mislead the next full scan's delta, so the sweep reports what
// is there now and the dashboard's scan keeps the history.
//
// A transient 5xx is retried on the dashboard's policy; any other
// failure, a repository with no commits, and one whose default branch
// the scan could not reach all come back as NotScanned, with why.
func (c *Client) SweepScan(ctx context.Context, targets []SweepTarget, accountRepos []Repo) []SweepResult {
	results := make([]SweepResult, len(targets))
	sem := make(chan struct{}, watchedRepoConcurrency)
	var wg sync.WaitGroup
	for i, t := range targets {
		wg.Add(1)
		go func(i int, t SweepTarget) {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				results[i] = SweepResult{Target: t, NotScanned: "the sweep was stopped before this repository"}
				return
			}
			defer func() { <-sem }()
			results[i] = c.sweepOne(t, accountRepos)
		}(i, t)
	}
	wg.Wait()
	return results
}

// sweepOne scans one repository's default branch and judges whether
// the result can be reported as a scan at all.
func (c *Client) sweepOne(t SweepTarget, accountRepos []Repo) SweepResult {
	scan, err := RetryTransient(func(ctx context.Context) (*RepoScan, error) {
		return c.FetchRepoScan(ctx, t.Owner, t.Name, ScanOptions{
			AccountRepos:      accountRepos,
			DefaultBranchOnly: true,
		})
	}, TransientAttempts, sweepBackoff, sweepAttemptTimeout)
	switch {
	case err != nil:
		return SweepResult{Target: t, NotScanned: Sanitize(err.Error())}
	case scan.DefaultBranch == "":
		return SweepResult{Target: t, NotScanned: "the repository has no commits yet"}
	case !scan.ScannedDefault:
		return SweepResult{Target: t, NotScanned: "its default branch is not among the first 100 branches the scan lists"}
	}
	return SweepResult{Target: t, Scan: scan}
}
