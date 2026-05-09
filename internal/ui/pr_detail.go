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

// PRDetailModel is the drill-in view for a single pull request.
// Lives on the root model alongside RepoDetailModel; the body of
// the PRs tab swaps to its View() while open. Same shape as
// RepoDetailModel: three-state machine (loading / error /
// loaded), sticky title + viewport-wrapped body, stale-fetch
// protection by URL correlation.
//
// Driven by `viewPRDetailMsg` from the action menu (or, post
// v0.11.0, by `Enter` on a PRs row directly).
type PRDetailModel struct {
	open    bool
	pr      github.PullRequest // the row that opened the detail (for header + retry)
	detail  *github.PRDetail
	err     error
	loading bool

	viewport viewport.Model

	// Cached rendered body keyed by the width it was produced at.
	// computeBody runs glamour markdown for the description, which
	// is the most expensive operation in the detail render path —
	// caching it stops Update + View from rendering it twice per
	// frame on every scroll keystroke. Invalidated by applyFetched
	// (new payload) and by width changes (re-rendered on demand).
	bodyCache      string
	bodyCacheWidth int
}

// IsOpen reports whether the detail view is currently active.
func (pd PRDetailModel) IsOpen() bool {
	return pd.open
}

// Open returns a fresh detail in the loading state, seeded with
// the row the user picked. Caller must dispatch
// fetchPRDetailCmd alongside this.
func (pd PRDetailModel) Open(pr github.PullRequest) PRDetailModel {
	return PRDetailModel{
		open:     true,
		pr:       pr,
		loading:  true,
		viewport: viewport.New(0, 0),
	}
}

// Close returns a closed detail (zero value).
func (pd PRDetailModel) Close() PRDetailModel {
	return PRDetailModel{}
}

// Update handles a single key event while the detail is open.
// Action keys (q / esc / r / o) take precedence; everything
// else is forwarded to the internal viewport so the body
// scrolls when its rendered height exceeds `height`.
func (pd PRDetailModel) Update(msg tea.KeyMsg, client *github.Client, width, height int) (PRDetailModel, tea.Cmd) {
	if !pd.open {
		return pd, nil
	}
	switch msg.String() {
	case "q":
		return pd.Close(), tea.Quit
	case "esc":
		return pd.Close(), nil
	case "r":
		owner, name, num := github.SplitOwnerNameNumber(pd.pr.URL)
		if owner == "" {
			return pd, nil
		}
		pd.loading = true
		pd.err = nil
		pd.detail = nil
		return pd, fetchPRDetailCmd(client, owner, name, num, pd.pr.URL)
	case "o":
		return pd, openURLCmd(pd.pr.URL)
	}

	pd = pd.syncViewport(width, height)
	var cmd tea.Cmd
	pd.viewport, cmd = pd.viewport.Update(msg)
	return pd, cmd
}

// applyFetched commits a fetched detail (or an error) into the
// model. The URL correlation lives on the root model — by the
// time we reach here, identity has already matched.
//
// Resets the body cache so the next syncViewport / View call
// recomputes against the fresh payload.
func (pd PRDetailModel) applyFetched(detail *github.PRDetail, err error) PRDetailModel {
	pd.loading = false
	pd.detail = detail
	pd.err = err
	pd.bodyCache = ""
	pd.bodyCacheWidth = 0
	return pd
}

// syncViewport refreshes the viewport's content + dimensions to
// match the current model state. Mirror of RepoDetailModel's
// helper. Populates bodyCache on the side so View can paint
// without recomputing.
func (pd PRDetailModel) syncViewport(width, height int) PRDetailModel {
	if pd.loading || pd.err != nil || pd.detail == nil {
		return pd
	}
	body := pd.bodyForWidth(width)
	pd.bodyCache = body
	pd.bodyCacheWidth = width
	pd.viewport.Width = width
	pd.viewport.Height = bodyViewportHeight(height)
	pd.viewport.SetContent(body)
	return pd
}

