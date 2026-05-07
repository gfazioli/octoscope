package ui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/gfazioli/octoscope/internal/github"
)

// RepoDetailModel is the drill-in view for a single repository.
// Lives on the root model; the body of the Repos tab swaps to its
// View() while open ("option B" of the design discussion in #5).
//
// State machine:
//
//	closed   ─open(repo)→  loading  ─fetched(detail)→  ready
//	                              ─fetched(err)──────→  error
//	         ←─────────── esc ───────────────────────  any
//	         ←──── fresh open / refresh ─── any (resets)
//
// The model carries enough context to retry on `r` (the original
// owner / name pair) without leaning on the root model — keeps the
// dispatch in model.go simple.
type RepoDetailModel struct {
	open    bool
	repo    github.Repo // the row that opened the detail (for header + retry)
	detail  *github.RepoDetail
	err     error
	loading bool

	// viewport wraps the body so a long detail (many languages,
	// many open issues / PRs, long topic list) stays inside the
	// tab-content height instead of pushing the pinned footer off
	// the screen. The title row above stays sticky — only the body
	// scrolls. Width/Height/SetContent are refreshed every Update
	// keystroke and every View paint so the viewport tracks the
	// current state.
	viewport viewport.Model
}

// IsOpen reports whether the detail view is currently active. The
// root model uses this to (a) route most keystrokes to the detail
// instead of the active list-tab dispatch and (b) replace the tab
// body in View() with the detail's render.
func (rd RepoDetailModel) IsOpen() bool {
	return rd.open
}

// Open returns a fresh detail in the loading state, seeded with the
// row the user picked. Caller is expected to dispatch
// fetchRepoDetailCmd(client, owner, name) alongside this so the
// loading state actually transitions to ready.
func (rd RepoDetailModel) Open(repo github.Repo) RepoDetailModel {
	return RepoDetailModel{
		open:     true,
		repo:     repo,
		loading:  true,
		viewport: viewport.New(0, 0),
	}
}

// Close returns a closed detail (zero value). Used by Update on
// `esc` and from the root when navigating away (e.g. tab switch
// while the detail is open — though we currently disable that path
// to keep the focus model predictable).
func (rd RepoDetailModel) Close() RepoDetailModel {
	return RepoDetailModel{}
}

// Update handles a single key event while the detail is open.
// Returns the updated model and a tea.Cmd. Action keys (q / esc /
// r / o) take precedence; everything else is forwarded to the
// internal viewport so ↑/↓/pgup/pgdn/space/u/d scroll the body
// when its rendered height exceeds `height`.
//
// `width` / `height` are the same `available` / `tabHeight` values
// View receives — passed in here so the viewport's internal
// maxYOffset reflects the current terminal size before the scroll
// command resolves.
func (rd RepoDetailModel) Update(msg tea.KeyMsg, client *github.Client, width, height int) (RepoDetailModel, tea.Cmd) {
	if !rd.open {
		return rd, nil
	}
	switch msg.String() {
	case "q":
		// Quit the app from any depth — same shortcut users have
		// at the dashboard level. Avoids the surprise of `q`
		// silently doing nothing inside the drill-in.
		return rd.Close(), tea.Quit
	case "esc":
		return rd.Close(), nil
	case "r":
		// Reset to the loading state and refire the fetch. Keeping
		// the previous detail visible during the refetch (instead of
		// blanking it like the dashboard does) felt jarring on this
		// modal-style view; the loading line is short enough.
		owner, name := github.SplitOwnerName(rd.repo.URL)
		if owner == "" {
			return rd, nil
		}
		rd.loading = true
		rd.err = nil
		rd.detail = nil
		return rd, fetchRepoDetailCmd(client, owner, name, rd.repo.URL)
	case "o":
		return rd, openURLCmd(rd.repo.URL)
	}

	// Scroll-key passthrough. We always sync first so the viewport
	// has fresh content + dimensions when its Update runs — without
	// the sync, the viewport's maxYOffset can be stale (e.g. after
	// terminal resize) and a pgdn keypress overshoots or no-ops.
	rd = rd.syncViewport(width, height)
	var cmd tea.Cmd
	rd.viewport, cmd = rd.viewport.Update(msg)
	return rd, cmd
}

