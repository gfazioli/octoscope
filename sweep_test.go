package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/gfazioli/octoscope/internal/github"
)

// fakeSweep stands in for the client: a dashboard to list, and a
// sweep that records what it was asked to scan.
type fakeSweep struct {
	stats      *github.Stats
	statsErr   error
	publicOnly bool
	asked      []github.SweepTarget
	results    func([]github.SweepTarget) []github.SweepResult
}

func (f *fakeSweep) FetchStats(context.Context) (*github.Stats, error) { return f.stats, f.statsErr }
func (f *fakeSweep) PublicOnly() bool                                  { return f.publicOnly }
func (f *fakeSweep) SweepScan(_ context.Context, t []github.SweepTarget, _ []github.Repo) []github.SweepResult {
	f.asked = t
	return f.results(t)
}

func allClean(targets []github.SweepTarget) []github.SweepResult {
	out := make([]github.SweepResult, len(targets))
	for i, t := range targets {
		out[i] = github.SweepResult{Target: t, Scan: &github.RepoScan{DefaultBranch: "main", ScannedDefault: true}}
	}
	return out
}

func TestSweepTargets(t *testing.T) {
	stats := &github.Stats{
		Repositories: []github.Repo{
			{URL: "https://github.com/me/a"},
			{URL: "https://github.com/me/b"},
			{URL: "not a github url"},
		},
		WatchedRepos: []github.Repo{
			{URL: "https://github.com/acme/lib"},
			{URL: "https://github.com/me/a"}, // also owned: once
		},
	}
	var got []string
	for _, tg := range sweepTargets(stats) {
		got = append(got, tg.Owner+"/"+tg.Name)
	}
	if strings.Join(got, " ") != "me/a me/b acme/lib" {
		t.Errorf("targets = %v; want the owned repositories, then the watched ones (an organisation's included), each once", got)
	}
}

func TestRunSweep(t *testing.T) {
	stats := &github.Stats{
		Login: "me",
		Repositories: []github.Repo{
			{URL: "https://github.com/me/public"},
			{URL: "https://github.com/me/secret", IsPrivate: true},
		},
		WatchedRepos: []github.Repo{{URL: "https://github.com/acme/lib"}},
	}

	t.Run("JSON: every repository, scanned or not", func(t *testing.T) {
		f := &fakeSweep{stats: stats, results: func(tg []github.SweepTarget) []github.SweepResult {
			out := allClean(tg)
			out[1] = github.SweepResult{Target: tg[1], NotScanned: "the repository has no commits yet"}
			return out
		}}
		var buf bytes.Buffer
		if err := runSweep(&buf, &bytes.Buffer{}, f, true); err != nil {
			t.Fatalf("runSweep: %v", err)
		}
		var doc struct {
			Summary struct {
				Repositories, Clean, NotScanned int
			} `json:"summary"`
			Repositories []struct {
				Repository string `json:"repository"`
				Scanned    bool   `json:"scanned"`
				Reason     string `json:"reason"`
				Verdict    string `json:"verdict"`
			} `json:"repositories"`
		}
		if err := json.Unmarshal(buf.Bytes(), &doc); err != nil {
			t.Fatalf("unmarshal: %v\n%s", err, buf.String())
		}
		if doc.Summary.Repositories != 3 || doc.Summary.Clean != 2 || len(doc.Repositories) != 3 {
			t.Errorf("summary = %+v, %d rows", doc.Summary, len(doc.Repositories))
		}
		if r := doc.Repositories[1]; r.Repository != "me/secret" || r.Scanned || r.Verdict != "" || r.Reason == "" {
			t.Errorf("an unscanned repository = %+v; want its reason and no verdict", r)
		}
	})

	t.Run("--public-only leaves private repositories out of the sweep", func(t *testing.T) {
		f := &fakeSweep{stats: stats, publicOnly: true, results: allClean}
		if err := runSweep(&bytes.Buffer{}, &bytes.Buffer{}, f, false); err != nil {
			t.Fatalf("runSweep: %v", err)
		}
		for _, tg := range f.asked {
			if tg.Name == "secret" {
				t.Errorf("swept %v under --public-only", tg)
			}
		}
		if len(f.asked) != 2 {
			t.Errorf("asked = %v, want the public repository and the watched one", f.asked)
		}
	})

	t.Run("a dashboard that cannot be fetched fails the run, before any sweep", func(t *testing.T) {
		f := &fakeSweep{statsErr: &github.FetchError{Reason: github.ReasonAuth, Err: errors.New("bad credentials")}, results: allClean}
		var out bytes.Buffer
		if err := runSweep(&out, &bytes.Buffer{}, f, true); err == nil || out.Len() != 0 || f.asked != nil {
			t.Errorf("err = %v, output %q, asked %v; want the error, nothing written, nothing swept", err, out.String(), f.asked)
		}
	})

	t.Run("no progress line when stderr is not a terminal", func(t *testing.T) {
		f := &fakeSweep{stats: stats, results: allClean}
		var errOut bytes.Buffer
		if err := runSweep(&bytes.Buffer{}, &errOut, f, false); err != nil {
			t.Fatalf("runSweep: %v", err)
		}
		if errOut.Len() != 0 {
			t.Errorf("wrote %q to a non-terminal stderr; a cron job would mail it", errOut.String())
		}
	})
}
