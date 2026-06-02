package ui

import (
	"fmt"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/gfazioli/octoscope/internal/github"
)

// PRsSort controls the ordering of the PRs-tab list.
type PRsSort int

const (
	PRsSortUpdated PRsSort = iota // newest-updated first (default)
	PRsSortRepo                   // alphabetical by repo, stable by number
	PRsSortNumber                 // PR number ascending (oldest first)
)

var prsSortLabels = [...]string{
	PRsSortUpdated: "updated",
	PRsSortRepo:    "repo",
	PRsSortNumber:  "#",
}

var prsSortChevron = [...]string{
	PRsSortUpdated: "↓",
	PRsSortRepo:    "↑",
	PRsSortNumber:  "↑",
}

// PRsModel is the PRs-tab sub-state, parallel to ReposModel. Same
// idioms (cursor, sort cycle, search/filter, input mode) so muscle
// memory transfers between tabs.
type PRsModel struct {
	cursor       int
	sort         PRsSort
	query        string
	searchActive bool
}

// IsInputMode reports whether the sub-model is absorbing keystrokes
// as text (for the search box).
func (pm PRsModel) IsInputMode() bool {
	return pm.searchActive
}

// selectedPR returns the PR at the current cursor inside the
// partitioned view. Same idiom as ReposModel.selectedRepo with
// pinned/watched — used by the action menu so the dispatcher
// doesn't reimplement the list pipeline.
func (pm PRsModel) selectedPR(stats *github.Stats) (github.PullRequest, bool) {
	if stats == nil {
		return github.PullRequest{}, false
	}
	rows, _, _ := visiblePRsPartitioned(stats.ReviewRequests, stats.OpenPullRequests, pm.query, pm.sort)
	if len(rows) == 0 {
		return github.PullRequest{}, false
	}
	idx := pm.cursor
	if idx < 0 {
		idx = 0
	}
	if idx >= len(rows) {
		idx = len(rows) - 1
	}
	return rows[idx], true
}

// visiblePRsPartitioned is the canonical pipeline the PRs tab
// paints. Two sections, flat slice:
//
//   - Review-requests (Stats.ReviewRequests) on top, kept in
//     API order (UpdatedAt DESC) so the most recently-touched
//     row surfaces first. Sort cycle does NOT apply here —
//     they're an inbox, not a sortable dataset.
//   - Authored PRs (Stats.OpenPullRequests) below, with the
//     same filter + sort pipeline the v0.11.0 PRs tab used.
//
// Returns (rows, reviewCount, authoredCount) so the renderer
// can insert the rule divider after the review-requests
// segment. Cursor walks both sections uniformly.
func visiblePRsPartitioned(reviews, authored []github.PullRequest, query string, mode PRsSort) (rows []github.PullRequest, reviewCount, authoredCount int) {
	rev := filterPRs(reviews, query)
	auth := sortPRs(filterPRs(authored, query), mode)
	rows = make([]github.PullRequest, 0, len(rev)+len(auth))
	rows = append(rows, rev...)
	rows = append(rows, auth...)
	return rows, len(rev), len(auth)
}

// Update routes keys received while the PRs tab is active.
func (pm PRsModel) Update(msg tea.Msg, stats *github.Stats) (PRsModel, tea.Cmd) {
	km, ok := msg.(tea.KeyMsg)
	if !ok {
		return pm, nil
	}
	if pm.searchActive {
		return pm.updateSearch(km), nil
	}

	// Row count drives cursor bounds. Built from the same
	// pipeline the renderer uses (visiblePRsPartitioned) so
	// Update + View can never disagree on which PR lives at
	// index N — same drift-prevention rationale as Repos v0.13.
	var rows []github.PullRequest
	if stats != nil {
		rows, _, _ = visiblePRsPartitioned(stats.ReviewRequests, stats.OpenPullRequests, pm.query, pm.sort)
	}
	n := len(rows)

	switch km.String() {
	case "up", "k":
		if pm.cursor > 0 {
			pm.cursor--
		}
	case "down", "j":
		if pm.cursor < n-1 {
			pm.cursor++
		}
	case "home", "g":
		pm.cursor = 0
	case "end", "G":
		if n > 0 {
			pm.cursor = n - 1
		}
	case "s":
		pm.sort = (pm.sort + 1) % PRsSort(len(prsSortLabels))
		pm.cursor = 0
	case "/":
		pm.searchActive = true
	case "enter", "d":
		// v0.11.0: Enter / d → drill-in detail. Was openURLCmd
		// through v0.10.1. See repos.go for the rationale.
		if n == 0 || pm.cursor >= n {
			return pm, nil
		}
		return pm, viewPRDetailCmd(rows[pm.cursor])
	case "o":
		if n == 0 || pm.cursor >= n {
			return pm, nil
		}
		return pm, openURLCmd(rows[pm.cursor].URL)
	case "c":
		if n == 0 || pm.cursor >= n {
			return pm, nil
		}
		return pm, copyURLCmd(rows[pm.cursor].URL)
	case "esc":
		if pm.query != "" {
			pm.query = ""
			pm.cursor = 0
		}
	}
	return pm, nil
}