// bodyForWidth returns a rendered body, hitting the cache when
// possible. Used by both syncViewport (which populates the cache)
// and View (which falls back to a one-off recompute when called
// at a width the cache hasn't seen — View is value-typed and
// can't update the cache itself).
func (pd PRDetailModel) bodyForWidth(width int) string {
	if pd.bodyCache != "" && pd.bodyCacheWidth == width {
		return pd.bodyCache
	}
	return pd.computeBody(width)
}

// View renders the PR drill-in. Sticky title row + body wrapped
// in a viewport when terminal height is known. Same shape as
// RepoDetailModel.View.
func (pd PRDetailModel) View(width, height int) string {
	if !pd.open {
		return ""
	}

	title := pd.renderTitle()

	if pd.loading {
		return title + "\n\n" +
			mutedStyle.Render(fmt.Sprintf("Loading PR #%d…", pd.pr.Number))
	}
	if pd.err != nil {
		return title + "\n\n" +
			errorStyle.Render("Could not fetch PR detail") + "\n" +
			mutedStyle.Render(pd.err.Error()) + "\n\n" +
			mutedStyle.Render("r retry · esc back")
	}
	if pd.detail == nil {
		return title + "\n\n" + mutedStyle.Render("(no data)")
	}

	body := pd.bodyForWidth(width)
	if height <= 0 {
		return title + "\n\n" + body
	}

	vp := pd.viewport
	vp.Width = width
	vp.Height = bodyViewportHeight(height)
	vp.SetContent(body)
	return title + "\n\n" + vp.View()
}

// renderTitle is the sticky one-line header above the detail.
// Same idiom as RepoDetailModel.renderTitle but breadcrumb-shaped
// to "PRs / owner/repo#NN".
func (pd PRDetailModel) renderTitle() string {
	owner, name, num := github.SplitOwnerNameNumber(pd.pr.URL)
	if owner == "" {
		owner, name, num = "?", "?", pd.pr.Number
	}
	titleText := fmt.Sprintf("▸ PRs / %s/%s#%d", owner, name, num)
	return activeTabStyle.Render(titleText) +
		mutedStyle.Render("  esc back · o open in github · r refresh")
}

// computeBody renders the loaded-detail body. Pure function of
// the detail + width, used by both View (paint) and syncViewport
// (SetContent). Sections are hidden when their corresponding
// data is empty so a fresh PR with no reviews / no checks reads
// as cleanly as a busy one.
func (pd PRDetailModel) computeBody(width int) string {
	d := pd.detail
	var b strings.Builder

	// ---- Title row: #N + title + state chips
	titleLine := valueStyle.Render(fmt.Sprintf("#%d", d.Number)) +
		"  " + boldStyle.Foreground(colAccent).Render(d.Title)
	b.WriteString(lipgloss.NewStyle().Width(width - 2).Render(titleLine))
	b.WriteString("\n")

	// ---- Chip row: repo · branches · diff size · state
	chips := prDetailChips(d)
	if chips != "" {
		b.WriteString(chips)
		b.WriteString("\n")
	}
	b.WriteString("\n")

	// ---- Quick stats: opened by + dates
	b.WriteString(prDetailMeta(d))
	b.WriteString("\n\n")

	// ---- Description (markdown via glamour, no internal cap;
	// the surrounding viewport handles overflow).
	if body := strings.TrimSpace(d.Body); body != "" {
		b.WriteString(subSectionTitleStyle.Render("Description"))
		b.WriteString("\n")
		b.WriteString(renderDetailDescription(body, width))
		b.WriteString("\n\n")
	}

	// ---- Reviewers (requested + actual reviews)
	if len(d.RequestedReviewers) > 0 || len(d.Reviews) > 0 {
		b.WriteString(subSectionTitleStyle.Render("Reviewers"))
		b.WriteString("\n")
		b.WriteString(prDetailReviewers(d))
		b.WriteString("\n\n")
	}

	// ---- Checks (CI status)
	if d.ChecksState != "" || len(d.ChecksContexts) > 0 {
		title := subSectionTitleStyle.Render("Checks")
		if summary := prDetailChecksSummary(d); summary != "" {
			title += "   " + summary
		}
		b.WriteString(title)
		b.WriteString("\n")
		b.WriteString(prDetailChecks(d, width))
		b.WriteString("\n\n")
	}

	// ---- Recent timeline
	if len(d.Timeline) > 0 {
		b.WriteString(subSectionTitleStyle.Render("Recent activity"))
		b.WriteString("\n")
		b.WriteString(prDetailTimeline(d.Timeline, width))
		b.WriteString("\n\n")
	}

	// ---- Labels
	if len(d.Labels) > 0 {
		b.WriteString(subSectionTitleStyle.Render("Labels"))
		b.WriteString("\n")
		b.WriteString(prDetailLabels(d.Labels))
		b.WriteString("\n")
	}

	return strings.TrimRight(b.String(), "\n")
}

