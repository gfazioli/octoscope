package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/gfazioli/octoscope/internal/github"
	"github.com/muesli/termenv"
)

// TestRenderReposTabShowsWatchedWhenOwnedEmpty is the regression for the
// P0 paint/cursor desync: when the owned set is empty but watch_repos is
// configured (a brand-new account; public-only mode with every owned
// repo private), the Watched section must still render. The cursor and
// the action menu both walk visibleReposPartitioned — which includes
// watched rows — so painting the "no repositories" empty-state instead
// stranded the cursor on rows the user couldn't see.
func TestRenderReposTabShowsWatchedWhenOwnedEmpty(t *testing.T) {
	_ = applyTheme("octoscope", "")

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
	_ = applyTheme("octoscope", "")

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

// TestRenderReposTableArchivedLabelWidth pins the Name-column contract
// for archived rows: the column is sized as if the longest archived name
// carried the "Archived " tag, so an archived repo's name keeps its full width
// (no ellipsis) and every row stays exactly nameW cells wide — header
// included — so no right-hand column shifts.
func TestRenderReposTableArchivedLabelWidth(t *testing.T) {
	repos := []github.Repo{
		mkRepo("me", "alpha", 1),
		mkRepo("me", "beta-service", 2),
	}
	repos[1].IsArchived = true

	out := ansi.Strip(renderReposTable(repos, 0, ReposSortPushed, 0, 0, false))
	lines := strings.Split(out, "\n")
	if len(lines) < 4 {
		t.Fatalf("expected header + rule + 2 rows, got %d lines", len(lines))
	}

	archivedRow := lines[3]
	if !strings.Contains(archivedRow, "Archived beta-service") {
		t.Errorf("archived name truncated by the label budget: %q", archivedRow)
	}
	if strings.Contains(archivedRow, "…") {
		t.Errorf("archived row should not ellipsize a 12-char name in a 21-cell column: %q", archivedRow)
	}
	for i, l := range lines {
		if cellWidth(l) != cellWidth(lines[0]) {
			t.Errorf("line %d is %d cells, header is %d — columns shifted:\n%s", i+1, cellWidth(l), cellWidth(lines[0]), out)
		}
	}
	// Cap case: allowance is applied before nameCap, so even at the 30-cell
	// ceiling the tag fits and the name keeps its 21 cells. Capping first
	// would leave only 12 for the name — this is what that ordering pins.
	long := github.Repo{Name: strings.Repeat("a", 30), URL: "https://github.com/me/long", IsArchived: true}
	out = ansi.Strip(renderReposTable([]github.Repo{long}, 0, ReposSortPushed, 0, 0, false))
	lines = strings.Split(out, "\n")
	if len(lines) < 3 {
		t.Fatalf("expected header + rule + 1 row, got %d lines", len(lines))
	}
	if !strings.Contains(lines[2], "Archived "+strings.Repeat("a", 20)+"…") {
		t.Errorf("at the cap the archived name must keep 21 cells: %q", lines[2])
	}
}

// TestRenderReposTableArchivedTagSizesOnlyArchivedRows pins that the tag's
// cells are added to archived names only: a short archived name beside a
// longer active one must not widen the Name column, so the Lang column
// starts where it would with no archived repo at all.
func TestRenderReposTableArchivedTagSizesOnlyArchivedRows(t *testing.T) {
	repos := []github.Repo{
		mkRepo("me", "a-rather-long-name-x", 1),
		mkRepo("me", "old", 2),
	}
	header := func() string {
		out := ansi.Strip(renderReposTable(repos, -1, ReposSortPushed, 0, 0, false))
		return strings.Split(out, "\n")[0]
	}
	want := strings.Index(header(), "Lang")
	repos[1].IsArchived = true
	if got := strings.Index(header(), "Lang"); got != want {
		t.Errorf("Lang column starts at %d with a short archived repo, want %d: the tag widened every row", got, want)
	}
}

// TestRenderReposTableActiveArchivedRowKeepsAccent pins the cursor row's
// highlight on an archived repo: the muted tag's own reset must not end
// the accent before the name, so the name is rendered in the same active
// style an unarchived cursor row gets.
func TestRenderReposTableActiveArchivedRowKeepsAccent(t *testing.T) {
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(prev) })

	repos := []github.Repo{mkRepo("me", "beta-service", 2)}
	repos[0].IsArchived = true
	row := strings.Split(renderReposTable(repos, 0, ReposSortPushed, 0, 0, false), "\n")[2]
	want := boldStyle.Foreground(colAccent).Render("beta-service")
	if !strings.Contains(row, want) {
		t.Errorf("cursor row of an archived repo lost its accent on the name:\n%q\nwant it to contain %q", row, want)
	}
}
