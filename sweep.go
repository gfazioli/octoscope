package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/gfazioli/octoscope/internal/github"
	"github.com/gfazioli/octoscope/internal/report"
)

// sweepSource is the part of *github.Client the --scan run uses, so a
// test can drive runSweep without the network.
type sweepSource interface {
	FetchStats(ctx context.Context) (*github.Stats, error)
	SweepScan(ctx context.Context, targets []github.SweepTarget, accountRepos []github.Repo) []github.SweepResult
	PublicOnly() bool
}

// runSweep is `octoscope --scan` (#66): fetch the dashboard for its
// repository list, sweep the default branch of every repository on it,
// print the report. It fails only when the list itself cannot be had —
// a repository that cannot be scanned is a row in the report, never a
// failed run, because "is anything of mine compromised" needs every
// answer, the unreadable ones included.
func runSweep(stdout, stderr io.Writer, client sweepSource, asJSON bool) error {
	stats, err := retryReport(30*time.Second, client.FetchStats)
	if err != nil {
		return err
	}
	publicOnly := client.PublicOnly()
	if publicOnly {
		stats = stats.Public()
	}
	targets := sweepTargets(stats)
	if isTerminal(stderr) {
		fmt.Fprintf(stderr, "octoscope: sweeping %d repositories, default branch only…\n", len(targets))
	}
	start := time.Now()
	results := client.SweepScan(context.Background(), targets, stats.Repositories)
	rep := report.FromSweep(results, version, time.Now().UTC(), time.Since(start), publicOnly)
	if asJSON {
		return report.RenderSweepJSON(stdout, rep)
	}
	return report.RenderSweepPlain(stdout, rep)
}

// sweepTargets is every repository the dashboard lists — the ones the
// account owns, then the watched ones (#59: an organisation's
// repositories enter the sweep the way they enter the dashboard, by
// being watched) — once each, in that order.
func sweepTargets(stats *github.Stats) []github.SweepTarget {
	seen := map[string]bool{}
	var out []github.SweepTarget
	for _, list := range [][]github.Repo{stats.Repositories, stats.WatchedRepos} {
		for _, r := range list {
			owner, name := github.SplitOwnerName(r.URL)
			if owner == "" || name == "" || seen[r.URL] {
				continue
			}
			seen[r.URL] = true
			out = append(out, github.SweepTarget{Owner: owner, Name: name, URL: r.URL})
		}
	}
	return out
}

// isTerminal reports whether w is a character device, so the progress
// line goes to a person and stays out of a cron job's mail.
func isTerminal(w io.Writer) bool {
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	fi, err := f.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}
