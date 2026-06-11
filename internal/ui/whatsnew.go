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
	"0.20.0": {
		headline: "Integrity — scan your repos for the supply-chain worm.",
		items: []whatsNewItem{
			{
				title: "Supply-chain integrity scan",
				desc:  "Open a repo's action menu (space) and pick Security scan to check it for the Shai-Hulud / Miasma class of attack — an implant pushed to your repos that auto-runs when you open them in an AI editor or install them. It scores by what matters (auto-execution surface, oversized/obfuscated payloads, forged or unsigned commit tips), not by a single filename, so renamed variants still trip it. Read-only: it explains the findings and hands you a fix script plus the right revoke links — it never touches the repo.",
			},
			{
				title: "Buy me a coffee",
				desc:  "The launch splash now offers a one-off donation (press b) alongside recurring GitHub Sponsors (press o).",
			},
		},
	},
	"0.19.0": {
		headline: "Freshness & correctness — stay current, count everything.",
		items: []whatsNewItem{
			{
				title: "Update notice",
				desc:  "octoscope now checks on launch (and hourly) whether a newer release is out, and shows a quiet line under the banner with the right upgrade command for how you installed it — brew, go install, gh extension or download. It never self-updates. Disable with check_for_updates = false.",
			},
			{
				title: "Accurate totals past 100 repos",
				desc:  "The dashboard used to count only your first 100 repositories, under-counting stars, forks, open issues/PRs and language bytes on prolific accounts. It now paginates through them all (up to 500), so the aggregates — and the Repos list — are complete.",
			},
		},
	},
	"0.18.0": {
		headline: "Insight — see further without leaving the terminal.",
		items: []whatsNewItem{
			{
				title: "Cumulative star history",
				desc:  "Inside a repo's detail view, press v to switch the 12-month star sparkline between weekly density and a cumulative growth curve (star-history.com style).",
			},
			{
				title: "Rate-limit details on %",
				desc:  "A per-resource breakdown of every REST + GraphQL budget — used, remaining, reset — straight from GitHub's free /rate_limit endpoint. The footer chip tells you how you're doing; the panel tells you why.",
			},
			{
				title: "Work filters in Repos",
				desc:  "Press w to cycle quick presets: PRs open, CI broken, stale 90d. Composes with the / search and spans pinned, owned and watched sections alike. esc clears.",
			},
		},
	},
	"0.17.0": {
		headline: "Hardening & polish — lighter, safer, clickable.",
		items: []whatsNewItem{
			{
				title: "Lighter on the GitHub API",
				desc:  "Auto-refresh now keeps exactly one timer no matter how often you refresh or change the interval — it no longer speeds up (and burns rate-limit budget) the more you use it.",
			},
			{
				title: "Clickable links",
				desc:  "The Sponsors and release-notes URLs are now OSC 8 terminal hyperlinks — click them where your terminal supports it; plain text everywhere else.",
			},
			{
				title: "Sturdier",
				desc:  "Transient GitHub 5xx errors (the occasional 502 on a busy account) are now retried automatically before showing a clean message — no more raw HTML error dump. A zero / negative / tiny refresh_interval is also floored so it can't peg the API.",
			},
			{
				title: "Search niceties",
				desc:  "Pasting into the list filter (/) now works, and backspace is multibyte-safe. Diffs respect monochromatic themes too.",
			},
		},
	},
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

// releasesURL is the GitHub Releases index — the full, per-version
// notes that the in-app bundled highlights only summarise.
const releasesURL = "https://github.com/gfazioli/octoscope/releases"

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

		// We surface only the running version's highlights — point at the
		// full, per-version notes on GitHub. Left unwrapped so the URL
		// stays copy-pasteable.
		b.WriteString("\n\n")
		b.WriteString(mutedStyle.Render("Full release notes → ") + hyperlink(releasesURL, valueStyle.Render(releasesURL)))
	} else {
		// Running version has no bundled highlights (dev build, or the
		// table wasn't updated this release). Don't show stale notes —
		// point at the source of truth instead.
		b.WriteString(mutedStyle.Width(wrapW).Render("Release highlights for this version aren't bundled."))
		b.WriteString("\n")
		// The URL is left unwrapped on purpose so it stays copy-pasteable.
		b.WriteString(mutedStyle.Render("See ") + hyperlink(releasesURL, valueStyle.Render(releasesURL)))
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
	b.WriteString(hyperlink(sponsorURL, valueStyle.Render(sponsorURL)))
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
