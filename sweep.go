package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"slices"
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
	targets := sweepTargets(stats, client.WatchRepos())
	accountRepos := stats.Repositories
	if publicOnly {
		// A repository the dashboard read as private is not swept at
		// all, owned or watched; and the push-burst context, whose
		// findings name other repositories, gets the public ones only.
		targets = slices.DeleteFunc(targets, func(t github.SweepTarget) bool { return t.VisibilityKnown && t.Private })
		accountRepos = stats.Public().Repositories
	}
	if isTerminal(stderr) {
		fmt.Fprintf(stderr, "octoscope: sweeping %d repositories, default branch only…\n", len(targets))
	}
	start := time.Now()
	results, leftOut := keepSweepResults(client.SweepScan(context.Background(), targets, accountRepos), publicOnly)
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
//
// Each target carries the visibility the dashboard read, where it read
// one: always for an owned repository, and for a watched one whose
// lookup succeeded.
func sweepTargets(stats *github.Stats, watchRefs []string) []github.SweepTarget {
	seen := map[string]bool{}
	var out []github.SweepTarget
	add := func(t github.SweepTarget) {
		key := strings.ToLower(t.Owner + "/" + t.Name)
		if t.Owner == "" || t.Name == "" || seen[key] {
			return
		}
		seen[key] = true
		out = append(out, t)
	}
	for _, r := range stats.Repositories {
		owner, name := github.SplitOwnerName(r.URL)
		add(github.SweepTarget{Owner: owner, Name: name, URL: r.URL, VisibilityKnown: true, Private: r.IsPrivate})
	}
	resolved := map[string]github.Repo{}
	for _, r := range stats.WatchedRepos {
		owner, name := github.SplitOwnerName(r.URL)
		resolved[strings.ToLower(owner+"/"+name)] = r
	}
	for _, ref := range watchRefs {
		owner, name, ok := strings.Cut(strings.TrimSpace(ref), "/")
		if !ok || strings.Contains(name, "/") {
			continue
		}
		t := github.SweepTarget{Owner: owner, Name: name, URL: "https://github.com/" + owner + "/" + name, Watched: true}
		if r, ok := resolved[strings.ToLower(owner+"/"+name)]; ok {
			t.VisibilityKnown, t.Private = true, r.IsPrivate
		}
		add(t)
	}
	return out
}

// keepSweepResults drops what the report must not show and counts what
// it had to leave out.
//
// A watched entry that resolves to a repository already swept — a
// renamed repository still configured under its old name — is that
// repository twice, and goes. Under --public-only a repository is named
// only when GitHub said it is public, in the dashboard fetch or in its
// scan, the later answer winning: one found private goes, the way the
// dashboard hides it, and one whose visibility GitHub never told either
// call — a watched entry the dashboard could not resolve and the scan
// could not read — is counted rather than named.
//
// What this does not cover: a repository that turns private in the
// seconds between the dashboard fetch and a scan that then fails is
// named, under the visibility the dashboard read moments before.
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
			if !r.VisibilityKnown {
				leftOut++
				continue
			}
			if r.Private {
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