// prDetailChips renders the per-PR chip row: repo · base ← head ·
// state · mergeable · diff size. Empty chips get skipped.
func prDetailChips(d *github.PRDetail) string {
	var parts []string
	parts = append(parts, mutedStyle.Render(d.NameWithOwner))

	branches := boldStyle.Foreground(colValue).Render(d.HeadRefName) +
		mutedStyle.Render(" → ") +
		boldStyle.Foreground(colValue).Render(d.BaseRefName)
	parts = append(parts, branches)

	parts = append(parts, prStateChip(d))

	if d.Mergeable != "" && d.Mergeable != "UNKNOWN" && d.State == "OPEN" {
		parts = append(parts, prMergeableChip(d.Mergeable))
	}

	if d.Additions != 0 || d.Deletions != 0 {
		diff := okStyle.Render(fmt.Sprintf("+%d", d.Additions)) +
			" " +
			errorStyle.Render(fmt.Sprintf("-%d", d.Deletions))
		if d.ChangedFiles > 0 {
			diff += "  " + mutedStyle.Render(fmt.Sprintf("· %d files", d.ChangedFiles))
		}
		parts = append(parts, diff)
	}

	return strings.Join(parts, mutedStyle.Render("  ·  "))
}

// prStateChip renders the PR state badge. Open / merged / closed
// each get a distinct accent so the eye lands on it immediately.
// When IsDraft is true on an OPEN PR we render "Draft" alone
// (not "Draft Open") — the chip row already carries enough
// context that the redundancy reads as noise.
func prStateChip(d *github.PRDetail) string {
	switch d.State {
	case "MERGED":
		return boldStyle.Foreground(colAccent).Render("Merged")
	case "CLOSED":
		return errorStyle.Render("Closed")
	default:
		if d.IsDraft {
			return mutedStyle.Render("Draft")
		}
		return okStyle.Render("Open")
	}
}

// prMergeableChip renders the mergeable status. Only useful while
// the PR is open — shown next to the state chip in that case.
func prMergeableChip(state string) string {
	switch state {
	case "MERGEABLE":
		return okStyle.Render("Mergeable")
	case "CONFLICTING":
		return errorStyle.Render("Conflicts")
	default:
		return mutedStyle.Render(state)
	}
}

// prDetailMeta renders the "opened by · created · updated" line
// underneath the chips.
func prDetailMeta(d *github.PRDetail) string {
	var parts []string
	if d.AuthorLogin != "" {
		parts = append(parts, mutedStyle.Render("opened by ")+d.AuthorLogin)
	}
	if !d.CreatedAt.IsZero() {
		parts = append(parts, mutedStyle.Render("created "+formatRelativeAgo(d.CreatedAt)))
	}
	if !d.UpdatedAt.IsZero() {
		parts = append(parts, mutedStyle.Render("updated "+formatRelativeAgo(d.UpdatedAt)))
	}
	return strings.Join(parts, mutedStyle.Render(" · "))
}

// prDetailReviewers renders the merged "requested + reviewed"
// list. Reviewed entries lead (they have outcomes); pending
// requests follow.
func prDetailReviewers(d *github.PRDetail) string {
	var lines []string
	for _, r := range d.Reviews {
		state := prReviewStateLabel(r.State)
		when := mutedStyle.Render(formatRelativeAgo(r.SubmittedAt))
		lines = append(lines, fmt.Sprintf("  %s  %s  %s",
			padRight(r.AuthorLogin, 18), padRight(state, 22), when))
	}
	for _, name := range d.RequestedReviewers {
		state := mutedStyle.Render("requested")
		lines = append(lines, fmt.Sprintf("  %s  %s",
			padRight(name, 18), state))
	}
	return strings.Join(lines, "\n")
}

