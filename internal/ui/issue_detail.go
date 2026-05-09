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

// IssueDetailModel is the drill-in view for a single issue.
// Same shape as RepoDetailModel / PRDetailModel — three-state
// machine (loading / error / loaded), sticky title +
// viewport-wrapped body, stale-fetch protection by URL.
//
// Driven by `viewIssueDetailMsg` from the action menu (or, post
// v0.11.0, by `Enter` on an Issues row directly).
type IssueDetailModel struct {
	open    bool
	issue   github.Issue
	detail  *github.IssueDetail
	err     error
	loading bool

	viewport viewport.Model
}

// IsOpen reports whether the detail view is currently active.
func (id IssueDetailModel) IsOpen() bool {
	return id.open
}

// Open returns a fresh detail in the loading state.
func (id IssueDetailModel) Open(issue github.Issue) IssueDetailModel {
	return IssueDetailModel{
		open:     true,
		issue:    issue,
		loading:  true,
		viewport: viewport.New(0, 0),
	}
}

// Close returns a closed detail (zero value).
func (id IssueDetailModel) Close() IssueDetailModel {
	return IssueDetailModel{}
}

// Update handles a single key event while the detail is open.
// Same dispatch as RepoDetailModel / PRDetailModel.
func (id IssueDetailModel) Update(msg tea.KeyMsg, client *github.Client, width, height int) (IssueDetailModel, tea.Cmd) {
	if !id.open {
		return id, nil
	}
	switch msg.String() {
	case "q":
		return id.Close(), tea.Quit
	case "esc":
		return id.Close(), nil
	case "r":
		owner, name, num := github.SplitOwnerNameNumber(id.issue.URL)
		if owner == "" {
			return id, nil
		}
		id.loading = true
		id.err = nil
		id.detail = nil
		return id, fetchIssueDetailCmd(client, owner, name, num, id.issue.URL)
	case "o":
		return id, openURLCmd(id.issue.URL)
	}

	id = id.syncViewport(width, height)
	var cmd tea.Cmd
	id.viewport, cmd = id.viewport.Update(msg)
	return id, cmd
}

// applyFetched commits a fetched detail (or an error) into the
// model. URL correlation is enforced upstream by the root.
func (id IssueDetailModel) applyFetched(detail *github.IssueDetail, err error) IssueDetailModel {
	id.loading = false
	id.detail = detail
	id.err = err
	return id
}

// syncViewport refreshes content + dimensions on the viewport.
func (id IssueDetailModel) syncViewport(width, height int) IssueDetailModel {
	if id.loading || id.err != nil || id.detail == nil {
		return id
	}
	body := id.computeBody(width)
	id.viewport.Width = width
	id.viewport.Height = bodyViewportHeight(height)
	id.viewport.SetContent(body)
	return id
}

// View renders the issue drill-in. Sticky title + viewport-
// wrapped body when terminal height is known.
func (id IssueDetailModel) View(width, height int) string {
	if !id.open {
		return ""
	}

	title := id.renderTitle()

	if id.loading {
		return title + "\n\n" +
			mutedStyle.Render(fmt.Sprintf("Loading issue #%d…", id.issue.Number))
	}
	if id.err != nil {
		return title + "\n\n" +
			errorStyle.Render("Could not fetch issue detail") + "\n" +
			mutedStyle.Render(id.err.Error()) + "\n\n" +
			mutedStyle.Render("r retry · esc back")
	}
	if id.detail == nil {
		return title + "\n\n" + mutedStyle.Render("(no data)")
	}

	body := id.computeBody(width)
	if height <= 0 {
		return title + "\n\n" + body
	}

	vp := id.viewport
	vp.Width = width
	vp.Height = bodyViewportHeight(height)
	vp.SetContent(body)
	return title + "\n\n" + vp.View()
}

// renderTitle is the sticky one-line header above the detail.
// Breadcrumb shape: "Issues / owner/repo#NN".
func (id IssueDetailModel) renderTitle() string {
	owner, name, num := github.SplitOwnerNameNumber(id.issue.URL)
	if owner == "" {
		owner, name, num = "?", "?", id.issue.Number
	}
	titleText := fmt.Sprintf("▸ Issues / %s/%s#%d", owner, name, num)
	return activeTabStyle.Render(titleText) +
		mutedStyle.Render("  esc back · o open in github · r refresh")
}

