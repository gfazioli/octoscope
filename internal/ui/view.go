package ui

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/gfazioli/octoscope/internal/github"
)

// View renders the current model. Layout for v0.3.0:
//
//  1. Banner (app name + version, accent-bordered top-left)
//  2. Profile card (bordered box: name · login · bio · meta)
//  3. Tab bar (Overview · Repos · PRs · Issues · Activity)
//  4. Tab content — Overview shows the four stat sections, Activity
//     shows the contribution heatmap, the rest are placeholders.
//  5. Footer bar (hotkeys left, freshness + version right, anchored
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
	// Apply the public-only filter at render time so toggling the
	// flag in the settings panel reflects instantly across every tab,
	// counter and language bar — no refetch round trip required. The
	// underlying m.stats stays untouched so flipping the flag back
	// off just stops calling Public() and the full dataset is
	// available again immediately.
	s := m.stats
	if s != nil && m.client.PublicOnly() {
		s = s.Public()
	}

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
	b.WriteString(renderProfileCard(s, available, m.client.PublicOnly()))
	b.WriteString("\n")
	b.WriteString(renderTabBar(m.activeTab, available))
	b.WriteString("\n")

	// Vertical budget for the tab body. The banner, profile card and
	// tab bar stay pinned; anything that scrolls (only Repos today)
	// confines itself to this height so the header doesn't get pushed
	// off the top of the terminal. Falls back to 0 (signal: no limit)
	// when m.height isn't known yet.
	tabHeight := 0
	if m.height > 0 {
		footerLines := lipgloss.Height(renderFooterBar(m))
		topLines := lipgloss.Height(b.String())
		tabHeight = m.height - topLines - footerLines - 3 // 2 outer padding + 1 gap
		if tabHeight < 6 {
			tabHeight = 6
		}
	}

	// Settings modal hijacks the tab content area while open. Banner,
	// profile, tab bar and footer all stay visible — only the body
	// swaps. Cleaner than a true overlay (no Z-order in Lipgloss) and
	// the user can still see what tab they were on by glancing up.
	if m.settings.IsOpen() {
		b.WriteString(m.settings.View(available))
	} else {
		switch m.activeTab {
		case TabOverview:
			b.WriteString(m.renderOverviewScrolled(s, available, tabHeight))
		case TabRepos:
			b.WriteString(m.repos.renderReposTab(s, available, tabHeight))
		case TabPRs:
			b.WriteString(m.prs.renderPRsTab(s, available, tabHeight))
		case TabIssues:
			b.WriteString(m.issues.renderIssuesTab(s, available, tabHeight))
		case TabActivity:
			b.WriteString(m.renderActivityScrolled(s, available, tabHeight))
		default:
			b.WriteString(renderComingSoonTab(m.activeTab))
		}
	}

	body := b.String()
	footer := renderFooterBar(m)

	// Anchor the footer to the bottom of the terminal when we have
	// more vertical room than the content needs. Falls back to a
	// plain blank line if the window is too small (or height is
	// unknown — first paint before WindowSizeMsg arrives).
	return outerStyle.Render(stackWithBottomFooter(body, footer, m.height))
}

// renderOverviewScrolled wraps renderOverviewTab in the Overview
// viewport so vertical overflow becomes scrollable instead of clipped
// off the top of the terminal. The viewport's YOffset is preserved
// from the model (advanced by Update when the user presses
// up/down/pgup/pgdn/space/u/d); we just feed it the freshly-rendered
// content + current dimensions every paint, since either could have
// changed between the last keystroke and this View (resize, refresh,
// public-only toggle, theme switch).
func (m Model) renderOverviewScrolled(s *github.Stats, available, tabHeight int) string {
	content := m.renderOverviewTab(s, available)
	if tabHeight <= 0 {
		// Height unknown (first paint before WindowSizeMsg) — render
		// inline and let the terminal decide; matches the pre-scroll
		// behaviour so we never get worse on first frame.
		return content
	}
	vp := m.overviewVP
	vp.Width = available
	vp.Height = tabHeight
	vp.SetContent(content)
	return vp.View()
}