// syncViewport refreshes the viewport's content and dimensions to
// match the current model state and the caller's terminal size.
// Returns a fresh model so the caller can persist the updated
// viewport (yOffset clamps automatically when the new content is
// shorter than the previous offset).
//
// No-op when not in the loaded-detail state — loading and error
// modes render a short fixed line that always fits, so there's
// nothing to scroll.
func (rd RepoDetailModel) syncViewport(width, height int) RepoDetailModel {
	if rd.loading || rd.err != nil || rd.detail == nil {
		return rd
	}
	body := rd.computeBody(width)
	rd.viewport.Width = width
	rd.viewport.Height = bodyViewportHeight(height)
	rd.viewport.SetContent(body)
	return rd
}

// bodyViewportHeight returns the height available for the
// scrollable body, given the caller's tab-content budget. Reserves
// 2 rows for the sticky title row + the blank separator below it,
// floors at 3 so even a very short window still has a usable
// viewport.
func bodyViewportHeight(tabHeight int) int {
	h := tabHeight - 2
	if h < 3 {
		h = 3
	}
	return h
}

// applyFetched commits a fetched detail (or an error) into the
// model. Called by the root Update when repoDetailFetchedMsg
// arrives — kept on this type so all detail-specific state changes
// live next to the rest of the model.
//
// The URL correlation prevents a stale fetch from clobbering a
// later open: if the user closes the detail and opens a different
// repo before the first fetch lands, the late response would race.
// The root checks the URL matches before calling this helper.
func (rd RepoDetailModel) applyFetched(detail *github.RepoDetail, err error) RepoDetailModel {
	rd.loading = false
	rd.detail = detail
	rd.err = err
	return rd
}

// View renders the detail inside the tab content area: a sticky
// title row at the top, then the body (description / stats /
// release / languages / commits / issues / PRs / topics). The
// title stays anchored, the body scrolls inside a viewport when
// its rendered height exceeds `height` — same idiom as the
// Overview / Activity tabs (see scroll.go), so a long detail can't
// push the pinned footer off-screen on a short terminal.
//
// `width` / `height` are the tab-content budget computed by the
// root View. height==0 means "unknown yet" (first paint before
// WindowSizeMsg) — we fall back to inline rendering, matching the
// fallback in renderOverviewScrolled / renderActivityScrolled.
func (rd RepoDetailModel) View(width, height int) string {
	if !rd.open {
		return ""
	}

	title := rd.renderTitle()

	// Loading / error / no-data: short fixed bodies that always fit.
	// Skip the viewport entirely so the layout stays simple in the
	// transient states.
	if rd.loading {
		_, name := github.SplitOwnerName(rd.repo.URL)
		return title + "\n\n" +
			mutedStyle.Render("Loading details for "+name+"…")
	}
	if rd.err != nil {
		return title + "\n\n" +
			errorStyle.Render("Could not fetch detail") + "\n" +
			mutedStyle.Render(rd.err.Error()) + "\n\n" +
			mutedStyle.Render("r retry · esc back")
	}
	if rd.detail == nil {
		// Defensive — shouldn't happen (loading=false + err=nil
		// without a detail means a programmer error), but render
		// rather than panic.
		return title + "\n\n" + mutedStyle.Render("(no data)")
	}

	body := rd.computeBody(width)

	// height==0 path: terminal size not known yet, render inline
	// and let the outer layout wrap. Same fallback as the v0.9.1
	// scrollable tabs use on first paint.
	if height <= 0 {
		return title + "\n\n" + body
	}

	// Wrap the body in the viewport so vertical overflow scrolls
	// instead of pushing the footer off-screen. yOffset is the one
	// piece of state preserved across paints — Update has it fresh
	// from the previous keystroke; here we just feed the current
	// content + dimensions and emit the rendered window.
	vp := rd.viewport
	vp.Width = width
	vp.Height = bodyViewportHeight(height)
	vp.SetContent(body)
	return title + "\n\n" + vp.View()
}

