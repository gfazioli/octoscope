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
	"0.29.0": {
		headline: "The things you still opened a browser for.",
		items: []whatsNewItem{
			{
				title: "Gists, with the code in them",
				desc:  "Your gists, newest first — and enter opens the file contents, syntax-highlighted, because a gist is a snippet and reading it is the point. c copies the code rather than the link. An untitled gist is listed by its first filename instead of its hash, and --public-only never asks for the secret ones, so not even the count says how many exist.",
			},
			{
				title: "Activity has a second half: what you actually did",
				desc:  "←/→ switches the heatmap for a feed — pushes, pull requests, reviews, issues, releases, across every repository at once. Review and comment traffic on one subject folds into a single row with a count, so a heavily-reviewed pull request no longer buries the week; anything that changed state, an approval included, always keeps its own line. It says the span it actually covers rather than implying it is a history.",
			},
			{
				title: "Who funds your work, and who you fund",
				desc:  "Sponsors received and given, by name, with GitHub's own monthly estimate for your own account. There is no tier, date or amount, and no way to tell a public sponsor from a private one — all of that needs a scope octoscope will not ask you for — so --public-only drops the section rather than guessing which names are safe to draw.",
			},
			{
				title: "Your real GitHub inbox",
				desc:  "Mentions, review requests, assignments, subscriptions and CI activity, unread and newest first — GitHub does not return them in time order, so octoscope sorts them. s cuts through the noise: three quarters of an inbox is usually a workflow reporting success. Read-only like everything else, so enter takes you to the thread and GitHub marks it read. It needs a classic token, which is GitHub's constraint rather than ours, and the tab says so.",
			},
		},
	},
	"0.28.0": {
		headline: "The capability axis stops taking a workflow at its word.",
		items: []whatsNewItem{
			{
				title: "Reusable-workflow chains are followed",
				desc:  "A fork-triggered workflow that holds nothing, calling one that reads a secret, used to be two innocent files. It is one path to your secrets, and the scan now reads it as one — naming the trigger and the caller that carries it in. Power travels the other way: a called workflow holds what its caller granted, not what it declares, so a callee whose caller hands over nothing is no longer credited with anything.",
			},
			{
				title: "The permission a workflow never mentions",
				desc:  "A workflow that declares no permissions runs with your repository's default, which an owner can widen to read/write — so the file can hold write access it never names. octoscope now reads that setting and joins it to the file. Where it cannot, the report says so instead of quietly deciding the workflow holds nothing.",
			},
			{
				title: "Secrets found by structure, and two more untrusted triggers",
				desc:  "Secret detection now walks parsed YAML rather than matching text, so alternate spellings of secrets: inherit no longer slip past and a commented-out reference no longer counts. The list of events an outsider can cause gained issues — anyone can open one, and the run gets your token and secrets.",
			},
			{
				title: "The evidence is ranked",
				desc:  "The scan report's scored findings now read heaviest first, so what drove the verdict is the first thing you see rather than something to find by comparing every number.",
			},
		},
	},
	"0.27.0": {
		headline: "The integrity scan stops describing, and starts noticing.",
		items: []whatsNewItem{
			{
				title: "What changed since the last scan",
				desc:  "Every scan now records a fingerprint of the repository's auto-execution surface, so the next one reports what moved: a file that auto-executes appeared, an existing one changed, or a branch tip that used to be signed no longer is. It survives renames and re-obfuscation, because a variant still has to appear. The first scan of a repo says so rather than passing for a clean comparison.",
			},
			{
				title: "What a compromise could reach",
				desc:  "Workflow permissions and triggers, self-hosted runners, write deploy keys and off-platform webhooks. Holding power is not a finding — a release workflow with contents:write on a tag push is normal. What scores is power reachable from untrusted input, such as a pull_request_target workflow holding your secrets. Checks needing admin scope fail open, and the report names the ones it could not run.",
			},
			{
				title: "A cross-repo push burst now counts as evidence",
				desc:  "The timing signal that spots a worm fanning out across your repos is wired into the scan at last: recency-gated, and only ever corroborating a repo that already scored. A push burst on its own is a Tuesday.",
			},
		},
	},
	"0.26.0": {
		headline: "Improvements & polish — a batch of long-standing backlog items.",
		items: []whatsNewItem{
			{
				title: "See the whole Checks list",
				desc:  "In a repo or PR drill-in, press c to expand the Checks section past the first 8 rows and back — handy on busy repos that run dozens of checks.",
			},
			{
				title: "Preview themes without guessing",
				desc:  "octoscope --theme list prints all seven palettes with a colour swatch each (names only under NO_COLOR), so you can pick one without trial-and-error.",
			},
			{
				title: "Accent colour & sponsor splash in Settings",
				desc:  "The in-app settings panel (,) now edits accent_color and the sponsor splash toggle too — the last two config keys that were file-only.",
			},
			{
				title: "Honest coverage & clearer empty states",
				desc:  "A partial integrity scan now says so instead of reading as a clean all-clear; the footer stops advertising keys that don't work in modals or while filtering; and a filter that hides every PR/issue tells you how to clear it.",
			},
		},
	},
	"0.25.0": {
		headline: "A red CI dot now tells you what broke.",
		items: []whatsNewItem{
			{
				title: "CI insight in the repo drill-in",
				desc:  "The Repos tab has always shown a CI rollup dot per repository — green, red, amber — and stopped there. Open a repo now (Enter, or space for the action menu) and a Checks section lists the individual checks on the default branch's tip, failures floated to the top so a red job in a twenty-job matrix can't hide behind the passing ones.",
			},
			{
				title: "Each check links to its run",
				desc:  "Check names are OSC 8 terminal hyperlinks straight to the run on GitHub — click them where your terminal supports it, and they stay plain text everywhere else. Only https github.com targets are linked: a third-party CI provider reporting through the Checks API renders as text rather than becoming a one-click trip off-platform.",
			},
		},
	},
	"0.24.2": {
		headline: "A small footer polish.",
		items: []whatsNewItem{
			{
				title: "Context-aware footer hints",
				desc:  "Inside a drill-in (repo, PR or issue detail, or the integrity scan) the bottom bar now advertises only the keys that actually work there — esc back · r refresh · q quit — instead of the list-level tab-switch / public / settings / help hotkeys that the drill-in swallows.",
			},
		},
	},
	"0.24.1": {
		headline: "A reliability & security patch.",
		items: []whatsNewItem{
			{
				title: "Repo drill-in survives a flaky star history",
				desc:  "A failed or restricted star-history fetch — GitHub now gates the stargazers endpoint behind a scoped token — no longer aborts the whole repo detail view. It degrades to just hiding the 12-month sparkline; description, release, commits and issue/PR previews still load.",
			},
			{
				title: "Standard-library security fix",
				desc:  "Built against Go 1.25.12, picking up the crypto/tls fix for GO-2026-5856.",
			},
		},
	},
	"0.24.0": {
		headline: "octoscope outside the TUI — pipe it into your scripts.",
		items: []whatsNewItem{
			{
				title: "Non-interactive --plain and --json",
				desc:  "octoscope --plain prints a static, human-readable summary and exits; --json emits the same data as JSON for piping into jq, cron jobs or a shell status-line. Neither opens the TUI. Both honour --public-only and the usual auth cascade.",
			},
			{
				title: "A stable JSON contract",
				desc:  "The --json output is versioned (schema_version: 1) and documented, so scripts can depend on its shape: fields are added additively, breaking changes bump the version, and every list is always an array. See the README's Scripting section for the full schema.",
			},
		},
	},
	"0.23.0": {
		headline: "Polish & reliability — kinder errors, honest notices, saved views.",
		items: []whatsNewItem{
			{
				title: "Actionable auth errors",
				desc:  "An expired or revoked token now says so and names the fix for where the token came from: $GITHUB_TOKEN points at the regenerate URL, a gh CLI login points at gh auth refresh. Under-scoped tokens name the missing scopes.",
			},
			{
				title: "Watched entries never vanish silently",
				desc:  "A watch_repos entry that no longer resolves (renamed, deleted, gone private) surfaces as a small notice under the Repos tab naming the stale refs, instead of disappearing without a trace. Transient network blips still pass quietly.",
			},
			{
				title: "Your view, every launch",
				desc:  "Three new config keys — default_sort, default_work_filter and default_star_history — seed the list tabs' sort, the Repos work filter and the star-history mode at startup, so octoscope opens the way you like it.",
			},
		},
	},
	"0.22.0": {
		headline: "Respect NO_COLOR — a calmer, monochrome octoscope.",
		items: []whatsNewItem{
			{
				title: "NO_COLOR / --no-color",
				desc:  "octoscope now honours the NO_COLOR convention: set NO_COLOR in your environment, or pass --no-color, and it switches to the zero-chroma monochrome theme, overriding --theme and the config. It's per-run only — your saved theme and accent stay untouched and come back the moment NO_COLOR is unset.",
			},
		},
	},
	"0.21.0": {
		headline: "Pin the issues you keep coming back to.",
		items: []whatsNewItem{
			{
				title: "Pinned issues",
				desc:  "Press P on an issue row (or pick Pin from the action menu) to pin or unpin it. Pinned issues stick to the top of the Issues tab in the order you pin them, and they compose with the sort cycle (s) and the / search just like pinned repos. The set is saved to pinned_issues in your config, so the next launch brings them right back.",
			},
		},
	},
	"0.20.2": {
		headline: "Hardening & housekeeping — a stronger, fresher build.",
		items: []whatsNewItem{
			{
				title: "Stronger terminal-injection defense",
				desc:  "The sanitizer that scrubs GitHub-sourced text before octoscope paints it now also strips UTF-8-encoded C1 control sequences (the 8-bit CSI/OSC/DCS introducers), not just the 7-bit ESC forms — on both the fetch and markdown paths.",
			},
			{
				title: "Fresher, safer build",
				desc:  "Built on a patched Go toolchain (closing several standard-library security advisories) with refreshed dependencies, and CI now scans dependencies for known vulnerabilities on every change.",
			},
		},
	},
	"0.20.1": {
		headline: "A small polish on the support links.",
		items: []whatsNewItem{
			{
				title: "Buy me a coffee on this tab",
				desc:  "The Support octoscope section here now offers the one-off \"buy me a coffee\" tip (press b) alongside recurring GitHub Sponsors (press o), mirroring the launch splash.",
			},
		},
	},
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
	// URLs left unwrapped so they stay copy-pasteable. Recurring
	// GitHub Sponsors first, then the one-off "buy me a coffee" tip —
	// same pairing the launch splash offers.
	key := func(k, label string) string {
		return boldStyle.Foreground(colAccent).Render(k) + "  " + mutedStyle.Render(label)
	}
	b.WriteString(key("o", "Sponsor on GitHub  (recurring)"))
	b.WriteString("\n")
	b.WriteString("   " + hyperlink(sponsorURL, valueStyle.Render(sponsorURL)))
	b.WriteString("\n\n")
	b.WriteString(key("b", "Buy me a coffee  (one-off)"))
	b.WriteString("\n")
	b.WriteString("   " + hyperlink(coffeeURL, valueStyle.Render(coffeeURL)))
	b.WriteString("\n\n")
	b.WriteString(keyHints("o", "sponsor", "b", "coffee", "c", "copy"))

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
