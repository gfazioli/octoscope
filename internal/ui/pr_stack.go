package ui

import (
	"fmt"
	"strings"

	"github.com/gfazioli/octoscope/internal/github"
)

// prDetailStack renders the "Stack" section of the PR drill-in (#99):
// the layers of the stacked pull request this one belongs to, in
// order from the base branch up, with this one marked. It answers
// "what am I sitting on, and has it landed?" — and what waits on top.
// "" when the PR is not part of a stack, which is most of them.
func prDetailStack(s *github.PRStack, current, width int) string {
	if s == nil || len(s.Entries) == 0 {
		return ""
	}
	where := fmt.Sprintf("%d of %d", s.Position, s.Size)
	if s.BaseRefName != "" {
		where += " · onto " + s.BaseRefName
	}
	lines := []string{subSectionTitleStyle.Render("Stack") + "   " + mutedStyle.Render(where)}
	for _, e := range s.Entries {
		lines = append(lines, prStackRow(e, e.Number == current && current != 0, width))
	}
	// A stack taller than the fetch cap: the layers past it exist, and
	// the count says so rather than letting the list end as if it were
	// the whole stack.
	if more := s.Size - len(s.Entries); more > 0 {
		lines = append(lines, mutedStyle.Render(fmt.Sprintf("    +%d more", more)))
	}
	return strings.Join(lines, "\n")
}

// prStackRow is one layer: its position, its state, its number and
// title linked to the pull request. The viewed PR is marked with the
// accent pointer and a bold title.
func prStackRow(e github.PRStackEntry, isCurrent bool, width int) string {
	marker := "  "
	if isCurrent {
		marker = accentStyle.Render("▸ ")
	}
	pos := mutedStyle.Render(fmt.Sprintf("%2d", e.Position))
	if e.Number == 0 {
		return marker + pos + "  " + mutedStyle.Render("a pull request you can't see")
	}
	state := prStackState(e)
	num := valueStyle.Render(fmt.Sprintf("#%d", e.Number))
	// Two for the marker, two for the position, the gaps, the state
	// column (8) and the number; the title takes what is left.
	titleW := width - 2 - 2 - 2 - 8 - 2 - len(fmt.Sprintf("#%d", e.Number)) - 2 - 2
	if titleW < 10 {
		titleW = 10
	}
	title := truncate(e.Title, titleW)
	if isCurrent {
		title = boldStyle.Render(title)
	}
	return marker + pos + "  " + state + "  " + num + "  " + githubHyperlink(e.URL, title)
}

// prStackState is a layer's state as one fixed-width word, in the
// colours the drill-in's state chip uses for the same states.
func prStackState(e github.PRStackEntry) string {
	const w = 8
	switch {
	case e.State == "MERGED":
		return boldStyle.Foreground(colAccent).Render(padRight("merged", w))
	case e.State == "CLOSED":
		return errorTextStyle.Render(padRight("closed", w))
	case e.IsDraft:
		return mutedStyle.Render(padRight("draft", w))
	case e.State == "OPEN":
		return okStyle.Render(padRight("open", w))
	default:
		return mutedStyle.Render(padRight(strings.ToLower(e.State), w))
	}
}