// renderTitle is the sticky one-line header above the detail body:
// breadcrumb to the repo + the in-detail key hints. Always
// rendered, regardless of loading / error / loaded state.
func (rd RepoDetailModel) renderTitle() string {
	owner, name := github.SplitOwnerName(rd.repo.URL)
	titleText := fmt.Sprintf("▸ Repos / %s / %s", owner, name)
	return activeTabStyle.Render(titleText) +
		mutedStyle.Render("  esc back · o open in github · r refresh")
}

// computeBody renders the loaded-detail body (everything below the
// sticky title). Pure function of the model + width — no side
// effects on the viewport. Used both by View (for paint) and by
// syncViewport (for SetContent before forwarding scroll keys).
func (rd RepoDetailModel) computeBody(width int) string {
	d := rd.detail
	var b strings.Builder

	// ---- Header line: name + chips
	nameLine := boldStyle.Foreground(colAccent).Render(d.Name)
	chips := repoDetailChips(d)
	if chips != "" {
		nameLine += strings.Repeat(" ", 4) + chips
	}
	b.WriteString(nameLine)
	b.WriteString("\n")

	// ---- Description
	if d.Description != "" {
		b.WriteString(lipgloss.NewStyle().Width(width - 2).Render(d.Description))
		b.WriteString("\n")
	}
	b.WriteString("\n")

	// ---- Quick stats row
	b.WriteString(repoDetailStats(d))
	b.WriteString("\n\n")

	// ---- Latest release (hidden when none)
	if d.LatestRelease != nil {
		b.WriteString(subSectionTitleStyle.Render("Latest release"))
		b.WriteString("\n")
		b.WriteString(repoDetailRelease(d.LatestRelease))
		b.WriteString("\n\n")
	}

	// ---- Languages
	if len(d.Languages) > 0 {
		b.WriteString(renderLanguages(d.Languages, width))
		b.WriteString("\n\n")
	}

	// ---- Recent commits + totals on the same heading line
	if len(d.RecentCommits) > 0 || d.HasDefaultBranch {
		title := subSectionTitleStyle.Render("Recent commits")
		if summary := repoDetailCommitSummary(d); summary != "" {
			title += "   " + summary
		}
		b.WriteString(title)
		b.WriteString("\n")
		if len(d.RecentCommits) > 0 {
			b.WriteString(repoDetailCommits(d.RecentCommits, width))
			b.WriteString("\n")
		}
		b.WriteString("\n")
	}

	// ---- Open issues preview
	if len(d.OpenIssuesPreview) > 0 {
		b.WriteString(subSectionTitleStyle.Render(
			fmt.Sprintf("Open issues (%d)", d.OpenIssues)))
		b.WriteString("\n")
		b.WriteString(repoDetailIssueList(d.OpenIssuesPreview, width))
		b.WriteString("\n\n")
	}

	// ---- Open PRs preview
	if len(d.OpenPRsPreview) > 0 {
		b.WriteString(subSectionTitleStyle.Render(
			fmt.Sprintf("Open pull requests (%d)", d.OpenPRs)))
		b.WriteString("\n")
		b.WriteString(repoDetailIssueList(d.OpenPRsPreview, width))
		b.WriteString("\n\n")
	}

	// ---- Topics
	if len(d.Topics) > 0 {
		b.WriteString(subSectionTitleStyle.Render("Topics"))
		b.WriteString("\n")
		b.WriteString(mutedStyle.Render(strings.Join(d.Topics, " · ")))
		b.WriteString("\n")
	}

	return strings.TrimRight(b.String(), "\n")
}

