package ui

import (
	"fmt"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/gfazioli/octoscope/internal/github"
)

// IssuesSort controls the ordering of the Issues-tab list. Same
// shape as PRsSort minus the draft/mergeable concept.
type IssuesSort int

const (
	IssuesSortUpdated IssuesSort = iota
	IssuesSortRepo
	IssuesSortNumber
)

var issuesSortLabels = [...]string{
	IssuesSortUpdated: "updated",
	IssuesSortRepo:    "repo",
	IssuesSortNumber:  "#",
}

var issuesSortChevron = [...]string{
	IssuesSortUpdated: "↓",
	IssuesSortRepo:    "↑",
	IssuesSortNumber:  "↑",
}

// IssuesModel is the Issues-tab sub-state. Same idioms as ReposModel
// and PRsModel: cursor, sort cycle, search filter, input mode.
type IssuesModel struct {
	cursor       int
	sort         IssuesSort
	query        string
	searchActive bool
}

// IsInputMode reports whether the sub-model is absorbing keystrokes
// as text (for the search box).
func (im IssuesModel) IsInputMode() bool {
	return im.searchActive
}

func (im IssuesModel) Update(msg tea.Msg, stats *github.Stats) (IssuesModel, tea.Cmd) {
	km, ok := msg.(tea.KeyMsg)
	if !ok {
		return im, nil
	}
	if im.searchActive {
		return im.updateSearch(km), nil
	}

	n := 0
	if stats != nil {
		n = len(filterIssues(stats.OpenIssuesList, im.query))
	}

	switch km.String() {
	case "up", "k":
		if im.cursor > 0 {
			im.cursor--
		}
	case "down", "j":
		if im.cursor < n-1 {
			im.cursor++
		}
	case "home", "g":
		im.cursor = 0
	case "end", "G":
		if n > 0 {
			im.cursor = n - 1
		}
	case "s":
		im.sort = (im.sort + 1) % IssuesSort(len(issuesSortLabels))
		im.cursor = 0
	case "/":
		im.searchActive = true
	case "enter":
		if stats == nil || n == 0 || im.cursor >= n {
			return im, nil
		}
		rows := sortIssues(filterIssues(stats.OpenIssuesList, im.query), im.sort)
		return im, openURLCmd(rows[im.cursor].URL)
	case "esc":
		if im.query != "" {
			im.query = ""
			im.cursor = 0
		}
	}
	return im, nil
}

func (im IssuesModel) updateSearch(km tea.KeyMsg) IssuesModel {
	switch km.String() {
	case "enter":
		im.searchActive = false
		im.cursor = 0
	case "esc":
		im.searchActive = false
		im.query = ""
		im.cursor = 0
	case "backspace":
		if len(im.query) > 0 {
			im.query = im.query[:len(im.query)-1]
			im.cursor = 0
		}
	default:
		if len(km.Runes) == 1 {
			im.query += string(km.Runes)
			im.cursor = 0
		}
	}
	return im
}

func filterIssues(issues []github.Issue, query string) []github.Issue {
	if query == "" {
		return issues
	}
	needle := strings.ToLower(query)
	out := make([]github.Issue, 0, len(issues))
	for _, i := range issues {
		if strings.Contains(strings.ToLower(i.Title), needle) ||
			strings.Contains(strings.ToLower(i.Repo), needle) {
			out = append(out, i)
		}
	}
	return out
}

func sortIssues(issues []github.Issue, mode IssuesSort) []github.Issue {
	out := make([]github.Issue, len(issues))
	copy(out, issues)
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		switch mode {
		case IssuesSortRepo:
			if a.Repo != b.Repo {
				return strings.ToLower(a.Repo) < strings.ToLower(b.Repo)
			}
			return a.Number < b.Number
		case IssuesSortNumber:
			return a.Number < b.Number
		default: // IssuesSortUpdated
			if !a.UpdatedAt.Equal(b.UpdatedAt) {
				return a.UpdatedAt.After(b.UpdatedAt)
			}
		}
		return a.Number < b.Number
	})
	return out
}

func (im IssuesModel) renderIssuesTab(stats *github.Stats, available, availableHeight int) string {
	if stats == nil {
		return mutedStyle.Render("(no issue data yet — waiting for first refresh)")
	}
	if len(stats.OpenIssuesList) == 0 {
		return mutedStyle.Render("(no open issues you authored)")
	}

	rows := sortIssues(filterIssues(stats.OpenIssuesList, im.query), im.sort)

	cursor := im.cursor
	if cursor >= len(rows) {
		cursor = len(rows) - 1
	}
	if cursor < 0 {
		cursor = 0
	}

	overhead := 6
	if im.searchActive || im.query != "" {
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

	headerLine := im.renderHeaderLine(len(rows), len(stats.OpenIssuesList), offset, end)

	var searchLine string
	switch {
	case im.searchActive:
		searchLine = mutedStyle.Render("search: ") +
			im.query + boldStyle.Foreground(colAccent).Render("█") +
			mutedStyle.Render("   (enter confirm · esc cancel)")
	case im.query != "":
		searchLine = mutedStyle.Render("filter: ") + im.query +
			mutedStyle.Render("   (esc to clear)")
	}

	table := renderIssuesTable(rows[offset:end], cursor-offset, im.sort)

	hint := mutedStyle.Render("↑↓ move · g/G top/bottom · s sort · / search · enter open")

	parts := []string{headerLine}
	if searchLine != "" {
		parts = append(parts, searchLine)
	}
	parts = append(parts, "", table, "", hint)
	return strings.Join(parts, "\n")
}

func (im IssuesModel) renderHeaderLine(visible, total, offset, end int) string {
	countLabel := fmt.Sprintf("%d open issue", visible)
	if visible != 1 {
		countLabel = fmt.Sprintf("%d open issues", visible)
	}
	if im.query != "" && visible != total {
		countLabel = fmt.Sprintf("%d of %d open issues", visible, total)
	}

	sortLabel := issuesSortLabels[im.sort] + " " + issuesSortChevron[im.sort]

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

func renderIssuesTable(issues []github.Issue, cursorRow int, sortMode IssuesSort) string {
	const (
		cursorW  = 2
		numberW  = 6
		titleW   = 50
		repoW    = 24
		updatedW = 10
	)

	decorate := func(label string, s IssuesSort, width int, align string) string {
		if s == sortMode {
			content := label + " " + issuesSortChevron[sortMode]
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
		decorate("#", IssuesSortNumber, numberW, "left"),
		mutedStyle.Render(padRight("Title", titleW)),
		decorate("Repo", IssuesSortRepo, repoW, "left"),
		decorate("Updated", IssuesSortUpdated, updatedW, "left"),
	}
	header := strings.Join(headerCells, "  ")

	rule := tabRuleStyle.Render(strings.Repeat("─", lipgloss.Width(header)))

	var out []string
	out = append(out, header, rule)

	for i, it := range issues {
		active := i == cursorRow

		marker := "  "
		if active {
			marker = activeTabStyle.Render("▸ ")
		}

		num := valueStyle.Render(padRight(fmt.Sprintf("#%d", it.Number), numberW))
		title := padRight(truncate(it.Title, titleW), titleW)
		if active {
			title = boldStyle.Foreground(colAccent).Render(title)
		}
		repo := padRight(truncate(it.Repo, repoW), repoW)
		updated := padRight(formatRelativeAgo(it.UpdatedAt), updatedW)

		if !active {
			repo = mutedStyle.Render(repo)
			updated = mutedStyle.Render(updated)
		}

		out = append(out, marker+num+"  "+title+"  "+repo+"  "+updated)
	}
	return strings.Join(out, "\n")
}
