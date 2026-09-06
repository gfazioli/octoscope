package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/gfazioli/octoscope/internal/github"
)

// The commits column (#70) is opt-in, and present only when the branch
// delivered. Three surfaces have to agree about that: the s cycle, the
// sort actually applied, and the table. Each is pinned here.

func TestNextSortSkipsCommitsUnlessConfigured(t *testing.T) {
	off := ReposModel{sort: ReposSortRelease}
	if got := off.nextSort(); got != ReposSortPushed {
		t.Errorf("off: after release want pushed (commits skipped), got %v", got)
	}
	on := ReposModel{sort: ReposSortRelease, commitCounts: true}
	if got := on.nextSort(); got != ReposSortCommits {
		t.Errorf("on: after release want commits, got %v", got)
	}
	if got := (ReposModel{sort: ReposSortCommits, commitCounts: true}).nextSort(); got != ReposSortPushed {
		t.Errorf("on: after commits want pushed (wraps), got %v", got)
	}
	// Every mode is reachable when on, and commits is never reached when off.
	seen := map[ReposSort]bool{}
	m := ReposModel{commitCounts: false}
	for i := 0; i < len(reposSortLabels)*2; i++ {
		m.sort = m.nextSort()
		seen[m.sort] = true
	}
	if seen[ReposSortCommits] {
		t.Error("off: the cycle reached commits")
	}
	if len(seen) != len(reposSortLabels)-1 {
		t.Errorf("off: cycle visits %d modes, want %d", len(seen), len(reposSortLabels)-1)
	}
}

func TestEffectiveSortFallsBackWhenCountsMissing(t *testing.T) {
	rm := ReposModel{sort: ReposSortCommits, commitCounts: true}
	if got := rm.effectiveSort(false); got != ReposSortPushed {
		t.Errorf("not applied: want pushed, got %v", got)
	}
	if got := rm.effectiveSort(true); got != ReposSortCommits {
		t.Errorf("applied: want commits, got %v", got)
	}
	if got := (ReposModel{sort: ReposSortStars}).effectiveSort(false); got != ReposSortStars {
		t.Errorf("other modes must pass through, got %v", got)
	}
}

func TestSortReposByCommits(t *testing.T) {
	a := mkRepo("alice", "alpha", 1)
	a.CommitsLastYear = 3
	b := mkRepo("alice", "beta", 1)
	b.CommitsLastYear = 30
	c := mkRepo("alice", "gamma", 1)
	c.CommitsLastYear = 3
	got := sortRepos([]github.Repo{a, b, c}, ReposSortCommits)
	want := []string{"beta", "alpha", "gamma"} // desc, then name
	for i, w := range want {
		if got[i].Name != w {
			t.Fatalf("order = %v, want %v", repoNames(got), want)
		}
	}
}

func repoNames(rs []github.Repo) []string {
	out := make([]string, len(rs))
	for i, r := range rs {
		out[i] = r.Name
	}
	return out
}

func TestReposTableShowsCommitsOnlyWhenApplied(t *testing.T) {
	_ = applyTheme("octoscope", "")
	r := mkRepo("alice", "alpha", 5)
	r.CommitsLastYear = 1234

	with := ansi.Strip(renderReposTable([]github.Repo{r}, 0, ReposSortPushed, 0, 0, true))
	if !strings.Contains(with, "Commits") {
		t.Errorf("showCommits=true: header lacks Commits:\n%s", with)
	}
	if !strings.Contains(with, formatCompact(1234)) {
		t.Errorf("showCommits=true: row lacks the count %q:\n%s", formatCompact(1234), with)
	}

	without := ansi.Strip(renderReposTable([]github.Repo{r}, 0, ReposSortPushed, 0, 0, false))
	if strings.Contains(without, "Commits") || strings.Contains(without, formatCompact(1234)) {
		t.Errorf("showCommits=false: column or count leaked:\n%s", without)
	}

	// Header width must stay consistent between the two, minus the
	// column: a stray separator would misalign every row under it.
	wl, wol := strings.Split(with, "\n")[0], strings.Split(without, "\n")[0]
	if len([]rune(wl))-len([]rune(wol)) != 7+2 { // commitsW + "  "
		t.Errorf("header width delta = %d, want 9 (commitsW + separator)", len([]rune(wl))-len([]rune(wol)))
	}
}

func TestReposHeaderChipReportsEffectiveSort(t *testing.T) {
	_ = applyTheme("octoscope", "")
	stats := &github.Stats{Repositories: []github.Repo{mkRepo("alice", "alpha", 1)}}
	rm := ReposModel{sort: ReposSortCommits, commitCounts: true}

	// Configured, but this refresh did not deliver: the chip must not
	// claim "commits" while the rows are sorted by pushed.
	out := ansi.Strip(rm.renderReposTab(stats, 120, 40, nil))
	if strings.Contains(out, "commits ↓") {
		t.Errorf("chip says commits while counts are not applied:\n%s", out)
	}
	if !strings.Contains(out, "pushed ↓") {
		t.Errorf("chip should fall back to pushed:\n%s", out)
	}

	stats.CommitsLastYearApplied = true
	out = ansi.Strip(rm.renderReposTab(stats, 120, 40, nil))
	if !strings.Contains(out, "commits ↓") || !strings.Contains(out, "Commits") {
		t.Errorf("applied: chip and column should both show commits:\n%s", out)
	}
}