// computeBody renders the loaded-detail body. Pure function of
// the detail + width. Section ordering: title row · chips ·
// description · assignees · comments preview · timeline · linked
// PRs · labels.
func (id IssueDetailModel) computeBody(width int) string {
	d := id.detail
	var b strings.Builder

	// ---- Title row: #N + title
	titleLine := valueStyle.Render(fmt.Sprintf("#%d", d.Number)) +
		"  " + boldStyle.Foreground(colAccent).Render(d.Title)
	b.WriteString(lipgloss.NewStyle().Width(width - 2).Render(titleLine))
	b.WriteString("\n")

	// ---- Chip row: repo · state · comment count
	b.WriteString(issueDetailChips(d))
	b.WriteString("\n\n")

	// ---- Quick stats: opened by + dates
	b.WriteString(issueDetailMeta(d))
	b.WriteString("\n\n")

	// ---- Description (markdown via glamour, same idiom as
	// PRDetail — viewport scrolls long bodies, no internal cap).
	if body := strings.TrimSpace(d.Body); body != "" {
		b.WriteString(subSectionTitleStyle.Render("Description"))
		b.WriteString("\n")
		b.WriteString(renderDetailDescription(body, width))
		b.WriteString("\n\n")
	}

	// ---- Assignees
	if len(d.Assignees) > 0 {
		b.WriteString(subSectionTitleStyle.Render("Assignees"))
		b.WriteString("\n")
		b.WriteString("  " + strings.Join(d.Assignees, mutedStyle.Render(" · ")))
		b.WriteString("\n\n")
	}

	// ---- Recent comments preview
	if len(d.CommentsPreview) > 0 {
		title := subSectionTitleStyle.Render("Recent comments")
		if d.CommentsTotal > len(d.CommentsPreview) {
			title += "   " + mutedStyle.Render(fmt.Sprintf("· %d total", d.CommentsTotal))
		}
		b.WriteString(title)
		b.WriteString("\n")
		b.WriteString(issueDetailComments(d.CommentsPreview, width))
		b.WriteString("\n\n")
	}

	// ---- Recent timeline
	if len(d.Timeline) > 0 {
		b.WriteString(subSectionTitleStyle.Render("Recent activity"))
		b.WriteString("\n")
		// Reuse the PR timeline renderer — TimelineEvent shape is
		// shared, glyphs map cleanly to issue-flavoured kinds too
		// (assigned, labeled, closed, reopened, comment, ref).
		b.WriteString(prDetailTimeline(d.Timeline, width))
		b.WriteString("\n\n")
	}

	// ---- Linked PRs
	if len(d.LinkedPRs) > 0 {
		b.WriteString(subSectionTitleStyle.Render("Linked pull requests"))
		b.WriteString("\n")
		b.WriteString(issueDetailLinkedPRs(d.LinkedPRs, width))
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

// issueDetailChips renders the chip row: repo · state · comment
// count. State drives the colour: open = ok, closed = muted (the
// classic GitHub closed/grey).
func issueDetailChips(d *github.IssueDetail) string {
	var parts []string
	parts = append(parts, mutedStyle.Render(d.NameWithOwner))
	parts = append(parts, issueStateChip(d.State))
	if d.CommentsTotal > 0 {
		parts = append(parts, mutedStyle.Render(fmt.Sprintf("%d comments", d.CommentsTotal)))
	}
	return strings.Join(parts, mutedStyle.Render("  ·  "))
}

// issueStateChip renders the issue state badge.
func issueStateChip(state string) string {
	switch state {
	case "OPEN":
		return okStyle.Render("Open")
	case "CLOSED":
		return mutedStyle.Render("Closed")
	default:
		return mutedStyle.Render(state)
	}
}

// issueDetailMeta is "opened by · created · updated".
func issueDetailMeta(d *github.IssueDetail) string {
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

// issueDetailComments renders up to 3 most-recent comments. Each
// entry: bold author + age, then a 2-line snippet of the body
// (truncated). Snippet helps users scan "is this thread on
// fire?" without opening the issue.
func issueDetailComments(comments []github.CommentSummary, width int) string {
	const maxBodyLines = 2
	var lines []string
	for _, c := range comments {
		header := boldStyle.Render(c.AuthorLogin) +
			"  " + mutedStyle.Render(formatRelativeAgo(c.CreatedAt))
		lines = append(lines, "  "+header)
		body := strings.TrimSpace(c.Body)
		if body == "" {
			continue
		}
		wrapped := lipgloss.NewStyle().Width(width - 4).Render(body)
		bodyLines := strings.Split(wrapped, "\n")
		if len(bodyLines) > maxBodyLines {
			bodyLines = bodyLines[:maxBodyLines]
			bodyLines[maxBodyLines-1] += "…"
		}
		for _, ln := range bodyLines {
			lines = append(lines, "    "+mutedStyle.Render(ln))
		}
	}
	return strings.Join(lines, "\n")
}

// issueDetailLinkedPRs renders the PRs that close this issue.
// Each row: state-coloured #N + title (truncated) + state label.
func issueDetailLinkedPRs(prs []github.LinkedPR, width int) string {
	const numW = 8
	const stateW = 8
	titleW := width - numW - stateW - 6
	if titleW < 20 {
		titleW = 20
	}

	var lines []string
	for _, pr := range prs {
		num := valueStyle.Render(padRight(fmt.Sprintf("#%d", pr.Number), numW))
		title := padRight(truncate(pr.Title, titleW), titleW)
		state := linkedPRStateLabel(pr.State)
		lines = append(lines, "  "+num+"  "+title+"  "+state)
	}
	return strings.Join(lines, "\n")
}

// linkedPRStateLabel renders the linked PR's state with the
// appropriate accent — same colour vocabulary as prStateChip.
func linkedPRStateLabel(state string) string {
	switch state {
	case "MERGED":
		return boldStyle.Foreground(colAccent).Render("merged")
	case "CLOSED":
		return errorStyle.Render("closed")
	case "OPEN":
		return okStyle.Render("open")
	default:
		return mutedStyle.Render(strings.ToLower(state))
	}
}

// issueDetailFetchedMsg carries the result of a FetchIssueDetail
// round-trip back to the root model. URL correlation key, same
// idiom as repoDetailFetchedMsg / prDetailFetchedMsg.
type issueDetailFetchedMsg struct {
	url    string
	detail *github.IssueDetail
	err    error
}

// fetchIssueDetailCmd builds the BubbleTea command that pulls
// the issue detail off the network. 30s timeout (same as
// dashboard fetch).
func fetchIssueDetailCmd(client *github.Client, owner, name string, number int, url string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		detail, err := client.FetchIssueDetail(ctx, owner, name, number)
		return issueDetailFetchedMsg{url: url, detail: detail, err: err}
	}
}
