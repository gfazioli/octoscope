package ui

import (
	"regexp"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/gfazioli/octoscope/internal/github"
)

func TestPRDetailStack(t *testing.T) {
	_ = applyTheme("octoscope", "")
	stack := &github.PRStack{
		Position: 3, Size: 6, BaseRefName: "main",
		Entries: []github.PRStackEntry{
			{Position: 1, Number: 330, Title: "Rebase stacks onto the latest remote trunk", State: "MERGED"},
			{Position: 2},
			{Position: 3, Number: 343, Title: "Minor docs updates", State: "OPEN"},
			{Position: 4, Number: 307, Title: "Merge Stacked PRs", State: "OPEN", IsDraft: true},
			{Position: 5, Number: 321, Title: "Async merge API docs", State: "CLOSED"},
		},
	}

	out := ansi.Strip(prDetailStack(stack, 343, 100))
	lines := strings.Split(out, "\n")
	if lines[0] != "Stack   3 of 6 · onto main" {
		t.Errorf("heading = %q", lines[0])
	}
	want := []struct{ prefix, has string }{
		{"   1  merged", "#330"},
		{"   2  a pull request you can't see", ""},
		{"▸  3  open", "#343"},
		{"   4  draft", "#307"},
		{"   5  closed", "#321"},
	}
	for i, w := range want {
		l := lines[i+1]
		if !strings.HasPrefix(l, w.prefix) || !strings.Contains(l, w.has) {
			t.Errorf("row %d = %q, want it to start %q and carry %q", i+1, l, w.prefix, w.has)
		}
	}
	// Five listed of six: the sixth layer exists and is counted, not
	// dropped as if the list were the whole stack.
	if strings.TrimSpace(lines[len(lines)-1]) != "+1 more" {
		t.Errorf("last line = %q, want +1 more", lines[len(lines)-1])
	}
	if strings.Count(out, "▸") != 1 {
		t.Errorf("want exactly one marked layer:\n%s", out)
	}
	for i, l := range lines {
		if w := ansi.StringWidth(l); w > 100 {
			t.Errorf("line %d is %d cells, past its 100: %q", i, w, l)
		}
	}

	t.Run("nothing outside a stack", func(t *testing.T) {
		if got := prDetailStack(nil, 1, 100); got != "" {
			t.Errorf("got %q", got)
		}
	})

	t.Run("in the drill-in body", func(t *testing.T) {
		pd := PRDetailModel{}.Open(github.PullRequest{Number: 343, URL: "https://github.com/github/gh-stack/pull/343"})
		pd = pd.applyFetched(&github.PRDetail{Number: 343, Title: "Minor docs updates", State: "OPEN", Stack: stack}, nil)
		if body := ansi.Strip(pd.computeBody(100)); !strings.Contains(body, "Stack   3 of 6") || !strings.Contains(body, "▸  3") {
			t.Errorf("the drill-in does not paint the section:\n%s", body)
		}
	})
}