func (pm PRsModel) updateSearch(km tea.KeyMsg) PRsModel {
	// Dispatch on km.Type (see ReposModel.updateSearch) so paste / fast
	// multi-rune batches are captured, not dropped.
	switch km.Type {
	case tea.KeyEnter:
		pm.searchActive = false
		pm.cursor = 0
	case tea.KeyEsc:
		pm.searchActive = false
		pm.query = ""
		pm.cursor = 0
	case tea.KeyBackspace:
		if r := []rune(pm.query); len(r) > 0 {
			pm.query = string(r[:len(r)-1])
			pm.cursor = 0
		}
	case tea.KeyRunes, tea.KeySpace:
		pm.query += string(km.Runes)
		pm.cursor = 0
	}
	return pm
}

// filterPRs returns PRs whose title or repo name contains `query`,
// case-insensitive. Empty query is a no-op pass-through.
func filterPRs(prs []github.PullRequest, query string) []github.PullRequest {
	if query == "" {
		return prs
	}
	needle := strings.ToLower(query)
	out := make([]github.PullRequest, 0, len(prs))
	for _, p := range prs {
		if strings.Contains(strings.ToLower(p.Title), needle) ||
			strings.Contains(strings.ToLower(p.Repo), needle) {
			out = append(out, p)
		}
	}
	return out
}

func sortPRs(prs []github.PullRequest, mode PRsSort) []github.PullRequest {
	out := make([]github.PullRequest, len(prs))
	copy(out, prs)
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		switch mode {
		case PRsSortRepo:
			if a.Repo != b.Repo {
				return strings.ToLower(a.Repo) < strings.ToLower(b.Repo)
			}
			return a.Number < b.Number
		case PRsSortNumber:
			return a.Number < b.Number
		default: // PRsSortUpdated
			if !a.UpdatedAt.Equal(b.UpdatedAt) {
				return a.UpdatedAt.After(b.UpdatedAt)
			}
		}
		return a.Number < b.Number
	})
	return out
}

// renderPRsTab draws the tab body: header line, optional search
// prompt / filter indicator, table, and bottom hint. Viewport math
// mirrors renderReposTab so the two behave identically when scrolling.
func (pm PRsModel) renderPRsTab(stats *github.Stats, available, availableHeight int) string {
	if stats == nil {
		return mutedStyle.Render("(no pull-request data yet — waiting for first refresh)")
	}
	if len(stats.OpenPullRequests) == 0 && len(stats.ReviewRequests) == 0 {
		return mutedStyle.Render("(no open pull requests you authored)")
	}

	rows, reviewCount, _ := visiblePRsPartitioned(stats.ReviewRequests, stats.OpenPullRequests, pm.query, pm.sort)

	cursor := pm.cursor
	if cursor >= len(rows) {
		cursor = len(rows) - 1
	}
	if cursor < 0 {
		cursor = 0
	}

	overhead := 6
	if pm.searchActive || pm.query != "" {
		overhead++
	}
	rowsVisible := len(rows)
	if availableHeight > 0 {
		rowsVisible = availableHeight - overhead
		if rowsVisible < 3 {
			rowsVisible = 3
		}
		if rowsVisible > len(rows) {
			rowsVisible = len(rows)
		}
	}

	offset := 0
	if len(rows) > rowsVisible {
		offset = cursor - rowsVisible/2
		if offset < 0 {
			offset = 0
		}
		if offset > len(rows)-rowsVisible {
			offset = len(rows) - rowsVisible
		}
	}
	end := offset + rowsVisible
	if end > len(rows) {
		end = len(rows)
	}

	headerLine := pm.renderHeaderLine(len(rows), len(stats.OpenPullRequests), offset, end)

	var searchLine string
	switch {
	case pm.searchActive:
		searchLine = mutedStyle.Render("search: ") +
			pm.query + boldStyle.Foreground(colAccent).Render("█") +
			mutedStyle.Render("   (enter confirm · esc cancel)")
	case pm.query != "":
		searchLine = mutedStyle.Render("filter: ") + pm.query +
			mutedStyle.Render("   (esc to clear)")
	}

	// reviewDivider marks the index (within the visible window)
	// AFTER which the renderer inserts the rule separating the
	// review-requests sticky section from the authored list.
	// ≤0 or ≥len(slice) suppresses the divider.
	reviewDivider := reviewCount - offset
	table := renderPRsTable(rows[offset:end], cursor-offset, pm.sort, reviewDivider)

	hint := keyHints(
		"↑↓", "move",
		"g/G", "top/bottom",
		"s", "sort",
		"/", "search",
		"enter", "details",
		"o", "github",
		"c", "copy",
	)

	parts := []string{headerLine}
	if searchLine != "" {
		parts = append(parts, searchLine)
	}
	parts = append(parts, "", table, "", hint)
	return strings.Join(parts, "\n")
}