// renderActivityScrolled mirrors renderOverviewScrolled for the
// Activity tab. The 52-week heatmap + summary line is the other
// static-content tab vulnerable to vertical clipping on short
// windows.
func (m Model) renderActivityScrolled(s *github.Stats, available, tabHeight int) string {
	content := renderActivityTab(s, available)
	if tabHeight <= 0 {
		return content
	}
	vp := m.activityVP
	vp.Width = available
	vp.Height = tabHeight
	vp.SetContent(content)
	return vp.View()
}

// renderOverviewTab is the v0.2.0 dashboard body: Social, Activity,
// Operational, Network. Lifted verbatim into its own function so the
// tab switch in View stays declarative.
func (m Model) renderOverviewTab(s *github.Stats, available int) string {
	var b strings.Builder
	b.WriteString(renderSection("Social", m.renderSocial(s, available)) + "\n")
	b.WriteString(renderSection("Activity", m.renderActivity(s, available)) + "\n")
	b.WriteString(renderSection("Operational", m.renderOperational(s, available)) + "\n")
	b.WriteString(renderSection("Network", renderNetwork(s, available)))
	return b.String()
}

// renderComingSoonTab draws a muted placeholder for tabs that aren't
// implemented yet. Keeps the tab bar navigable end-to-end so users can
// see what's planned rather than hitting dead ends.
func renderComingSoonTab(tab Tab) string {
	return mutedStyle.Render(fmt.Sprintf("%s — coming soon.", tabLabels[tab]))
}

// renderTabBar draws the single-line tab bar plus a faint rule below
// it. The active tab is accent-bold with a leading "▸" marker so the
// selection reads even on terminals that drop SGR bold.
func renderTabBar(active Tab, available int) string {
	parts := make([]string, 0, tabCount)
	for i, label := range tabLabels {
		if Tab(i) == active {
			parts = append(parts, activeTabStyle.Render("▸ "+label))
		} else {
			parts = append(parts, inactiveTabStyle.Render(label))
		}
	}
	sep := inactiveTabStyle.Render("  ·  ")
	bar := strings.Join(parts, sep)

	ruleW := available
	if ruleW < 1 {
		ruleW = 1
	}
	rule := tabRuleStyle.Render(strings.Repeat("─", ruleW))
	return bar + "\n" + rule
}

