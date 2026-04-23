package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/gfazioli/octoscope/internal/github"
)

// View renders the current model. Layout for v0.2.0:
//
//  1. Banner (app name + version, accent-bordered top-left)
//  2. Profile card (bordered box: name · login · bio · meta)
//  3. Social section (followers / following / stars received)
//  4. Activity section (PRs / merged / issues / commits) + languages bar
//  5. Operational section (repos / forks / open issues / open PRs)
//  6. Network section (organizations · social links)
//  7. Footer bar (hotkeys left, freshness + version right, anchored
//     to the bottom of the terminal when there's room)
//
// The whole output is wrapped in outerStyle so content has breathing
// room from the terminal edges.
func (m Model) View() string {
	// Loading / error states are rendered without the full dashboard
	// chrome so the user isn't staring at an empty profile card.
	if m.loading && m.stats == nil {
		return outerStyle.Render(
			renderBanner(m.version) + "\n\n" +
				mutedStyle.Render("Loading…") + "\n\n" +
				mutedStyle.Render("q quit"),
		)
	}
	if m.err != nil && m.stats == nil {
		return outerStyle.Render(
			renderBanner(m.version) + "\n\n" +
				errorStyle.Render("Could not fetch stats") + "\n" +
				mutedStyle.Render(m.err.Error()) + "\n\n" +
				mutedStyle.Render("r retry · q quit"),
		)
	}

	var b strings.Builder
	s := m.stats

	// Usable content width = terminal minus outerStyle's horizontal
	// padding (2 chars on each side). Fall back to 80 only when the
	// first WindowSizeMsg hasn't arrived yet (m.width == 0); a genuinely
	// narrow terminal still gets its real width so content doesn't
	// overflow past the visible edge.
	available := m.width - 4
	if m.width <= 0 {
		available = 80
	} else if available < 20 {
		available = 20
	}

	b.WriteString(renderBanner(m.version))
	b.WriteString("\n")
	b.WriteString(renderProfileCard(s, available))
	b.WriteString("\n")
	b.WriteString(renderSection("Social", m.renderSocial(s, available)) + "\n")
	b.WriteString(renderSection("Activity", m.renderActivity(s, available)) + "\n")
	b.WriteString(renderSection("Operational", m.renderOperational(s, available)) + "\n")
	b.WriteString(renderSection("Network", renderNetwork(s, available)))

	body := b.String()
	footer := renderFooterBar(m)

	// Anchor the footer to the bottom of the terminal when we have
	// more vertical room than the content needs. Falls back to a
	// plain blank line if the window is too small (or height is
	// unknown — first paint before WindowSizeMsg arrives).
	return outerStyle.Render(stackWithBottomFooter(body, footer, m.height))
}

// renderBanner draws the app identity at the top: bold accent
// "octoscope" and a muted version next to it, wrapped in a small
// rounded box.
func renderBanner(version string) string {
	content := "octoscope"
	if version != "" {
		content += mutedStyle.Render("  " + version)
	}
	return bannerStyle.Render(content)
}

// renderProfileCard renders the user's profile (name/login/pronouns,
// bio, company/location/website/age) inside a bordered box so it
// reads as the subject of the dashboard. The card's outer width tracks
// `available` so the border always hugs the terminal's right edge,
// and bio + meta wrap to multiple lines instead of overflowing.
func renderProfileCard(s *github.Stats, available int) string {
	var lines []string

	// Inner width = available minus border (2) and horizontal padding (4).
	inner := available - 6
	if inner < 20 {
		inner = 20
	}
	wrap := lipgloss.NewStyle().Width(inner)

	// First line: name · @login · pronouns · auth badge
	name := s.Name
	if name == "" {
		name = s.Login
	}
	parts := []string{boldStyle.Render(name), mutedStyle.Render("@" + s.Login)}
	if s.Pronouns != "" {
		parts = append(parts, mutedStyle.Render("· ")+s.Pronouns)
	}
	parts = append(parts, authBadge(s.Authenticated))
	lines = append(lines, wrap.Render(strings.Join(parts, "  ")))

	if s.Bio != "" {
		lines = append(lines, wrap.Render(s.Bio))
	}

	if meta := renderMetaRow(s, inner); meta != "" {
		lines = append(lines, meta)
	}

	return profileCardStyle.Width(available).Render(strings.Join(lines, "\n"))
}

