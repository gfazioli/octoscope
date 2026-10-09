package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
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
	WatchRepos() []string
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
	targets := sweepTargets(stats, client.WatchRepos())
	if isTerminal(stderr) {
		fmt.Fprintf(stderr, "octoscope: sweeping %d repositories, default branch only…\n", len(targets))
	}
	start := time.Now()
	results, leftOut := keepSweepResults(client.SweepScan(context.Background(), targets, stats.Repositories), publicOnly)
	rep := report.FromSweep(results, version, time.Now().UTC(), time.Since(start), publicOnly)
	rep.WatchedLeftOut = leftOut
	if asJSON {
		return report.RenderSweepJSON(stdout, rep)
	}
	return report.RenderSweepPlain(stdout, rep)
}

// sweepTargets is every repository the dashboard covers — the ones the
// account owns, then every watch_repos entry (#59: an organisation's
// repositories enter the sweep the way they enter the dashboard, by
// being watched) — once each, in that order.
//
// The watched half is the configured list itself, not the dashboard's
// resolved one: the dashboard drops an entry whose lookup failed for
// that refresh, and a sweep that inherited the drop would leave a
// repository out of the report without a row. Here a watched entry is
// always scanned, and one that cannot be read says why.
func sweepTargets(stats *github.Stats, watchRefs []string) []github.SweepTarget {
	seen := map[string]bool{}
	var out []github.SweepTarget
	add := func(owner, name, url string, watched bool) {
		key := strings.ToLower(owner + "/" + name)
		if owner == "" || name == "" || seen[key] {
			return
		}
		seen[key] = true
		out = append(out, github.SweepTarget{Owner: owner, Name: name, URL: url, Watched: watched})
	}
	for _, r := range stats.Repositories {
		owner, name := github.SplitOwnerName(r.URL)
		add(owner, name, r.URL, false)
	}
	for _, ref := range watchRefs {
		owner, name, ok := strings.Cut(strings.TrimSpace(ref), "/")
		if !ok || strings.Contains(name, "/") {
			continue
		}
		add(owner, name, "https://github.com/"+owner+"/"+name, true)
	}
	return out
}

// keepSweepResults drops what the report must not show and counts what
// it had to leave out.
//
// A watched entry that resolves to a repository already swept — a
// renamed repository still configured under its old name — is that
// repository twice, and goes. Under --public-only any repository the
// scan found private goes, the way the dashboard hides it — an owned
// one included, in case it turned private after the dashboard listed
// it. A watched repository whose visibility GitHub never told the scan
// goes as well, because nothing confirms it is public, and the report
// counts those rather than naming them; an owned one stays, since the
// dashboard listed it as public moments before.
func keepSweepResults(results []github.SweepResult, publicOnly bool) ([]github.SweepResult, int) {
	seen := map[string]bool{}
	out := results[:0:0]
	leftOut := 0
	for _, r := range results {
		if r.Scan != nil {
			key := strings.ToLower(r.Scan.URL)
			if key != "" && seen[key] {
				continue
			}
			seen[key] = true
		}
		if publicOnly {
			if r.VisibilityKnown && r.Private {
				continue
			}
			if !r.VisibilityKnown && r.Target.Watched {
				leftOut++
				continue
			}
		}
		out = append(out, r)
	}
	return out, leftOut
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
