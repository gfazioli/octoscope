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

// Update routes keys received while the PRs tab is active.
func (pm PRsModel) Update(msg tea.Msg, stats *github.Stats) (PRsModel, tea.Cmd) {
	km, ok := msg.(tea.KeyMsg)
	if !ok {
		return pm, nil
	}
	if pm.searchActive {
		return pm.updateSearch(km), nil
	}

	n := 0
	if stats != nil {
		n = len(filterPRs(stats.OpenPullRequests, pm.query))
	}

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
	case "enter":
		if stats == nil || n == 0 || pm.cursor >= n {
			return pm, nil
		}
		rows := sortPRs(filterPRs(stats.OpenPullRequests, pm.query), pm.sort)
		return pm, openURLCmd(rows[pm.cursor].URL)
	case "esc":
		if pm.query != "" {
			pm.query = ""
			pm.cursor = 0
		}
	}
	return pm, nil
}

func (pm PRsModel) updateSearch(km tea.KeyMsg) PRsModel {
	switch km.String() {
	case "enter":
		pm.searchActive = false
		pm.cursor = 0
	case "esc":
		pm.searchActive = false
		pm.query = ""
		pm.cursor = 0
	case "backspace":
		if len(pm.query) > 0 {
			pm.query = pm.query[:len(pm.query)-1]
			pm.cursor = 0
		}
	default:
		if len(km.Runes) == 1 {
			pm.query += string(km.Runes)
			pm.cursor = 0
		}
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
	if len(stats.OpenPullRequests) == 0 {
		return mutedStyle.Render("(no open pull requests you authored)")
	}

	rows := sortPRs(filterPRs(stats.OpenPullRequests, pm.query), pm.sort)

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

	table := renderPRsTable(rows[offset:end], cursor-offset, pm.sort)

	hint := mutedStyle.Render("↑↓ move · g/G top/bottom · s sort · / search · enter open")

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
func renderPRsTable(prs []github.PullRequest, cursorRow int, sortMode PRsSort) string {
	const (
		cursorW  = 2
		numberW  = 6  // "#12345"
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
