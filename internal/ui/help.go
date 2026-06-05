package ui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// HelpModel is the keyboard-shortcut overlay opened with `?`. It's a
// read-only modal following the same idiom as SettingsModel /
// SponsorModel: the root model opens it, it absorbs keys while open
// (any key dismisses, ctrl+c still quits), and it renders centered in
// the content area — or over the loading screen if `?` is pressed
// before the first fetch lands, so open-in-Update stays aligned with
// visible-in-View.
//
// The bindings here are a quick in-app reference for the common
// shortcuts (the rotating footer hints only show a few at a time, and
// `--help` is unreachable inside the alt-screen). A few context-specific
// keys (f in the PR drill-in, pgup/pgdn paging) are intentionally left
// to the README's full table. Keep helpGroups in sync with the real key
// handlers in model.go and the per-tab sub-models.
type HelpModel struct {
	open bool
}

// IsOpen reports whether the help overlay is visible.
func (h HelpModel) IsOpen() bool { return h.open }

// Open returns a visible help overlay.
func (h HelpModel) Open() HelpModel { return HelpModel{open: true} }

// Close returns a dismissed help overlay (zero value).
func (h HelpModel) Close() HelpModel { return HelpModel{} }

type helpBinding struct {
	keys string
	desc string
}

var helpGroups = []struct {
	title    string
	bindings []helpBinding
}{
	{"Navigate", []helpBinding{
		{"1-6", "jump to tab"},
		{"tab / shift+tab", "cycle tabs"},
		{"up / down", "move cursor"},
		{"g / G", "top / bottom"},
	}},
	{"Lists (Repos / PRs / Issues)", []helpBinding{
		{"s", "cycle sort"},
		{"/", "filter by substring"},
		{"w", "work filter (Repos)"},
		{"enter", "open details"},
		{"o", "open in browser"},
		{"c", "copy URL"},
		{"P", "pin repo (Repos)"},
	}},
	{"App", []helpBinding{
		{"r", "refresh now"},
		{"p", "toggle public-only"},
		{",", "settings"},
		{"space", "action menu"},
		{"%", "rate-limit details"},
		{"esc", "close / clear filter"},
		{"?", "this help"},
		{"q", "quit"},
	}},
}

// View renders the centered, accent-bordered overlay. Built inline (like
// the action menu) so it tracks the live theme accent. Returns "" when
// closed.
func (h HelpModel) View(width int) string {
	if !h.open {
		return ""
	}

	// Align the description column past the widest key label.
	keyW := 0
	for _, g := range helpGroups {
		for _, b := range g.bindings {
			if w := lipgloss.Width(b.keys); w > keyW {
				keyW = w
			}
		}
	}

	lines := []string{boldStyle.Foreground(colAccent).Render("Keyboard shortcuts")}
	for _, g := range helpGroups {
		// sectionTitleStyle carries MarginTop(1), which already supplies
		// the blank line above each group — don't prepend another "".
		lines = append(lines, sectionTitleStyle.Render(g.title))
		for _, b := range g.bindings {
			gap := strings.Repeat(" ", keyW-lipgloss.Width(b.keys)+2)
			lines = append(lines,
				"  "+boldStyle.Foreground(colAccent).Render(b.keys)+gap+mutedStyle.Render(b.desc))
		}
	}
	lines = append(lines, "", mutedStyle.Render("esc or any key to close"))

	panel := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(colAccent).
		Padding(1, 3).
		Render(strings.Join(lines, "\n"))

	if width <= 0 {
		return panel
	}
	return lipgloss.PlaceHorizontal(width, lipgloss.Center, panel)
}
