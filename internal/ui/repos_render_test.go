package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/gfazioli/octoscope/internal/github"
)

// TestRenderReposTabShowsWatchedWhenOwnedEmpty is the regression for the
// P0 paint/cursor desync: when the owned set is empty but watch_repos is
// configured (a brand-new account; public-only mode with every owned
// repo private), the Watched section must still render. The cursor and
// the action menu both walk visibleReposPartitioned — which includes
// watched rows — so painting the "no repositories" empty-state instead
// stranded the cursor on rows the user couldn't see.
func TestRenderReposTabShowsWatchedWhenOwnedEmpty(t *testing.T) {
	applyTheme("octoscope", "")

	stats := &github.Stats{
		Repositories: nil,
		WatchedRepos: []github.Repo{mkRepo("charmbracelet", "bubbletea", 20000)},
	}
	rm := ReposModel{}

	out := ansi.Strip(rm.renderReposTab(stats, 120, 40, nil))
	if strings.Contains(out, "no repositories to show yet") {
		t.Fatalf("renderReposTab painted the empty-state and hid the Watched section:\n%s", out)
	}
	if !strings.Contains(out, "bubbletea") {
		t.Errorf("watched repo not rendered when owned set is empty:\n%s", out)
	}

	// The paint and the cursor/action-menu source must agree.
	r, ok := rm.selectedRepo(stats, nil)
	if !ok || r.Name != "bubbletea" {
		t.Errorf("selectedRepo = (%q, %v), want the watched repo to be selectable", r.Name, ok)
	}
}

// TestRenderReposTabEmptyStates keeps the early-return behaviour honest
// now that the guard is on len(rows) rather than len(Repositories):
// truly-empty shows the waiting message, a no-match filter shows the
// esc-to-clear message instead.
func TestRenderReposTabEmptyStates(t *testing.T) {
	applyTheme("octoscope", "")

	t.Run("nothing fetched", func(t *testing.T) {
		out := ansi.Strip(ReposModel{}.renderReposTab(&github.Stats{}, 120, 40, nil))
		if !strings.Contains(out, "waiting for first refresh") {
			t.Errorf("want waiting message, got:\n%s", out)
		}
	})

	t.Run("filter matches nothing", func(t *testing.T) {
		stats := &github.Stats{Repositories: []github.Repo{mkRepo("alice", "alpha", 1)}}
		rm := ReposModel{query: "zzz-no-match"}
		out := ansi.Strip(rm.renderReposTab(stats, 120, 40, nil))
		if !strings.Contains(out, "no repositories match") {
			t.Errorf("want no-match message, got:\n%s", out)
		}
	})
}
