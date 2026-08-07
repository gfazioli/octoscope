package ui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/gfazioli/octoscope/internal/github"
)

func gist(desc string, files []string, stars int, public bool, ago time.Duration) github.Gist {
	g := github.Gist{
		Name:        "hash-" + desc + strings.Join(files, ""),
		Description: desc,
		URL:         "https://gist.github.com/u/abc",
		IsPublic:    public,
		Stars:       stars,
		UpdatedAt:   time.Now().Add(-ago),
	}
	for _, f := range files {
		g.Files = append(g.Files, github.GistFile{Name: f, Size: 100})
	}
	return g
}

// GitHub's gist "name" is a hash, so the description is the only
// human-readable handle — and it is routinely absent (2 of 16 on a real
// account). Every fallback step has to land on something a person can
// recognise, because the alternative is a row that reads as a hash.
func TestGistLabelFallsBackThroughFilenameToHash(t *testing.T) {
	tests := []struct {
		name string
		in   github.Gist
		want string
	}{
		{
			name: "description wins",
			in:   gist("Sample of product list", []string{"products.json"}, 0, true, time.Hour),
			want: "Sample of product list",
		},
		{
			name: "no description falls back to the first filename",
			in:   github.Gist{Name: "7ac536c", Files: []github.GistFile{{Name: "about.json"}}},
			want: "about.json",
		},
		{
			name: "whitespace-only description is not a description",
			in:   github.Gist{Name: "7ac536c", Description: "   ", Files: []github.GistFile{{Name: "about.json"}}},
			want: "about.json",
		},
		{
			name: "nothing at all falls back to the hash",
			in:   github.Gist{Name: "7ac536c"},
			want: "7ac536c",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := gistLabel(tt.in); got != tt.want {
				t.Errorf("gistLabel = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestVisibleGistsSortsAndFilters(t *testing.T) {
	in := []github.Gist{
		gist("beta", []string{"b.go"}, 1, true, 3*time.Hour),
		gist("alpha", []string{"a.rb"}, 9, false, 1*time.Hour),
		gist("gamma", []string{"c.py"}, 5, true, 2*time.Hour),
	}

	labels := func(gs []github.Gist) []string {
		out := make([]string, 0, len(gs))
		for _, g := range gs {
			out = append(out, gistLabel(g))
		}
		return out
	}

	t.Run("updated is newest first", func(t *testing.T) {
		got := labels(visibleGists(in, "", GistsSortUpdated))
		want := []string{"alpha", "gamma", "beta"}
		if strings.Join(got, ",") != strings.Join(want, ",") {
			t.Errorf("got %v, want %v", got, want)
		}
	})

	t.Run("stars is highest first", func(t *testing.T) {
		got := labels(visibleGists(in, "", GistsSortStars))
		want := []string{"alpha", "gamma", "beta"}
		if strings.Join(got, ",") != strings.Join(want, ",") {
			t.Errorf("got %v, want %v", got, want)
		}
	})

	t.Run("name is alphabetical by the label, not the hash", func(t *testing.T) {
		got := labels(visibleGists(in, "", GistsSortName))
		want := []string{"alpha", "beta", "gamma"}
		if strings.Join(got, ",") != strings.Join(want, ",") {
			t.Errorf("got %v, want %v", got, want)
		}
	})
}

// The filter has to match what the user can *see*. An untitled gist shows
// its first filename on the row, so matching the raw description alone
// would make it unfindable by the only name it displays.
func TestFilterGistsMatchesTheVisibleLabelAndFilenames(t *testing.T) {
	untitled := github.Gist{Name: "7ac536c", Files: []github.GistFile{{Name: "about.json"}}}
	titled := gist("WordPress hooks", []string{"hooks.php"}, 0, true, time.Hour)
	in := []github.Gist{untitled, titled}

	if got := filterGists(in, "about"); len(got) != 1 || got[0].Name != "7ac536c" {
		t.Errorf("filtering by the displayed filename found %d rows, want the untitled gist", len(got))
	}
	if got := filterGists(in, "wordpress"); len(got) != 1 || gistLabel(got[0]) != "WordPress hooks" {
		t.Errorf("filtering by description found %d rows", len(got))
	}
	if got := filterGists(in, "hooks.php"); len(got) != 1 {
		t.Errorf("filtering by a filename of a *titled* gist found %d rows, want 1", len(got))
	}
	if got := filterGists(in, "zzz"); len(got) != 0 {
		t.Errorf("no-match query returned %d rows", len(got))
	}

	// The case only the label check can satisfy, and the reason this test
	// is worth its length: with no description and no files, the row
	// displays the hash, so the hash is the only string a user can type to
	// find it. Matching the raw description here finds nothing, and the
	// filename loop has nothing to walk — caught by mutation, because
	// every other case in this test passes through the filename loop too
	// and so proved nothing about the label.
	bare := github.Gist{Name: "0d2c0a0530448ad1af7e46"}
	if got := filterGists([]github.Gist{bare, titled}, "0d2c0a"); len(got) != 1 || got[0].Name != bare.Name {
		t.Errorf("a gist with only a hash is unfindable by that hash: got %d rows", len(got))
	}
}

// The empty tab must not claim the account has no gists: the fetch is
// best-effort, so empty means "nothing to show" and could equally be a
// failed query. Asserting the stronger sentence would be the confident
// wrong answer the rest of the app is built to avoid.
func TestGistsTabDoesNotClaimTheAccountHasNone(t *testing.T) {
	var gm GistsModel
	out := ansi.Strip(gm.renderGistsTab(&github.Stats{}, 100, 20))
	if strings.Contains(strings.ToLower(out), "you have no") ||
		strings.Contains(strings.ToLower(out), "no gists yet") {
		t.Errorf("empty state asserts something the best-effort fetch cannot know: %q", out)
	}
	if !strings.Contains(out, "no gists to show") {
		t.Errorf("empty state should still say something: %q", out)
	}
}

// A truncated list has to say so. gistsPageSize caps one refresh, and
// GitHub reports the real total alongside — without the "of N" the window
// would look like the whole thing.
func TestGistsTabSaysWhenItIsShowingAWindow(t *testing.T) {
	stats := &github.Stats{
		Gists:      []github.Gist{gist("only one", []string{"a.go"}, 0, true, time.Hour)},
		GistsTotal: 240,
	}
	var gm GistsModel
	out := ansi.Strip(gm.renderGistsTab(stats, 120, 20))
	if !strings.Contains(out, "1 of 240 gists") {
		t.Errorf("a truncated list did not disclose the total:\n%s", out)
	}
}

// "secret" is GitHub's own word, and the accurate one: a non-public gist
// is readable by anyone holding the link. Calling it "private" would
// overstate what it protects.
func TestGistsTableCallsThemSecretNotPrivate(t *testing.T) {
	rows := []github.Gist{gist("hidden", []string{"a.go"}, 0, false, time.Hour)}
	out := ansi.Strip(renderGistsTable(rows, 0, GistsSortUpdated))
	if !strings.Contains(out, "secret") {
		t.Errorf("a non-public gist is not labelled secret:\n%s", out)
	}
	if strings.Contains(out, "private") {
		t.Errorf("labelled private, which overstates what a secret gist protects:\n%s", out)
	}
}

// Enter expands the files already carried by the row. There is no fetch,
// so there is no loading state to render and none to wait for.
func TestGistsExpandShowsFilesWithoutFetching(t *testing.T) {
	stats := &github.Stats{Gists: []github.Gist{
		gist("settings sync", []string{"cloudSettings", "extensions.json"}, 0, false, time.Hour),
	}}
	gm := GistsModel{expanded: true}
	out := ansi.Strip(gm.renderGistsTab(stats, 120, 30))
	for _, want := range []string{"cloudSettings", "extensions.json"} {
		if !strings.Contains(out, want) {
			t.Errorf("expanded view is missing %q:\n%s", want, out)
		}
	}
}

func TestHumanBytes(t *testing.T) {
	for _, tt := range []struct {
		in   int
		want string
	}{
		{0, "0 B"},
		{812, "812 B"},
		// Binary divisors, so binary labels. 1<<10 is a kibibyte; calling
		// it a kB was wrong by a factor that grows with the size, and
		// Copilot caught it on #123.
		{2048, "2.0 KiB"},
		{3 * 1024 * 1024, "3.0 MiB"},
	} {
		if got := humanBytes(tt.in); got != tt.want {
			t.Errorf("humanBytes(%d) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

// At the fetch cap the true file count is unknowable — GitHub's Gist.files
// is a plain list with no totalCount — so the row must not print a number
// it cannot stand behind. Both reviewers found this independently on #123.
func TestGistsTableDisclosesTheFileCap(t *testing.T) {
	names := make([]string, github.GistFilesLimit)
	for i := range names {
		names[i] = fmt.Sprintf("f%02d.go", i)
	}
	capped := gist("big", names, 0, true, time.Hour)
	small := gist("small", []string{"a.go", "b.go"}, 0, true, time.Hour)

	out := ansi.Strip(renderGistsTable([]github.Gist{capped, small}, 0, GistsSortUpdated))
	if !strings.Contains(out, fmt.Sprintf("%d+", github.GistFilesLimit)) {
		t.Errorf("a gist at the fetch cap printed an exact count:\n%s", out)
	}
	if !strings.Contains(out, " 2 ") && !strings.Contains(out, "2  ") {
		t.Errorf("a gist below the cap should print its real count:\n%s", out)
	}
}

// A filter and a truncated fetch are independent facts. They used to be an
// if/else, so filtering a truncated list silently dropped the truncation —
// the one of the two a reader cannot otherwise discover. Copilot, #123.
func TestGistsHeaderKeepsBothTheFilterAndTheTruncation(t *testing.T) {
	stats := &github.Stats{
		Gists: []github.Gist{
			gist("alpha", []string{"a.go"}, 0, true, time.Hour),
			gist("beta", []string{"b.go"}, 0, true, 2*time.Hour),
		},
		GistsTotal: 240,
	}
	gm := GistsModel{query: "alpha"}
	out := ansi.Strip(gm.renderGistsTab(stats, 140, 20))
	if !strings.Contains(out, "240") {
		t.Errorf("filtering a truncated list hid the total:\n%s", out)
	}
	if !strings.Contains(out, "1 of 2") {
		t.Errorf("the filter count went missing:\n%s", out)
	}
}

// Typing in the filter can move the cursor onto a different gist, and an
// expansion that survives shows one gist's files under another's name.
// CodeRabbit, #123.
func TestGistsFilterClosesAnOpenExpansion(t *testing.T) {
	gm := GistsModel{expanded: true, searchActive: true}
	got := gm.updateSearch(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a")})
	if got.expanded {
		t.Error("typing in the filter left the previous gist's files expanded")
	}

	gm = GistsModel{expanded: true}
	stats := &github.Stats{Gists: []github.Gist{gist("x", []string{"a.go"}, 0, true, time.Hour)}}
	got, _ = gm.Update(key("/"), stats)
	if got.expanded {
		t.Error("opening the search box left the expansion open underneath it")
	}
}

// The expansion competes with the list for the same rows. Unbounded, a
// 20-file gist on a short terminal pushed the pinned footer off screen —
// the list clamps at three rows, the expansion did not clamp at all.
func TestGistsExpansionIsBoundedByHeight(t *testing.T) {
	names := make([]string, 20)
	for i := range names {
		names[i] = fmt.Sprintf("file%02d.go", i)
	}
	stats := &github.Stats{Gists: []github.Gist{gist("big", names, 0, true, time.Hour)}}
	gm := GistsModel{expanded: true}

	out := ansi.Strip(gm.renderGistsTab(stats, 140, 16))
	if lines := strings.Count(out, "\n") + 1; lines > 16 {
		t.Errorf("rendered %d lines into a %d-line budget:\n%s", lines, 16, out)
	}
	if !strings.Contains(out, "of 20 files") {
		t.Errorf("the expansion was cut without saying so:\n%s", out)
	}
}

// The action menu has to actually open on the Gists tab.
//
// This test exists because it did not. The `case TabGists` was added to
// the action-menu switch during review, but the *guard* around that switch
// still listed only Repos / PRs / Issues — so the branch was unreachable
// and `space` fell through to the tab's own Update. Both the commit message
// and the README claimed the feature worked. Nothing failed, because
// nothing tested it; CodeRabbit read the guard.
func TestGistsActionMenuOpens(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "test-token-not-used")
	client, err := github.New("octocat", github.Options{})
	if err != nil {
		t.Fatalf("github.New: %v", err)
	}
	m := NewModel(client, "0.29.0", Options{})
	m.stats = &github.Stats{Gists: []github.Gist{
		gist("Sample list", []string{"a.json"}, 3, true, time.Hour),
	}}
	m.activeTab = TabGists

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeySpace, Runes: []rune(" ")})
	m = updated.(Model)

	if !m.actionMenu.IsOpen() {
		t.Fatal("space on the Gists tab did not open the action menu — the guard " +
			"around the switch excludes TabGists again")
	}
	view := ansi.Strip(m.actionMenu.View(80))
	if !strings.Contains(view, "Sample list") {
		t.Errorf("the menu is not titled for the selected gist:\n%s", view)
	}
	for _, want := range []string{"Open in GitHub", "Copy URL"} {
		if !strings.Contains(view, want) {
			t.Errorf("action %q missing:\n%s", want, view)
		}
	}
	// No "View details": the files expand on the row, so there is no
	// detail view to open and offering one would dead-end.
	if strings.Contains(view, "View details") {
		t.Errorf("the menu offers a detail view the Gists tab does not have:\n%s", view)
	}
}