// renderBanner draws the app identity at the top: a rounded box with
// a crosshair glyph + app name + version. The glyph (⌖ U+2316
// POSITION INDICATOR) evokes the target-scope design of the logo
// without fighting the terminal's cell aspect ratio — a multi-line
// ASCII octagon comes out vertically stretched on any font.
func renderBanner(version string) string {
	cyan := lipgloss.NewStyle().Foreground(colValue)
	content := cyan.Render("⌖") + "  " + boldStyle.Foreground(colAccent).Render("octoscope")
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
func renderProfileCard(s *github.Stats, available int, publicOnly bool) string {
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
	if publicOnly {
		// "◐" — half-filled circle, semantically "showing only part
		// of the picture". Yellow rather than red: this is a safer
		// mode the user has opted into, not an error.
		parts = append(parts, warnStyle.Render("◐ public-only"))
	}
	lines = append(lines, wrap.Render(strings.Join(parts, "  ")))

	if s.Bio != "" {
		lines = append(lines, wrap.Render(s.Bio))
	}

	if meta := renderMetaRow(s, inner); meta != "" {
		lines = append(lines, meta)
	}

	// lipgloss' Width sets the total block size (border + padding
	// included). We pass `available - 1` so the right border sits
	// exactly under the last cell of the tab rule below — matching
	// `available` renders the border one cell past the rule on most
	// terminals. The inner wrap above already caps each line at
	// `inner` = available - 6 so content can't overflow.
	return profileCardStyle.Width(available - 1).Render(strings.Join(lines, "\n"))
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
	specs := []cardSpec{
		{id: "followers", icon: "●", label: "Followers", short: "Followers", value: s.Followers},
		{id: "following", icon: "○", label: "Following", short: "Following", value: s.Following},
		{id: "stars", icon: "★", label: "Stars Received", short: "Stars", value: s.TotalStars},
	}
	// When the user owns forks that contribute stars, the all-inclusive
	// total earns its own card so the dashboard reconciles with
	// counters (github-readme-stats and similar) that don't filter
	// forks out — without inflating the headline "Stars Received"
	// which remains "stars on stuff you authored".
	if s.TotalStarsWithForks > s.TotalStars {
		specs = append(specs, cardSpec{
			id: "stars_with_forks", icon: "★", label: "Stars + Forks",
			short: "+Forks", value: s.TotalStarsWithForks,
		})
	}
	return renderCardRow(available, m.compact, m.pulseMap, specs)
}

func (m Model) renderActivity(s *github.Stats, available int) string {
	row := renderCardRow(available, m.compact, m.pulseMap, []cardSpec{
		{id: "prs_authored", icon: "⎇", label: "PRs Authored", short: "PRs", value: s.PRsTotal},
		{id: "prs_merged", icon: "✓", label: "PRs Merged", short: "Merged", value: s.PRsMerged},
		{id: "issues_authored", icon: "⚠", label: "Issues Authored", short: "Issues", value: s.IssuesAuthored},
		{id: "commits_year", icon: "↻", label: "Commits (yr)", short: "Commits", value: s.CommitsLastYear},
	})

	// Derived metric: what share of the user's PRs made it in, plus
	// how many are still open right now. Wrapped in a summary box
	// sized to match the card row above so it visually "belongs to"
	// the cards as their takeaway. Accent border separates it from
	// the muted card borders. Hidden when the user has no PRs — the
	// "0% of 0" case is noise.
	var extras string
	if s.PRsTotal > 0 {
		rate := float64(s.PRsMerged) / float64(s.PRsTotal) * 100
		text := fmt.Sprintf("%.0f%% of %d PRs merged", rate, s.PRsTotal)
		if s.OpenPRsAuthored > 0 {
			text += fmt.Sprintf(" · %d still open", s.OpenPRsAuthored)
		}
		// Match the rendered width of the card row. lipgloss's Width
		// with Padding sets the *inside-border* width (padding lives
		// inside Width, only the 2-col border is added on top), so
		// for total render = rowW we pass Width = rowW - 2.
		rowW := lipgloss.Width(row)
		extras = summaryBoxStyle.Width(rowW - 2).Render(text)
	}

	// The synthesis box sits flush against the cards (single \n) so
	// it visually attaches to the row it summarises. Languages and
	// Top repositories keep the wider \n\n gap to read as their own
	// sub-sections.
	out := row
	if extras != "" {
		out += "\n" + extras
	}
	tail := []string{}
	if len(s.Languages) > 0 {
		tail = append(tail, renderLanguages(s.Languages, available))
	}
	if top := renderTopRepos(s.Repositories, available); top != "" {
		tail = append(tail, top)
	}
	if len(tail) > 0 {
		out += "\n\n" + strings.Join(tail, "\n\n")
	}
	return out
}

// renderTopRepos surfaces the user's five most-starred owned non-fork
// repositories as a tight ranked column: right-aligned star count,
// accent-pink star glyph, repo name. Quieter than a bar chart —
// matches the rest of the muted prose blocks (Languages, Network) so
// the section reads as supporting context, not a hero panel. Hidden
// when the user has fewer than three starred repos: filler rather
// than insight at that point.
func renderTopRepos(repos []github.Repo, _ int) string {
	type entry struct {
		name  string
		stars int
	}
	var ranked []entry
	for _, r := range repos {
		if r.Stars > 0 {
			ranked = append(ranked, entry{r.Name, r.Stars})
		}
	}
	if len(ranked) < 3 {
		return ""
	}
	sort.Slice(ranked, func(i, j int) bool {
		return ranked[i].stars > ranked[j].stars
	})
	if len(ranked) > 5 {
		ranked = ranked[:5]
	}

	var starColW int
	for _, e := range ranked {
		if l := len(fmt.Sprintf("%d", e.stars)); l > starColW {
			starColW = l
		}
	}

	var b strings.Builder
	b.WriteString(subSectionTitleStyle.Render("Top repositories") + "\n")
	starGlyph := lipgloss.NewStyle().Foreground(colAccent).Render("★")
	for _, e := range ranked {
		count := valueStyle.Render(fmt.Sprintf("%*d", starColW, e.stars))
		b.WriteString(fmt.Sprintf("  %s %s  %s\n", count, starGlyph, e.name))
	}
	return strings.TrimRight(b.String(), "\n")
}

func (m Model) renderOperational(s *github.Stats, available int) string {
	return renderCardRow(available, m.compact, m.pulseMap, []cardSpec{
		{id: "public_repos", icon: "▣", label: "Repositories", short: "Repos", value: s.PublicRepos},
		{id: "forks_received", icon: "⑂", label: "Forks Received", short: "Forks", value: s.ForksReceived},
		{id: "open_issues", icon: "◌", label: "Open Issues (own)", short: "Issues", value: s.OpenIssues},
		{id: "open_prs", icon: "⇄", label: "Open PRs (own)", short: "PRs", value: s.OpenPRs},
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
	b.WriteString(subSectionTitleStyle.Render("Languages") + "\n")
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
//   - short  — abbreviated label used in compact mode. Falls back to
//              label when empty.
//   - value  — the integer displayed below the label.
type cardSpec struct {
	id    string
	icon  string
	label string
	short string
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
//
// Compact mode shrinks both bounds and selects the cardSpec.short
// label, so more cards fit per row on narrow terminals.
func renderCardRow(available int, compact bool, pulseMap map[string]time.Time, specs []cardSpec) string {
	minCardW := 18
	maxCardW := 26
	if compact {
		minCardW = 12
		maxCardW = 18
	}
	const gap = 1
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
			// In compact mode, swap the long label for its short
			// counterpart so the narrower card width still fits the
			// text without truncation. sp is a local value copy,
			// safe to mutate.
			if compact && sp.short != "" {
				sp.label = sp.short
			}
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

// renderFooterBar draws the bottom bar.
//
// Layout is responsive: wide terminals get a single-line footer with
// hotkeys hugging the left edge and runtime status hugging the right.
// Narrow terminals (where the two pieces would collide) split into
// two stacked lines — status right-aligned on top, hotkeys left
// below — so neither side gets truncated. Threshold is dynamic:
// it's the rendered width of `keys + status + minSpacer`. A thin top
// border above the content separates the footer from the body.
func renderFooterBar(m Model) string {
	age := time.Since(m.lastFetch).Truncate(time.Second)

	keys := mutedStyle.Render("r") + " refresh  " +
		mutedStyle.Render("·") + "  " +
		mutedStyle.Render("1-5/tab") + " switch  " +
		mutedStyle.Render("·") + "  " +
		mutedStyle.Render("p") + " public  " +
		mutedStyle.Render("·") + "  " +
		mutedStyle.Render(",") + " settings  " +
		mutedStyle.Render("·") + "  " +
		mutedStyle.Render("q") + " quit"

	// Scroll hint surfaces only when the active tab actually overflows
	// vertically — otherwise the keys row stays compact and the hint
	// doesn't tease a behaviour the user can't see in action. The
	// viewport reports total > visible only after SetContent has been
	// called at least once, so on first paint (before any scroll key
	// fires) we silently miss the hint; that's fine, the next
	// keystroke or refresh will populate it.
	var scrollHint string
	switch m.activeTab {
	case TabOverview:
		if m.overviewVP.TotalLineCount() > m.overviewVP.VisibleLineCount() {
			scrollHint = mutedStyle.Render("·") + "  " +
				mutedStyle.Render("↑/↓") + " scroll"
		}
	case TabActivity:
		if m.activityVP.TotalLineCount() > m.activityVP.VisibleLineCount() {
			scrollHint = mutedStyle.Render("·") + "  " +
				mutedStyle.Render("↑/↓") + " scroll"
		}
	}
	if scrollHint != "" {
		keys += "  " + scrollHint
	}

	// freshness is the "Updated Xs ago" or, while a fetch is in
	// flight, a live spinner. Keeps the last known cache visible
	// (numbers don't blank) while signalling the refresh activity.
	var freshness string
	if m.loading {
		freshness = m.spinner.View() + "  " + mutedStyle.Render("refreshing…")
	} else {
		freshness = mutedStyle.Render(fmt.Sprintf("Updated %s ago", age))
	}

	var status string
	if m.err != nil {
		status = renderErrorLine(m)
	} else {
		rate := renderRateLimitChip(m.lastRateLimit)
		meta := mutedStyle.Render(fmt.Sprintf("auto %ds", int(m.interval.Seconds())))
		pieces := []string{freshness}
		if rate != "" {
			pieces = append(pieces, rate)
		}
		pieces = append(pieces, meta)
		status = strings.Join(pieces, mutedStyle.Render("  ·  "))
	}

	available := m.width - 4 // subtract outerStyle horizontal padding

	// Choose layout based on whether keys + status fit side-by-side
	// with a comfortable spacer. minSpacer = 4 keeps the two halves
	// from touching even at the boundary width — anything tighter
	// reads as a typo.
	const minSpacer = 4
	keysW := lipgloss.Width(keys)
	statusW := lipgloss.Width(status)

	var body string
	if available >= keysW+statusW+minSpacer {
		// Single line: keys left, spacer, status right.
		spacer := strings.Repeat(" ", available-keysW-statusW)
		body = keys + spacer + status
	} else {
		// Two lines: status right-aligned on top, keys left-aligned
		// below. PlaceHorizontal handles the right-padding so the
		// status lands flush against the right edge regardless of
		// rune widths in the spinner / chips.
		statusLine := lipgloss.PlaceHorizontal(available, lipgloss.Right, status)
		body = statusLine + "\n" + keys
	}
	return footerBarStyle.Width(available).Render(body)
}

// renderErrorLine picks the footer's error message based on the
// classified reason attached to the last fetch failure. Falls back
// to the generic "stale" wording for ReasonUnknown so old behaviour
// is preserved when the classifier can't match.
func renderErrorLine(m Model) string {
	warn := lipgloss.NewStyle().Foreground(colWarn)
	switch m.errReason {
	case github.ReasonRateLimitPrimary:
		msg := "rate-limited"
		if m.lastRateLimit != nil && !m.lastRateLimit.ResetAt.IsZero() {
			msg += " · retry at " + m.lastRateLimit.ResetAt.Local().Format("15:04")
		}
		return errorStyle.Render(msg)
	case github.ReasonRateLimitSecondary:
		return warn.Render("throttled briefly · backing off")
	case github.ReasonAuth:
		return errorStyle.Render("token rejected · check $GITHUB_TOKEN")
	case github.ReasonNetwork:
		return warn.Render("offline · retrying")
	case github.ReasonServer:
		return warn.Render("github errored · retrying")
	default:
		return errorStyle.Render("stale — last refresh errored")
	}
}

// renderRateLimitChip draws a compact "rate N/L · reset Xm" pill when
// a GraphQL budget snapshot is available. Colour tiers: muted by
// default, warn-yellow under 20%, error-red under 5%. Returns an
// empty string when rl is nil (e.g. before the first successful
// fetch, or when the viewer hits an unauthenticated ceiling).
func renderRateLimitChip(rl *github.RateLimit) string {
	if rl == nil || rl.Limit <= 0 {
		return ""
	}
	pct := float64(rl.Remaining) / float64(rl.Limit)

	style := mutedStyle
	switch {
	case pct < 0.05:
		style = lipgloss.NewStyle().Foreground(colError)
	case pct < 0.20:
		style = lipgloss.NewStyle().Foreground(colWarn)
	}

	label := fmt.Sprintf("rate %d/%d", rl.Remaining, rl.Limit)
	if !rl.ResetAt.IsZero() {
		label += " · reset " + formatResetETA(rl.ResetAt)
	}
	return style.Render(label)
}

// formatResetETA renders the remaining time until `reset` as a
// compact "Xm" / "Xs" label. Caps at "1h+" when we're more than an
// hour out so the chip doesn't widen unpredictably.
func formatResetETA(reset time.Time) string {
	d := time.Until(reset)
	if d <= 0 {
		return "now"
	}
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	return "1h+"
}
