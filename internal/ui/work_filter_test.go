package ui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/gfazioli/octoscope/internal/github"
)

func workRepos() []github.Repo {
	now := time.Now()
	return []github.Repo{
		{Name: "busy", URL: "https://github.com/me/busy", OpenPRs: 3, CIState: "SUCCESS", PushedAt: now},
		{Name: "broken", URL: "https://github.com/me/broken", CIState: "FAILURE", PushedAt: now},
		{Name: "errored", URL: "https://github.com/me/errored", CIState: "ERROR", PushedAt: now},
		{Name: "pending", URL: "https://github.com/me/pending", CIState: "PENDING", PushedAt: now},
		{Name: "dormant", URL: "https://github.com/me/dormant", CIState: "SUCCESS", PushedAt: now.Add(-120 * 24 * time.Hour)},
		{Name: "never-pushed", URL: "https://github.com/me/never-pushed"}, // zero PushedAt
	}
}

// applyWorkFilter's matching rules per preset, including the edges:
// PENDING / empty CI are not "broken", a zero PushedAt is stale.
func TestApplyWorkFilter(t *testing.T) {
	repos := workRepos()

	names := func(rs []github.Repo) string {
		var out []string
		for _, r := range rs {
			out = append(out, r.Name)
		}
		return strings.Join(out, ",")
	}

	tests := []struct {
		name string
		f    WorkFilter
		want string
	}{
		{"none is a no-op", WorkFilterNone, "busy,broken,errored,pending,dormant,never-pushed"},
		{"PRs open", WorkFilterPRsOpen, "busy"},
		{"CI broken = FAILURE or ERROR only", WorkFilterCIBroken, "broken,errored"},
		{"stale includes zero PushedAt", WorkFilterStale, "dormant,never-pushed"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := names(applyWorkFilter(repos, tt.f)); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

// The work preset must span every section uniformly — a pinned repo
// and a watched repo that don't match disappear exactly like an
// owned one (same contract as the `/` filter).
func TestWorkFilterSpansAllSections(t *testing.T) {
	owned := workRepos()
	watched := []github.Repo{
		{Name: "ext-busy", URL: "https://github.com/them/ext-busy", OpenPRs: 1},
		{Name: "ext-quiet", URL: "https://github.com/them/ext-quiet"},
	}
	pinned := []string{"me/busy", "me/broken"}

	rows, pinCount, restCount, watchCount := visibleReposPartitioned(
		owned, watched, "", WorkFilterPRsOpen, ReposSortPushed, pinned)

	if pinCount != 1 || rows[0].Name != "busy" {
		t.Errorf("pinned section should keep only the matching pin: pinCount=%d rows[0]=%q", pinCount, rows[0].Name)
	}
	if restCount != 0 {
		t.Errorf("no unpinned owned repo has open PRs, restCount = %d", restCount)
	}
	if watchCount != 1 || rows[len(rows)-1].Name != "ext-busy" {
		t.Errorf("watched section should keep only ext-busy: watchCount=%d", watchCount)
	}
}

// The `w` key cycles through every preset and back to none, resetting
// the cursor; esc clears the work preset together with the query.
func TestWorkFilterKeyCycleAndEsc(t *testing.T) {
	stats := &github.Stats{Repositories: workRepos()}
	w := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("w")}

	rm := ReposModel{cursor: 3}
	want := []WorkFilter{WorkFilterPRsOpen, WorkFilterCIBroken, WorkFilterStale, WorkFilterNone}
	for i, expect := range want {
		rm, _ = rm.Update(w, stats, nil)
		if rm.work != expect {
			t.Fatalf("press %d: work = %v, want %v", i+1, rm.work, expect)
		}
	}
	if rm.cursor != 0 {
		t.Error("w should reset the cursor")
	}

	// esc clears both filters at once.
	rm.work = WorkFilterStale
	rm.query = "bro"
	rm, _ = rm.Update(tea.KeyMsg{Type: tea.KeyEsc}, stats, nil)
	if rm.work != WorkFilterNone || rm.query != "" {
		t.Errorf("esc should clear query+work, got work=%v query=%q", rm.work, rm.query)
	}
}

// Header chip, narrowed count, and the filtered-to-nothing messages.
func TestWorkFilterRendering(t *testing.T) {
	stats := &github.Stats{Repositories: workRepos()}

	t.Run("header names the preset and narrows the count", func(t *testing.T) {
		rm := ReposModel{work: WorkFilterCIBroken}
		out := ansi.Strip(rm.renderReposTab(stats, 100, 0, nil))
		for _, want := range []string{"work CI broken", "2 of 6 repositories", "w work"} {
			if !strings.Contains(out, want) {
				t.Errorf("tab should contain %q:\n%s", want, out)
			}
		}
		if strings.Contains(out, "dormant") {
			t.Error("non-matching repos must not render under CI broken")
		}
	})

	t.Run("empty result names the active preset", func(t *testing.T) {
		rm := ReposModel{work: WorkFilterPRsOpen, query: "zzz"}
		out := ansi.Strip(rm.renderReposTab(stats, 100, 0, nil))
		if !strings.Contains(out, `"zzz"`) || !strings.Contains(out, "PRs open") {
			t.Errorf("empty state should name both filters:\n%s", out)
		}

		rm = ReposModel{work: WorkFilterPRsOpen}
		empty := &github.Stats{Repositories: []github.Repo{{Name: "quiet", URL: "https://github.com/me/quiet"}}}
		out = ansi.Strip(rm.renderReposTab(empty, 100, 0, nil))
		if !strings.Contains(out, "PRs open") || !strings.Contains(out, "esc to clear") {
			t.Errorf("work-only empty state should name the preset + esc affordance:\n%s", out)
		}
	})
}