// stackWithBottomFooter places `body` at the top and `footer` at the
// bottom of a box of `height` lines (when height > body+footer).
// When we don't know the height yet, renders body + blank line +
// footer inline.
func stackWithBottomFooter(body, footer string, height int) string {
	if height <= 0 {
		return body + "\n\n" + footer
	}
	// outerStyle adds 2 lines of vertical padding (top + bottom); the
	// terminal gives us m.height total, so the content area is
	// height - 2.
	available := height - 2
	bodyLines := lipgloss.Height(body)
	footerLines := lipgloss.Height(footer)
	gap := available - bodyLines - footerLines
	if gap < 1 {
		gap = 1
	}
	return body + strings.Repeat("\n", gap) + footer
}

// ---------- Section scaffolding ----------

// renderSection wraps a body with a small colored title above it. Keeps
// visual hierarchy consistent without resorting to heavy borders (which
// gobble horizontal space on narrow terminals).
func renderSection(title, body string) string {
	return sectionTitleStyle.Render(title) + "\n" + body
}

// ---------- Profile bits ----------

func authBadge(authenticated bool) string {
	if authenticated {
		return okStyle.Render("● authenticated")
	}
	return warnStyle.Render("● unauthenticated")
}

// renderMetaRow renders company, location, website, and member years
// on one or more lines, packing greedily so the visible width stays
// within `width`. Only non-empty fields show up.
func renderMetaRow(s *github.Stats, width int) string {
	var parts []string
	if s.Company != "" {
		parts = append(parts, mutedStyle.Render("🏢")+" "+s.Company)
	}
	if s.Location != "" {
		parts = append(parts, mutedStyle.Render("📍")+" "+s.Location)
	}
	if s.WebsiteURL != "" {
		parts = append(parts, mutedStyle.Render("🔗")+" "+s.WebsiteURL)
	}
	if !s.CreatedAt.IsZero() {
		years := time.Since(s.CreatedAt).Hours() / 24 / 365
		parts = append(parts, mutedStyle.Render(fmt.Sprintf("⏱ %.0f years on GitHub", years)))
	}
	if len(parts) == 0 {
		return ""
	}
	return packLines(parts, "   ", width)
}

// packLines concatenates items with `sep`, wrapping to a new line when
// the next item would exceed `width`. Width is measured via
// lipgloss.Width so ANSI escape sequences don't inflate the count.
// When `width <= 0` (unknown terminal), falls back to a single line.
func packLines(items []string, sep string, width int) string {
	if len(items) == 0 {
		return ""
	}
	if width <= 0 {
		return strings.Join(items, sep)
	}
	sepW := lipgloss.Width(sep)
	var lines []string
	var cur string
	var curW int
	for _, it := range items {
		itW := lipgloss.Width(it)
		if cur == "" {
			cur = it
			curW = itW
			continue
		}
		if curW+sepW+itW > width {
			lines = append(lines, cur)
			cur = it
			curW = itW
			continue
		}
		cur += sep + it
		curW += sepW + itW
	}
	if cur != "" {
		lines = append(lines, cur)
	}
	return strings.Join(lines, "\n")
}

// ---------- Stat sections ----------

func (m Model) renderSocial(s *github.Stats, available int) string {
	return renderCardRow(available, m.pulseMap, []cardSpec{
		{id: "followers", icon: "●", label: "Followers", value: s.Followers},
		{id: "following", icon: "○", label: "Following", value: s.Following},
		{id: "stars", icon: "★", label: "Stars Received", value: s.TotalStars},
	})
}