func (pm PRsModel) renderHeaderLine(visible, total, offset, end int) string {
	countLabel := fmt.Sprintf("%d open PR", visible)
	if visible != 1 {
		countLabel = fmt.Sprintf("%d open PRs", visible)
	}
	if pm.query != "" && visible != total {
		countLabel = fmt.Sprintf("%d of %d open PRs", visible, total)
	}

	sortLabel := prsSortLabels[pm.sort] + " " + prsSortChevron[pm.sort]

	parts := []string{
		mutedStyle.Render(countLabel),
		mutedStyle.Render("sort ") + activeTabStyle.Render(sortLabel),
	}
	if visible > 0 && (end-offset) < visible {
		parts = append(parts, mutedStyle.Render(fmt.Sprintf("%d–%d of %d", offset+1, end, visible)))
	}
	parts = append(parts, mutedStyle.Render("s cycle"))
	return strings.Join(parts, mutedStyle.Render(" · "))
}

// renderPRsTable lays out the PRs list: number, title, repo, state
// (draft / ready / conflicts), updated. Truncation is applied per
// column so a single long title doesn't push everything off-screen.
//
// reviewDivider is the index (within the visible window, post-
// offset) AFTER which the renderer inserts a muted rule separating
// the "PRs awaiting your review" sticky section from "Your authored
// PRs" below. ≤0 or ≥len(prs) suppresses the divider.
func renderPRsTable(prs []github.PullRequest, cursorRow int, sortMode PRsSort, reviewDivider int) string {
	const (
		cursorW  = 2
		numberW  = 6 // "#12345"
		titleW   = 40
		repoW    = 24
		stateW   = 10
		updatedW = 10
	)

	decorate := func(label string, s PRsSort, width int, align string) string {
		if s == sortMode {
			content := label + " " + prsSortChevron[sortMode]
			styled := activeTabStyle.Render(content)
			if align == "right" {
				return padLeftStr(styled, width)
			}
			return padRightRaw(styled, width)
		}
		if align == "right" {
			return mutedStyle.Render(padLeft(label, width))
		}
		return mutedStyle.Render(padRight(label, width))
	}

	headerCells := []string{
		strings.Repeat(" ", cursorW),
		decorate("#", PRsSortNumber, numberW, "left"),
		mutedStyle.Render(padRight("Title", titleW)),
		decorate("Repo", PRsSortRepo, repoW, "left"),
		mutedStyle.Render(padRight("State", stateW)),
		decorate("Updated", PRsSortUpdated, updatedW, "left"),
	}
	header := strings.Join(headerCells, "  ")

	rule := tabRuleStyle.Render(strings.Repeat("─", lipgloss.Width(header)))

	var out []string
	out = append(out, header, rule)

	for i, p := range prs {
		active := i == cursorRow

		marker := "  "
		if active {
			marker = activeTabStyle.Render("▸ ")
		}

		numStr := fmt.Sprintf("#%d", p.Number)
		num := valueStyle.Render(padRight(numStr, numberW))

		title := padRight(truncate(p.Title, titleW), titleW)
		if active {
			title = boldStyle.Foreground(colAccent).Render(title)
		}

		repo := padRight(truncate(p.Repo, repoW), repoW)
		state := padRightRaw(prStateCell(p), stateW)
		updated := padRight(formatRelativeAgo(p.UpdatedAt), updatedW)

		if !active {
			repo = mutedStyle.Render(repo)
			updated = mutedStyle.Render(updated)
		}

		out = append(out, marker+num+"  "+title+"  "+repo+"  "+state+"  "+updated)

		// Insert the review-requests / authored divider exactly
		// once, after the last review-requests row in the visible
		// window. Header width drives the rule length so it spans
		// the same band as the table itself.
		if reviewDivider > 0 && i == reviewDivider-1 && i+1 < len(prs) {
			out = append(out, tabRuleStyle.Render(strings.Repeat("─", lipgloss.Width(header))))
		}
	}
	return strings.Join(out, "\n")
}

// prStateCell returns a coloured 1-word status for a PR: draft ·
// ready · conflicts · ?. Precedence: IsDraft wins over Mergeable.
func prStateCell(p github.PullRequest) string {
	switch {
	case p.IsDraft:
		return mutedStyle.Render("draft")
	case p.Mergeable == "CONFLICTING":
		return lipgloss.NewStyle().Foreground(colError).Render("conflicts")
	case p.Mergeable == "MERGEABLE":
		return lipgloss.NewStyle().Foreground(colOK).Render("ready")
	default:
		return mutedStyle.Render("?")
	}
}