// What GitHub answers is not always consistent; the section must not
// turn that into a claim.
func TestPRDetailStackInconsistentAnswers(t *testing.T) {
	_ = applyTheme("octoscope", "")
	two := []github.PRStackEntry{
		{Position: 1, Number: 10, Title: "first", State: "MERGED"},
		{Position: 2, Number: 11, Title: "second", State: "OPEN"},
	}
	t.Run("a position the stack cannot hold is not stated", func(t *testing.T) {
		// The viewed PR is not among the layers listed, so only
		// GitHub's position could be stated, and it is impossible.
		out := ansi.Strip(prDetailStack(&github.PRStack{Position: 5, Size: 2, Entries: two}, 99, 100))
		if strings.Contains(out, "5 of 2") || !strings.Contains(out, "2 layers") {
			t.Errorf("got:\n%s", out)
		}
	})
	t.Run("a size below the layers listed takes the list's count", func(t *testing.T) {
		out := ansi.Strip(prDetailStack(&github.PRStack{Position: 2, Size: 1, Entries: two}, 11, 100))
		if !strings.Contains(out, "2 of 2") || strings.Contains(out, "more") {
			t.Errorf("got:\n%s", out)
		}
	})
	t.Run("the marker follows the PR, not the position", func(t *testing.T) {
		// GitHub says position 1, but the viewed PR (#11) is the layer
		// listed at position 2: the marker goes where #11 is.
		lines := strings.Split(ansi.Strip(prDetailStack(&github.PRStack{Position: 1, Size: 2, Entries: two}, 11, 100)), "\n")
		if !strings.HasPrefix(lines[2], "▸") || strings.HasPrefix(lines[1], "▸") {
			t.Errorf("rows = %q / %q, want #11 marked", lines[1], lines[2])
		}
		// ...and the heading follows the marker, not GitHub's position.
		if !strings.Contains(lines[0], "2 of 2") {
			t.Errorf("heading = %q, want the marked row's position", lines[0])
		}
	})
	t.Run("a title with a newline stays one row", func(t *testing.T) {
		out := ansi.Strip(prDetailStack(&github.PRStack{Position: 1, Size: 1, Entries: []github.PRStackEntry{
			{Position: 1, Number: 10, Title: "two\nlines\tand a tab", State: "OPEN"},
		}}, 10, 100))
		if lines := strings.Split(out, "\n"); len(lines) != 2 || !strings.Contains(lines[1], "two lines and a tab") {
			t.Errorf("got %q", out)
		}
	})
	t.Run("narrow terminals, long base names", func(t *testing.T) {
		s := &github.PRStack{Position: 1, Size: 2, BaseRefName: strings.Repeat("release/very-long-name-", 3), Entries: two}
		for _, width := range []int{40, 60} {
			for i, l := range strings.Split(ansi.Strip(prDetailStack(s, 11, width)), "\n") {
				if w := ansi.StringWidth(l); w > width-2 {
					t.Errorf("width %d, line %d is %d cells: %q", width, i, w, l)
				}
			}
		}
	})
}

func TestPRsTableStackMarker(t *testing.T) {
	_ = applyTheme("octoscope", "")
	out := ansi.Strip(renderPRsTable([]github.PullRequest{
		{Number: 307, Title: "Merge Stacked PRs", Repo: "github/gh-stack", StackPosition: 4, StackSize: 5},
		{Number: 9, Title: "On its own", Repo: "o/r"},
	}, 0, PRsSortUpdated, 0))
	lines := strings.Split(out, "\n")
	if !strings.Contains(lines[2], "4/5 Merge Stacked PRs") {
		t.Errorf("stacked row = %q, want the 4/5 marker ahead of the title", lines[2])
	}
	if regexp.MustCompile(`\d+/\d+ `).MatchString(lines[3]) {
		t.Errorf("row outside any stack = %q, want no marker", lines[3])
	}
	if a, b := ansi.StringWidth(lines[2]), ansi.StringWidth(lines[3]); a != b {
		t.Errorf("rows are %d and %d cells wide; the marker must come out of the title's column", a, b)
	}
	// A two-digit placement takes its own width out of the title too.
	wide := ansi.Strip(renderPRsTable([]github.PullRequest{
		{Number: 1, Title: strings.Repeat("t", 60), Repo: "o/r", StackPosition: 12, StackSize: 15},
		{Number: 2, Title: strings.Repeat("t", 60), Repo: "o/r"},
	}, 0, PRsSortUpdated, 0))
	wl := strings.Split(wide, "\n")
	if !strings.Contains(wl[2], "12/15 ") || ansi.StringWidth(wl[2]) != ansi.StringWidth(wl[3]) {
		t.Errorf("12/15 row = %q (%d cells) against %d", wl[2], ansi.StringWidth(wl[2]), ansi.StringWidth(wl[3]))
	}
}
