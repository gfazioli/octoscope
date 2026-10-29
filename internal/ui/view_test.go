package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/gfazioli/octoscope/internal/github"
)

func TestPackLines(t *testing.T) {
	cases := []struct {
		name  string
		items []string
		sep   string
		width int
		want  string
	}{
		{
			name:  "empty input returns empty",
			items: nil,
			sep:   " · ",
			width: 40,
			want:  "",
		},
		{
			name:  "single item fits",
			items: []string{"abc"},
			sep:   " · ",
			width: 40,
			want:  "abc",
		},
		{
			name:  "all items fit on one line",
			items: []string{"aa", "bb", "cc"},
			sep:   " · ",
			width: 40,
			want:  "aa · bb · cc",
		},
		{
			name:  "wraps when next item would overflow",
			items: []string{"aaa", "bbb", "ccc"},
			sep:   " · ",
			width: 10,
			want:  "aaa · bbb\nccc",
		},
		{
			name:  "zero width falls back to single line",
			items: []string{"a", "b"},
			sep:   " · ",
			width: 0,
			want:  "a · b",
		},
		{
			name:  "item longer than width still placed alone",
			items: []string{"aaaaaaaaaa", "bb"},
			sep:   " · ",
			width: 4,
			want:  "aaaaaaaaaa\nbb",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := packLines(tc.items, tc.sep, tc.width)
			if got != tc.want {
				t.Errorf("packLines(%v, %q, %d)\n  got:  %q\n  want: %q",
					tc.items, tc.sep, tc.width, got, tc.want)
			}
		})
	}
}

// An archived repo stays in Top repositories, labelled; the others are not.
func TestRenderTopReposLabelsArchived(t *testing.T) {
	repos := []github.Repo{
		{Name: "live", Stars: 30},
		{Name: "frozen", Stars: 20, IsArchived: true},
		{Name: "other", Stars: 10},
	}
	out := ansi.Strip(renderTopRepos(repos, 80))
	for _, want := range []string{"★  live", "★  Archived frozen", "★  other"} {
		if !strings.Contains(out, want) {
			t.Errorf("top repos missing %q:\n%s", want, out)
		}
	}
}

// A Top repositories row fits the width it is given: a long archived name
// is truncated with an ellipsis after its tag instead of running past the
// edge, where the viewport would cut it without one.
func TestRenderTopReposFitsAvailableWidth(t *testing.T) {
	long := strings.Repeat("n", 65)
	repos := []github.Repo{
		{Name: long, Stars: 100, IsArchived: true},
		{Name: long, Stars: 10},
		{Name: "c", Stars: 1},
	}
	const available = 76
	for i, l := range strings.Split(ansi.Strip(renderTopRepos(repos, available)), "\n") {
		if w := cellWidth(l); w > available {
			t.Errorf("line %d is %d cells, available is %d: %q", i, w, available, l)
		}
	}
	if out := ansi.Strip(renderTopRepos(repos, available)); !strings.Contains(out, "Archived nnn") || !strings.Contains(out, "…") {
		t.Errorf("long archived name should keep its tag and end in an ellipsis:\n%s", out)
	}
}