func (m Model) renderActivity(s *github.Stats, available int) string {
	row := renderCardRow(available, m.pulseMap, []cardSpec{
		{id: "prs_authored", icon: "⎇", label: "PRs Authored", value: s.PRsTotal},
		{id: "prs_merged", icon: "✓", label: "PRs Merged", value: s.PRsMerged},
		{id: "issues_authored", icon: "⚠", label: "Issues Authored", value: s.IssuesAuthored},
		{id: "commits_year", icon: "↻", label: "Commits (yr)", value: s.CommitsLastYear},
	})

	// Derived metric: what share of the user's PRs made it in.
	// Rendered as a small muted line rather than another card so we
	// don't crowd the row. Hidden when the user has no PRs — the
	// "0% of 0" case is noise.
	var extras string
	if s.PRsTotal > 0 {
		rate := float64(s.PRsMerged) / float64(s.PRsTotal) * 100
		extras = "  " + mutedStyle.Render(fmt.Sprintf(
			"%.0f%% of your PRs were merged", rate,
		))
	}

	sections := []string{row}
	if extras != "" {
		sections = append(sections, extras)
	}
	if len(s.Languages) > 0 {
		sections = append(sections, renderLanguages(s.Languages, available))
	}
	return strings.Join(sections, "\n\n")
}

func (m Model) renderOperational(s *github.Stats, available int) string {
	return renderCardRow(available, m.pulseMap, []cardSpec{
		{id: "public_repos", icon: "▣", label: "Public Repos", value: s.PublicRepos},
		{id: "forks_received", icon: "⑂", label: "Forks Received", value: s.ForksReceived},
		{id: "open_issues", icon: "◌", label: "Open Issues", value: s.OpenIssues},
		{id: "open_prs", icon: "⇄", label: "Open PRs", value: s.OpenPRs},
	})
}

func renderNetwork(s *github.Stats, available int) string {
	var lines []string

	const (
		orgLabel    = "Organizations"
		socialLabel = "Social       "
		labelGap    = "  "
	)
	// Values are indented under a fixed-width label column so long
	// lists wrap under the label rather than extending past the
	// terminal right edge.
	valueWidth := available - lipgloss.Width(orgLabel) - lipgloss.Width(labelGap)
	if valueWidth < 20 {
		valueWidth = 20
	}

	if len(s.Organizations) > 0 {
		var logins []string
		for _, o := range s.Organizations {
			logins = append(logins, o.Login)
		}
		packed := packLines(logins, " · ", valueWidth)
		lines = append(lines, mutedStyle.Render(orgLabel)+labelGap+indentContinuation(packed, orgLabel, labelGap))
	}

	if len(s.SocialAccounts) > 0 || s.TwitterUsername != "" {
		var links []string
		seen := map[string]bool{}
		// TwitterUsername is often duplicated by SocialAccounts; dedupe
		// by URL to avoid showing "@me · @me".
		if s.TwitterUsername != "" {
			url := "https://twitter.com/" + s.TwitterUsername
			links = append(links, "@"+s.TwitterUsername)
			seen[url] = true
		}
		for _, sa := range s.SocialAccounts {
			if seen[sa.URL] {
				continue
			}
			label := sa.DisplayName
			if label == "" {
				label = sa.URL
			}
			links = append(links, label)
			seen[sa.URL] = true
		}
		if len(links) > 0 {
			packed := packLines(links, " · ", valueWidth)
			lines = append(lines, mutedStyle.Render(socialLabel)+labelGap+indentContinuation(packed, socialLabel, labelGap))
		}
	}

	if len(lines) == 0 {
		return mutedStyle.Render("(no public organizations or social links)")
	}
	return strings.Join(lines, "\n")
}

// indentContinuation pads every line after the first with spaces equal
// to the label column, so wrapped content lines up under the value
// rather than starting at column zero.
func indentContinuation(body, label, gap string) string {
	if !strings.Contains(body, "\n") {
		return body
	}
	pad := strings.Repeat(" ", lipgloss.Width(label)+lipgloss.Width(gap))
	parts := strings.Split(body, "\n")
	for i := 1; i < len(parts); i++ {
		parts[i] = pad + parts[i]
	}
	return strings.Join(parts, "\n")
}

// ---------- Languages bar ----------

