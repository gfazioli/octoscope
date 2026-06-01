package ui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// whatsNewItem is a single highlight line: a short bold title and an
// optional wrapped description.
type whatsNewItem struct {
	title string
	desc  string
}

// whatsNewEntry is the bundled "What's new" content for one release.
type whatsNewEntry struct {
	headline string
	items    []whatsNewItem
}

// whatsNew maps a version (matching main.version, no leading "v") to its
// bundled highlights. The What's new tab shows ONLY the running
// version's entry — if the running version isn't here (a dev build, or
// a release where this wasn't updated) the tab falls back to a link.
//
// RELEASE CHECKLIST: add an entry for each new version here, mirroring
// the GitHub release notes' headline points. Keep it short — 3-5 lines.
var whatsNew = map[string]whatsNewEntry{
	"0.16.0": {
		headline: "Support octoscope, and never miss what changed.",
		items: []whatsNewItem{
			{
				title: "Sponsor splash at launch",
				desc:  "A quick prompt to support octoscope's development. Press o to open the Sponsors page, c to copy the link, or any key to dismiss. Suppressed under --public-only; opt out with show_sponsor = false or --no-sponsor.",
			},
			{
				title: "This “What's new” tab",
				desc:  "See the highlights of the version you're running without leaving the terminal — jump here any time with 6.",
			},
			{
				title: "Keyboard-shortcut overlay",
				desc:  "Press ? on any tab for a full keymap, grouped by area — no need to leave the app to remember a binding.",
			},
		},
	},
}

// renderWhatsNewTab draws the What's new tab body: the running version's
// bundled highlights (or a fallback link) followed by a sponsor section.
// `version` is main.version (no leading "v"); `available` is the content
// width for wrapping.
func renderWhatsNewTab(version string, available int) string {
	wrapW := available
	if wrapW > 72 {
		wrapW = 72
	}
	if wrapW < 20 {
		wrapW = 20
	}

	var b strings.Builder
	b.WriteString(boldStyle.Foreground(colAccent).Render("What's new in v" + version))
	b.WriteString("\n\n")

	if entry, ok := whatsNew[version]; ok {
		if entry.headline != "" {
			b.WriteString(mutedStyle.Width(wrapW).Render(entry.headline))
			b.WriteString("\n\n")
		}
		for i, it := range entry.items {
			if i > 0 {
				b.WriteString("\n\n")
			}
			b.WriteString(boldStyle.Foreground(colAccent).Render("• ") + valueStyle.Render(it.title))
			if it.desc != "" {
				// Wrap to wrapW-2: indentBlock prepends 2 spaces to every
				// line, so the wrapped body must be 2 cells narrower to
				// keep the indented block inside the content budget.
				wrapped := lipgloss.NewStyle().Width(wrapW - 2).Render(it.desc)
				b.WriteString("\n" + indentBlock(mutedStyle.Render(wrapped), "  "))
			}
		}
	} else {
		// Running version has no bundled highlights (dev build, or the
		// table wasn't updated this release). Don't show stale notes —
		// point at the source of truth instead.
		b.WriteString(mutedStyle.Width(wrapW).Render("Release highlights for this version aren't bundled."))
		b.WriteString("\n")
		// The URL is left unwrapped on purpose so it stays copy-pasteable.
		b.WriteString(mutedStyle.Render("See ") + valueStyle.Render("https://github.com/gfazioli/octoscope/releases"))
	}

	// Sponsor section — the persistent home for the ask the launch
	// splash makes transiently. Same URL; o/c are wired in the What's
	// new tab's key handler (model.go).
	b.WriteString("\n\n")
	b.WriteString(tabRuleStyle.Render(strings.Repeat("─", wrapW)))
	b.WriteString("\n\n")
	b.WriteString(boldStyle.Foreground(colAccent).Render("♥  Support octoscope"))
	b.WriteString("\n")
	b.WriteString(mutedStyle.Width(wrapW).Render("If octoscope is useful to you, please consider sponsoring:"))
	b.WriteString("\n")
	// URL left unwrapped so it stays copy-pasteable.
	b.WriteString(valueStyle.Render(sponsorURL))
	b.WriteString("\n\n")
	b.WriteString(keyHints("o", "open", "c", "copy"))

	return b.String()
}

// indentBlock prefixes every line of s with indent. Used to inset
// wrapped descriptions under their bullet.
func indentBlock(s, indent string) string {
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = indent + l
	}
	return strings.Join(lines, "\n")
}