// repoDetailChips renders the dot-separated metadata badges shown
// to the right of the repo name: visibility, license, primary
// language. Empty / unknown fields are skipped silently — a repo
// with no license shows just "Public · Go" rather than "Public · ·
// Go" or "Public · — · Go".
func repoDetailChips(d *github.RepoDetail) string {
	var parts []string
	if d.IsPrivate {
		parts = append(parts, warnStyle.Render("Private"))
	} else {
		parts = append(parts, mutedStyle.Render("Public"))
	}
	if d.IsArchived {
		parts = append(parts, warnStyle.Render("Archived"))
	}
	if d.IsFork {
		parts = append(parts, mutedStyle.Render("Fork"))
	}
	if d.License != "" {
		parts = append(parts, mutedStyle.Render(d.License))
	}
	if d.PrimaryLanguage != "" {
		if d.PrimaryLanguageColor != "" {
			parts = append(parts,
				lipgloss.NewStyle().Foreground(lipgloss.Color(d.PrimaryLanguageColor)).Render(d.PrimaryLanguage),
			)
		} else {
			parts = append(parts, mutedStyle.Render(d.PrimaryLanguage))
		}
	}
	return strings.Join(parts, mutedStyle.Render(" · "))
}

// repoDetailStats renders the quick-stats row underneath the
// description: ★ ⑂ ⚠ ⎇ counts plus created / pushed times. Same
// glyphs as the Repos tab so the eye lines up.
func repoDetailStats(d *github.RepoDetail) string {
	stat := func(glyph string, n int) string {
		return lipgloss.NewStyle().Foreground(colAccent).Render(glyph) +
			" " + valueStyle.Render(formatCompact(n))
	}
	parts := []string{
		stat("★", d.Stars),
		stat("⑂", d.Forks),
		stat("⚠", d.OpenIssues),
		stat("⎇", d.OpenPRs),
	}
	dates := []string{}
	if !d.CreatedAt.IsZero() {
		dates = append(dates, mutedStyle.Render("Created "+formatRelativeAgo(d.CreatedAt)))
	}
	if !d.PushedAt.IsZero() {
		dates = append(dates, mutedStyle.Render("Pushed "+formatRelativeAgo(d.PushedAt)))
	}
	if len(dates) > 0 {
		parts = append(parts, strings.Join(dates, mutedStyle.Render(" · ")))
	}
	return strings.Join(parts, "   ")
}

// repoDetailRelease renders the latest-release info — tag, optional
// title, and a relative published-at marker. Title is suppressed
// when it equals the tag (common pattern: release name = "v1.2.3"
// matches tagName "v1.2.3" — printing both reads as redundant).
func repoDetailRelease(r *github.Release) string {
	tag := boldStyle.Foreground(colValue).Render(r.TagName)
	line := tag
	if r.Name != "" && r.Name != r.TagName {
		line += "  " + mutedStyle.Render("— ") + r.Name
	}
	when := mutedStyle.Render(fmt.Sprintf(
		"Published %s · %s ago",
		r.PublishedAt.Local().Format("2006-01-02"),
		humanDuration(time.Since(r.PublishedAt)),
	))
	return line + "\n  " + when
}

// repoDetailCommitSummary renders the inline counts shown next to
// the "Recent commits" sub-section heading: total commits on the
// default branch (all authors) and, when meaningful, the viewer's
// commits in the last 365 days. Returns "" when the repo has no
// default branch (empty repo) — the section title alone is enough.
//
// The "by you in the last year" sub-line shows only when the query
// was actually run with a viewer-author filter (AuthorFilterApplied
// — i.e. an authenticated client). For unauthenticated clients the
// count would always read 0 with no way to distinguish it from a
// real "you didn't commit" signal, so the half-truth is hidden.
func repoDetailCommitSummary(d *github.RepoDetail) string {
	if !d.HasDefaultBranch {
		return ""
	}
	parts := []string{
		valueStyle.Render(formatCompact(d.Commits)) + mutedStyle.Render(" total"),
	}
	if d.AuthorFilterApplied {
		parts = append(parts,
			valueStyle.Render(formatCompact(d.CommitsYearAuthored))+
				mutedStyle.Render(" by you in the last year"),
		)
	}
	return strings.Join(parts, mutedStyle.Render(" · "))
}