// renderLanguages draws a horizontal bar per top-language with the
// colour GitHub itself assigns to the language. Percentages are
// computed against the total bytes in the top-N set (what we rendered),
// not across every language ever touched — keeps the bars adding up
// to ~100% visually.
func renderLanguages(langs []github.Language, available int) string {
	if len(langs) == 0 {
		return ""
	}
	var total int
	for _, l := range langs {
		total += l.Bytes
	}
	if total == 0 {
		return ""
	}

	var longestName int
	for _, l := range langs {
		if len(l.Name) > longestName {
			longestName = len(l.Name)
		}
	}

	// Line layout: "  <name>  <bar>  <pct>"
	//   leading + gap + gap + percentage column = 2 + 2 + 2 + 6 = 12
	// Remaining width is given to the bar, clamped to [10, 32].
	const (
		minBar = 10
		maxBar = 32
		fixed  = 12
	)
	barWidth := available - longestName - fixed
	if barWidth < minBar {
		barWidth = minBar
	}
	if barWidth > maxBar {
		barWidth = maxBar
	}

	var b strings.Builder
	b.WriteString(mutedStyle.Render("Languages") + "\n")
	for _, l := range langs {
		pct := float64(l.Bytes) / float64(total) * 100
		filled := int(float64(barWidth)*pct/100 + 0.5)
		if filled < 1 && l.Bytes > 0 {
			filled = 1
		}

		barColour := lipgloss.Color(l.Color)
		if l.Color == "" {
			barColour = colMuted
		}
		filledBar := lipgloss.NewStyle().Foreground(barColour).Render(strings.Repeat("█", filled))
		// Pad with invisible spaces instead of a ░ track so the
		// percentage column stays aligned without a visually heavy
		// "empty container" trailing the coloured fill.
		padding := strings.Repeat(" ", barWidth-filled)

		line := fmt.Sprintf(
			"  %-*s  %s%s  %5.1f%%",
			longestName, l.Name, filledBar, padding, pct,
		)
		b.WriteString(line + "\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

// ---------- Stat card ----------

// cardSpec is the payload for one card in a responsive row.
//
//   - id     — stable key used to match against the pulse map when a
//              value changed between refreshes.
//   - icon   — a single Unicode symbol prepended to the label. Geometric
//              shapes only, no emoji (consistent rendering across
//              terminals and fonts).
//   - label  — human-readable label, rendered muted.
//   - value  — the integer displayed below the label.
type cardSpec struct {
	id    string
	icon  string
	label string
	value int
}

// pulseDuration is how long a card shows its "recently changed"
// accent border. Long enough to register, short enough not to hang
// around if several refreshes happen in a row.
const pulseDuration = 2 * time.Second

// renderCardRow lays out N cards sized to fit the available width.
// Wider terminals get a single row; narrow ones reflow onto multiple
// rows while keeping all rows the same length — so 3 cards split into
// 1+1+1 (never 2+1) and 4 cards split into 2+2 or 1+1+1+1. Symmetry
// matters visually: an asymmetric 2+1 reads as a bug.
//
// The card width is a tug-of-war between:
//   - `minCardW`: enough for icon + label like "★ Stars Received"
//   - `maxCardW`: keeps cards from looking empty on ultrawide
//   - `gap = 1`: space between cards (lipgloss.JoinHorizontal gives 0
//     so we factor a +1 per card into the budget).
func renderCardRow(available int, pulseMap map[string]time.Time, specs []cardSpec) string {
	const (
		minCardW = 18
		maxCardW = 26
		gap      = 1
	)
	n := len(specs)
	if n == 0 {
		return ""
	}

	// Walk divisors of n downward (from n itself toward 1) and pick
	// the largest perRow that keeps each card ≥ minCardW. Divisors
	// only — so every row carries the same number of cards.
	perRow := n
	for perRow > 1 {
		w := (available - gap*(perRow-1)) / perRow
		if w >= minCardW {
			break
		}
		next := perRow - 1
		for next > 1 && n%next != 0 {
			next--
		}
		perRow = next
	}

	width := (available - gap*(perRow-1)) / perRow
	if width > maxCardW {
		width = maxCardW
	}
	if width < minCardW {
		width = minCardW
	}

	var rows []string
	for i := 0; i < n; i += perRow {
		end := i + perRow
		if end > n {
			end = n
		}
		cards := make([]string, 0, end-i)
		for _, sp := range specs[i:end] {
			cards = append(cards, statBox(sp, width, pulseMap[sp.id]))
		}
		rows = append(rows, lipgloss.JoinHorizontal(lipgloss.Top, cards...))
	}
	return strings.Join(rows, "\n")
}

// statBox renders a single card at the given width.
//
// When `pulsedAt` is within the pulseDuration window, the card's
// border flips from muted to accent — a visual cue that the value
// just changed. After the window expires, it reverts automatically
// (the caller is responsible for scheduling a redraw at t+pulseDuration
// so the revert becomes visible without waiting for the next auto-
// refresh).
//
// Numeric values get a K/M suffix once they cross 10 000 — easier to
// scan and keeps the card width bounded.
func statBox(sp cardSpec, width int, pulsedAt time.Time) string {
	style := boxStyle.Width(width)
	if !pulsedAt.IsZero() && time.Since(pulsedAt) < pulseDuration {
		style = style.BorderForeground(colAccent)
	}
	iconCell := lipgloss.NewStyle().Foreground(colAccent).Render(sp.icon)
	labelLine := iconCell + " " + mutedStyle.Render(sp.label)
	valueLine := valueStyle.Render(formatCompact(sp.value))
	return style.Render(labelLine + "\n" + valueLine)
}

// formatCompact returns either a thousands-separated number (for
// values up to 9 999) or a k/M-shortened one. Chosen over strict
// "always shorten" because for a stat like Followers = 119 the raw
// number is what the user wants to see.
func formatCompact(n int) string {
	switch {
	case n >= 1_000_000:
		return fmt.Sprintf("%.1fM", float64(n)/1_000_000)
	case n >= 10_000:
		return fmt.Sprintf("%.1fk", float64(n)/1_000)
	case n >= 1_000:
		return formatThousands(n)
	default:
		return fmt.Sprintf("%d", n)
	}
}

func formatThousands(n int) string {
	s := fmt.Sprintf("%d", n)
	if len(s) <= 3 {
		return s
	}
	var out []byte
	for i, c := range []byte(s) {
		if i > 0 && (len(s)-i)%3 == 0 {
			out = append(out, ',')
		}
		out = append(out, c)
	}
	return string(out)
}

// ---------- Footer ----------

// renderFooterBar draws the bottom bar. Hotkeys hug the left edge,
// freshness + version hug the right. A thin top border separates it
// from the body. Width tracks the terminal so the right-aligned
// section lands at the visible right edge rather than 4 chars before
// it (outerStyle adds 2 chars of horizontal padding on each side).
func renderFooterBar(m Model) string {
	age := time.Since(m.lastFetch).Truncate(time.Second)

	left := mutedStyle.Render("r") + " refresh  " +
		mutedStyle.Render("·") + "  " +
		mutedStyle.Render("q") + " quit"

	// freshness is the "Updated Xs ago" or, while a fetch is in
	// flight, a live spinner. Keeps the last known cache visible
	// (numbers don't blank) while signalling the refresh activity.
	var freshness string
	if m.loading {
		freshness = m.spinner.View() + "  " + mutedStyle.Render("refreshing…")
	} else {
		freshness = mutedStyle.Render(fmt.Sprintf("Updated %s ago", age))
	}

	var right string
	if m.err != nil {
		right = errorStyle.Render("stale — last refresh errored") + "  " +
			mutedStyle.Render("octoscope "+m.version)
	} else {
		right = freshness + "  " +
			mutedStyle.Render(fmt.Sprintf("·  auto %ds  ·  octoscope %s",
				int(m.interval.Seconds()), m.version))
	}

	// If the terminal is wider than left+right, spread them to the
	// edges with Lipgloss JoinHorizontal + spacer. Otherwise stack.
	available := m.width - 4 // subtract outerStyle horizontal padding
	leftW := lipgloss.Width(left)
	rightW := lipgloss.Width(right)

	var row string
	if available >= leftW+rightW+4 {
		spacer := strings.Repeat(" ", available-leftW-rightW)
		row = left + spacer + right
	} else {
		row = left + "   " + right
	}

	return footerBarStyle.Width(available).Render(row)
}
