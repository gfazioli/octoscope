package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
	watch      []string
	asked      []github.SweepTarget
	account    []github.Repo
	results    func([]github.SweepTarget) []github.SweepResult
}

func (f *fakeSweep) FetchStats(context.Context) (*github.Stats, error) { return f.stats, f.statsErr }
func (f *fakeSweep) PublicOnly() bool                                  { return f.publicOnly }
func (f *fakeSweep) WatchRepos() []string                              { return f.watch }
func (f *fakeSweep) SweepScan(_ context.Context, t []github.SweepTarget, account []github.Repo) []github.SweepResult {
	f.asked = t
	f.account = account
	return f.results(t)
}

func allClean(targets []github.SweepTarget) []github.SweepResult {
	out := make([]github.SweepResult, len(targets))
	for i, t := range targets {
		out[i] = github.SweepResult{Target: t, VisibilityKnown: true, Scan: &github.RepoScan{DefaultBranch: "main", ScannedDefault: true, URL: t.URL}}
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
		// The dashboard resolved only one of the three entries below; the
		// sweep must not inherit that.
		WatchedRepos: []github.Repo{{URL: "https://github.com/acme/lib", IsPrivate: true}},
	}
	stats.Repositories[1].IsPrivate = true
	watch := []string{"acme/lib", "acme/flaky", "Me/A", "malformed", "a/b/c"}
	var got []string
	for _, tg := range sweepTargets(stats, watch) {
		got = append(got, fmt.Sprintf("%s/%s:%v", tg.Owner, tg.Name, tg.Watched))
	}
	want := "me/a:false me/b:false acme/lib:true acme/flaky:true"
	if strings.Join(got, " ") != want {
		t.Errorf("targets = %v; want %q: the owned repositories, then every configured watched entry (resolved or not), each once whatever its case", got, want)
	}
	var vis []string
	for _, tg := range sweepTargets(stats, watch) {
		vis = append(vis, fmt.Sprintf("%s:%v/%v", tg.Name, tg.VisibilityKnown, tg.Private))
	}
	if want := "a:true/false b:true/true lib:true/true flaky:false/false"; strings.Join(vis, " ") != want {
		t.Errorf("visibility = %v; want %q: what the dashboard read, and unknown where it read nothing", vis, want)
	}
}

func TestKeepSweepResults(t *testing.T) {
	scanned := func(name, url string, watched, private bool) github.SweepResult {
		return github.SweepResult{
			Target:          github.SweepTarget{Owner: "o", Name: name, Watched: watched},
			Scan:            &github.RepoScan{URL: url, IsPrivate: private},
			VisibilityKnown: true, Private: private,
		}
	}
	// unread is a repository whose scan failed: its visibility is what
	// the dashboard read, if it read one.
	unread := func(name string, watched, known bool) github.SweepResult {
		return github.SweepResult{Target: github.SweepTarget{Owner: "o", Name: name, Watched: watched}, NotScanned: "Not Found", VisibilityKnown: known}
	}
	results := []github.SweepResult{
		scanned("own", "https://github.com/o/own", false, false),
		scanned("own-now-private", "https://github.com/o/own-now-private", false, true), // turned private after the dashboard listed it
		unread("own-empty", false, true),
		scanned("public", "https://github.com/o/public", true, false),
		scanned("secret", "https://github.com/o/secret", true, true),
		unread("gone", true, false),
		unread("resolved-then-failed", true, true), // the dashboard read it as public
		// Read, public, but not scanned: no commits yet. Its visibility is known.
		{Target: github.SweepTarget{Owner: "o", Name: "fresh", Watched: true}, NotScanned: "the repository has no commits yet", VisibilityKnown: true},
		scanned("old-name", "https://github.com/o/Own", true, false), // a rename of o/own
	}
	names := func(rs []github.SweepResult) string {
		var out []string
		for _, r := range rs {
			out = append(out, r.Target.Name)
		}
		return strings.Join(out, " ")
	}

	got, leftOut := keepSweepResults(append([]github.SweepResult(nil), results...), false)
	if names(got) != "own own-now-private own-empty public secret gone resolved-then-failed fresh" || leftOut != 0 {
		t.Errorf("without --public-only: %q, left out %d; want every row but the rename's duplicate", names(got), leftOut)
	}
	got, leftOut = keepSweepResults(append([]github.SweepResult(nil), results...), true)
	if names(got) != "own own-empty public resolved-then-failed fresh" || leftOut != 1 {
		t.Errorf("--public-only: %q, left out %d; want every private one dropped, owned included, the one GitHub never placed counted, and the public ones kept as rows, scanned or not", names(got), leftOut)
	}
}

func TestRunSweep(t *testing.T) {
	stats := &github.Stats{
		Login: "me",
		Repositories: []github.Repo{
			{URL: "https://github.com/me/public"},
			{URL: "https://github.com/me/secret", IsPrivate: true},
		},
		WatchedRepos: []github.Repo{{URL: "https://github.com/acme/lib"}, {URL: "https://github.com/acme/hidden", IsPrivate: true}},
	}

	t.Run("JSON: every repository, scanned or not", func(t *testing.T) {
		f := &fakeSweep{stats: stats, watch: []string{"acme/lib"}, results: func(tg []github.SweepTarget) []github.SweepResult {
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
		f := &fakeSweep{stats: stats, publicOnly: true, watch: []string{"acme/lib", "acme/hidden", "acme/gone"}, results: func(tg []github.SweepTarget) []github.SweepResult {
			out := allClean(tg)
			out[len(out)-1] = github.SweepResult{Target: tg[len(tg)-1], NotScanned: "Not Found"}
			return out
		}}
		var buf bytes.Buffer
		if err := runSweep(&buf, &bytes.Buffer{}, f, true); err != nil {
			t.Fatalf("runSweep: %v", err)
		}
		for _, tg := range f.asked {
			if tg.Name == "secret" || tg.Name == "hidden" {
				t.Errorf("swept %v under --public-only, which the dashboard had read as private", tg)
			}
		}
		if len(f.asked) != 3 {
			t.Errorf("asked = %v, want the public repository and the two watched entries not known to be private", f.asked)
		}
		for _, r := range f.account {
			if r.IsPrivate {
				t.Errorf("the push-burst context, whose findings name repositories, got private %s", r.URL)
			}
		}
		out := buf.String()
		if !strings.Contains(out, `"watched_left_out": 1`) || strings.Contains(out, "acme/gone") {
			t.Errorf("want the unreadable watched entry counted, not named:\n%s", out)
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