// repoDetailCommits renders up to 5 recent commits as a tight
// 4-column row: short OID, headline (truncated), author, age. The
// bullet glyph is omitted to keep the section visually quieter than
// the cards above.
func repoDetailCommits(commits []github.Commit, width int) string {
	const (
		oidW = 8
		ageW = 10
	)
	// Headline gets whatever room is left after OID + author + age
	// + 3 separators of 2 spaces. Author is capped at 16 cells so a
	// long login doesn't push the headline off-screen.
	authorW := 16
	headlineW := width - oidW - authorW - ageW - 6
	if headlineW < 20 {
		headlineW = 20
	}

	var lines []string
	for _, c := range commits {
		oid := mutedStyle.Render(padRight(c.OID[:min(7, len(c.OID))], oidW))
		headline := padRight(truncate(c.MessageHeadline, headlineW), headlineW)
		author := mutedStyle.Render(padRight(truncate(c.Author, authorW), authorW))
		age := mutedStyle.Render(padRight(formatRelativeAgo(c.CommittedDate), ageW))
		lines = append(lines, "  "+oid+"  "+headline+"  "+author+"  "+age)
	}
	return strings.Join(lines, "\n")
}

// repoDetailIssueList renders the up-to-3 issues / PRs preview
// blocks. Same shape for both — the section title (issues vs PRs)
// is the only difference, and that lives in View().
func repoDetailIssueList(items []github.IssuePreview, width int) string {
	const (
		numW = 6  // "#1234"
		ageW = 10 // "Xmo ago"
	)
	titleW := width - numW - ageW - 6
	if titleW < 20 {
		titleW = 20
	}
	var lines []string
	for _, it := range items {
		num := valueStyle.Render(padRight(fmt.Sprintf("#%d", it.Number), numW))
		title := padRight(truncate(it.Title, titleW), titleW)
		age := mutedStyle.Render(padRight(formatRelativeAgo(it.UpdatedAt), ageW))
		lines = append(lines, "  "+num+"  "+title+"  "+age)
	}
	return strings.Join(lines, "\n")
}

// humanDuration is the same kind of compact relative-time formatter
// as formatRelativeAgo but takes a duration rather than a time.
// Duplicated rather than refactored because the two callers want
// slightly different precision (release publish-at can be hours-old
// "fresh", while commits typically read in days/weeks).
func humanDuration(d time.Duration) string {
	switch {
	case d < time.Hour:
		m := int(d.Minutes())
		if m < 1 {
			m = 1
		}
		return fmt.Sprintf("%dm", m)
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	case d < 7*24*time.Hour:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	case d < 30*24*time.Hour:
		return fmt.Sprintf("%dw", int(d.Hours()/24/7))
	case d < 365*24*time.Hour:
		return fmt.Sprintf("%dmo", int(d.Hours()/24/30))
	default:
		return fmt.Sprintf("%dy", int(d.Hours()/24/365))
	}
}

// repoDetailFetchedMsg carries the result of a FetchRepoDetail
// round-trip back to the root model's Update. The URL field is the
// repo we asked for, used to discard stale responses (user closed +
// reopened on a different repo before this one landed).
type repoDetailFetchedMsg struct {
	url    string
	detail *github.RepoDetail
	err    error
}

// fetchRepoDetailCmd builds the BubbleTea command that pulls the
// detail payload off the network. 10s timeout matches the dashboard
// fetch — the detail query is even cheaper than the main one, so a
// timeout here is genuinely a network problem worth surfacing.
func fetchRepoDetailCmd(client *github.Client, owner, name, url string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		detail, err := client.FetchRepoDetail(ctx, owner, name)
		return repoDetailFetchedMsg{url: url, detail: detail, err: err}
	}
}

// min — Go 1.21+ has builtin min/max; keep this guard helper just
// for the OID slicing math above. Inline because importing slices
// or math is overkill for two integer args.
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