// prReviewStateLabel renders a review state with appropriate
// colour: green for approval, red for changes requested, muted
// for everything else.
func prReviewStateLabel(state string) string {
	switch state {
	case "APPROVED":
		return okStyle.Render("approved")
	case "CHANGES_REQUESTED":
		return errorStyle.Render("changes requested")
	case "COMMENTED":
		return mutedStyle.Render("commented")
	case "DISMISSED":
		return mutedStyle.Render("dismissed")
	case "PENDING":
		return mutedStyle.Render("pending")
	default:
		return mutedStyle.Render(strings.ToLower(state))
	}
}

// prDetailChecksSummary renders the inline summary next to the
// "Checks" sub-heading: the rollup state (success / failure /
// pending / etc.). Returns "" when there's no rollup.
func prDetailChecksSummary(d *github.PRDetail) string {
	switch d.ChecksState {
	case "":
		return ""
	case "SUCCESS":
		return okStyle.Render("all passing")
	case "FAILURE":
		return errorStyle.Render("failing")
	case "PENDING":
		return warnStyle.Render("pending")
	case "ERROR":
		return errorStyle.Render("errored")
	default:
		return mutedStyle.Render(strings.ToLower(d.ChecksState))
	}
}

// prDetailChecks renders one line per CI / status context, capped
// at 8 visible to keep the section compact. Excess gets a "+N
// more" footer.
func prDetailChecks(d *github.PRDetail, width int) string {
	const maxVisible = 8
	contexts := d.ChecksContexts
	overflow := 0
	if len(contexts) > maxVisible {
		overflow = len(contexts) - maxVisible
		contexts = contexts[:maxVisible]
	}

	var lines []string
	for _, c := range contexts {
		marker := prCheckMarker(c)
		name := truncate(c.Name, width-12)
		lines = append(lines, "  "+marker+"  "+name)
	}
	if overflow > 0 {
		lines = append(lines, mutedStyle.Render(fmt.Sprintf("    +%d more", overflow)))
	}
	return strings.Join(lines, "\n")
}

// prCheckMarker renders the per-check status glyph. SUCCESS = ✓
// (ok); failure-flavoured conclusions = ✗; muted neutrals = ·;
// everything pending or in flight = ⏳.
//
// The failure-flavoured set spans both CheckRun conclusions
// (FAILURE / TIMED_OUT / CANCELLED / ACTION_REQUIRED /
// STARTUP_FAILURE) and StatusContext states (FAILURE / ERROR).
// Keeping them in one case avoids treating STARTUP_FAILURE as
// "still pending" — a bot whose CI startup blew up is firmly
// in the red zone.
func prCheckMarker(c github.CheckSummary) string {
	state := c.Conclusion
	if state == "" {
		state = c.Status
	}
	switch state {
	case "SUCCESS":
		return okStyle.Render("✓")
	case "FAILURE", "ERROR", "TIMED_OUT", "CANCELLED", "ACTION_REQUIRED", "STARTUP_FAILURE":
		return errorStyle.Render("✗")
	case "NEUTRAL", "SKIPPED", "STALE":
		return mutedStyle.Render("·")
	default: // PENDING, IN_PROGRESS, QUEUED, WAITING, REQUESTED, COMPLETED-with-no-conclusion
		return warnStyle.Render("⏳")
	}
}

