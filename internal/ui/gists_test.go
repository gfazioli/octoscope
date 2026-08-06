package ui

import (
	"strings"
	"testing"
	"time"

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
		{2048, "2.0 kB"},
		{3 * 1024 * 1024, "3.0 MB"},
	} {
		if got := humanBytes(tt.in); got != tt.want {
			t.Errorf("humanBytes(%d) = %q, want %q", tt.in, got, tt.want)
		}
	}
}
