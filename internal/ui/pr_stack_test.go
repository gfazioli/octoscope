package ui

import (
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