// prDetailTimeline renders the most recent ~10 events as a
// compact list. Each event line: glyph (per kind) + actor +
// detail + age.
//
// Actor and Detail are GitHub-sourced strings (login is usually
// safe but commit messages and label names can carry anything,
// including ANSI escapes). We sanitise both before painting —
// same hardening applied to body / comment rendering elsewhere
// in the file.
//
// Width-aware truncation: when the composed line plus the right-
// hand age column would exceed `width`, we trim Detail to fit.
// Truncate even at small budgets (down to a single ellipsis) so
// the age column always stays visible — the previous guard
// (`budget > 10`) skipped truncation entirely on narrow
// terminals or long actor names, letting the line overflow.
func prDetailTimeline(events []github.TimelineEvent, width int) string {
	const ageW = 10
	var lines []string
	for _, e := range events {
		glyph := prTimelineGlyph(e.Kind)
		actorText := sanitizeBody(e.Actor)
		actor := boldStyle.Render(actorText)
		detail := sanitizeBody(strings.TrimSpace(e.Detail))
		age := mutedStyle.Render(padRight(formatRelativeAgo(e.At), ageW))

		// Compose: " glyph  actor  detail  age" — width-aware
		// truncation on the detail so age stays right-side.
		left := "  " + glyph + "  " + actor + "  " + detail
		if cellWidth(left)+ageW+4 > width {
			budget := width - ageW - 4 - cellWidth("  "+glyph+"  ") - cellWidth(actor) - 2
			if budget < 1 {
				// Nothing fits — drop detail entirely; age stays.
				detail = ""
			} else {
				detail = truncate(detail, budget)
			}
			left = "  " + glyph + "  " + actor + "  " + detail
		}
		lines = append(lines, left+"  "+age)
	}
	return strings.Join(lines, "\n")
}

// prTimelineGlyph picks a single-cell glyph per timeline kind.
// Single colour-coded character per event keeps the section's
// vertical density readable.
//
// This function is shared with the Issues timeline render (issues
// emit kinds like "labeled" and "ref" that PRs don't have) — keep
// the case list union-aware so an issue's activity feed doesn't
// fall back to the generic dot for its issue-flavoured events.
func prTimelineGlyph(kind string) string {
	switch kind {
	case "review":
		return lipgloss.NewStyle().Foreground(colAccent).Render("✓")
	case "comment":
		return mutedStyle.Render("✎")
	case "merged":
		return boldStyle.Foreground(colAccent).Render("⏷")
	case "ready":
		return okStyle.Render("→")
	case "closed":
		return errorStyle.Render("×")
	case "reopened":
		return okStyle.Render("↺")
	case "assigned":
		return mutedStyle.Render("●")
	case "commit":
		return lipgloss.NewStyle().Foreground(colValue).Render("◆")
	case "labeled":
		// Issues only — emitted by extractIssueDetail when a label
		// is added. Tag-shaped glyph reads as "categorisation".
		return mutedStyle.Render("⎙")
	case "ref":
		// Issues only — emitted by extractIssueDetail for cross-
		// reference events ("referenced in PR", "referenced in
		// issue"). Right-arrow chain hints at the linkage.
		return lipgloss.NewStyle().Foreground(colValue).Render("⇄")
	default:
		return mutedStyle.Render("·")
	}
}

// prDetailLabels renders the labels as a dot-separated list,
// each in its GitHub-assigned hex colour.
func prDetailLabels(labels []github.LabelSummary) string {
	var parts []string
	for _, l := range labels {
		colour := lipgloss.Color("#" + strings.TrimPrefix(l.Color, "#"))
		parts = append(parts, lipgloss.NewStyle().Foreground(colour).Render(l.Name))
	}
	return "  " + strings.Join(parts, mutedStyle.Render(" · "))
}

// prDetailFetchedMsg carries the result of a FetchPRDetail
// round-trip back to the root model. URL field is the
// correlation key — same idiom as repoDetailFetchedMsg.
type prDetailFetchedMsg struct {
	url    string
	detail *github.PRDetail
	err    error
}

// fetchPRDetailCmd builds the BubbleTea command that pulls the
// PR detail off the network. 30s timeout matches the dashboard
// fetch (set in v0.10.1) — same headroom for transient
// network slowdowns.
func fetchPRDetailCmd(client *github.Client, owner, name string, number int, url string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		detail, err := client.FetchPRDetail(ctx, owner, name, number)
		return prDetailFetchedMsg{url: url, detail: detail, err: err}
	}
}
