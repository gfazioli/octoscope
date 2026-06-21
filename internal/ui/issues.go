package ui

import (
	"fmt"
	"sort"
	"strconv"
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

// selectedIssue returns the issue at the current cursor inside the
// sorted-filtered-partitioned view. Same idiom as
// ReposModel.selectedRepo / PRsModel.selectedPR — used by the action
// menu so the dispatcher doesn't reimplement the list pipeline.
//
// `pinned` is the ordered list of "owner/name#N" entries that render
// in the sticky top section. An empty slice degenerates the partition
// into a single section — same behaviour as before v0.21.0.
func (im IssuesModel) selectedIssue(stats *github.Stats, pinned []string) (github.Issue, bool) {
	if stats == nil {
		return github.Issue{}, false
	}
	rows, _, _ := visibleIssuesPartitioned(stats.OpenIssuesList, im.query, im.sort, pinned)
	if len(rows) == 0 {
		return github.Issue{}, false
	}
	idx := im.cursor
	if idx < 0 {
		idx = 0
	}
	if idx >= len(rows) {
		idx = len(rows) - 1
	}
	return rows[idx], true
}

// Update handles key events routed from the root model when the
// Issues tab is active. `pinned` is the live list of pinned
// "owner/name#N" identifiers so the cursor walks the same partitioned
// ordering the view paints — without it, the cursor at row N would
// resolve to a different issue than the highlighted one once any issue
// got pinned.
func (im IssuesModel) Update(msg tea.Msg, stats *github.Stats, pinned []string) (IssuesModel, tea.Cmd) {
	km, ok := msg.(tea.KeyMsg)
	if !ok {
		return im, nil
	}
	if im.searchActive {
		return im.updateSearch(km), nil
	}

	// Row count + ordering drive cursor bounds and the row-key
	// handlers. Built from the same pipeline the renderer uses
	// (visibleIssuesPartitioned) so Update + View can never disagree
	// on which issue lives at index N.
	var rows []github.Issue
	if stats != nil {
		rows, _, _ = visibleIssuesPartitioned(stats.OpenIssuesList, im.query, im.sort, pinned)
	}
	n := len(rows)

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
	case "enter", "d":
		// v0.11.0: Enter / d → drill-in detail. Was openURLCmd
		// through v0.10.1. See repos.go for the rationale.
		if n == 0 || im.cursor >= n {
			return im, nil
		}
		return im, viewIssueDetailCmd(rows[im.cursor])
	case "o":
		if n == 0 || im.cursor >= n {
			return im, nil
		}
		return im, openURLCmd(rows[im.cursor].URL)
	case "c":
		if n == 0 || im.cursor >= n {
			return im, nil
		}
		return im, copyURLCmd(rows[im.cursor].URL)
	case "P":
		// Toggle pin/unpin (v0.21.0). Capital P, mirror of the Repos
		// tab. The list mutation + config writeback happen in the root
		// model's pinIssueToggledMsg handler; this sub-model only emits
		// the request.
		if n == 0 || im.cursor >= n {
			return im, nil
		}
		return im, togglePinIssueCmd(issueID(rows[im.cursor]), !isPinnedIssue(rows[im.cursor], pinned))
	case "esc":
		if im.query != "" {
			im.query = ""
			im.cursor = 0
		}
	}
	return im, nil
}

func (im IssuesModel) updateSearch(km tea.KeyMsg) IssuesModel {
	// Dispatch on km.Type (see ReposModel.updateSearch) so paste / fast
	// multi-rune batches are captured, not dropped.
	switch km.Type {
	case tea.KeyEnter:
		im.searchActive = false
		im.cursor = 0
	case tea.KeyEsc:
		im.searchActive = false
		im.query = ""
		im.cursor = 0
	case tea.KeyBackspace:
		if r := []rune(im.query); len(r) > 0 {
			im.query = string(r[:len(r)-1])
			im.cursor = 0
		}
	case tea.KeyRunes, tea.KeySpace:
		// Strip ANSI / C0 from pasted batches (see sanitizeFilterInput).
		im.query += sanitizeFilterInput(string(km.Runes))
		im.cursor = 0
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

// issueID is the pin identity for an issue: "owner/name#N". Unlike
// repos (which parse owner/name out of a URL) the Issue struct
// already carries Repo ("owner/name") and Number, so the identity is
// a direct concatenation.
func issueID(it github.Issue) string {
	return it.Repo + "#" + strconv.Itoa(it.Number)
}

// isPinnedIssue reports whether an issue's "owner/name#N" identity is
// in pins (case-insensitive). Mirrors repos.isPinned but needs no URL
// parse. Used by the action-menu label ("Pin" vs "Unpin").
func isPinnedIssue(it github.Issue, pins []string) bool {
	if len(pins) == 0 {
		return false
	}
	key := strings.ToLower(issueID(it))
	for _, p := range pins {
		if strings.ToLower(p) == key {
			return true
		}
	}
	return false
}

// partitionIssuesByPinned splits issues into (pinned, rest); pinned
// are ordered by their position in pins (config order preserved),
// rest keeps input order. Comparison is case-insensitive on the
// "owner/name#N" identity. A pinned id that matches no issue in the
// input is silently absent — a closed/stale pinned entry simply
// doesn't appear. Mirror of repos.partitionByPinned.
func partitionIssuesByPinned(issues []github.Issue, pins []string) (pinned, rest []github.Issue) {
	if len(pins) == 0 {
		return nil, issues
	}
	rank := make(map[string]int, len(pins))
	for i, p := range pins {
		rank[strings.ToLower(p)] = i
	}
	type ranked struct {
		it   github.Issue
		rank int
	}
	var ordered []ranked
	rest = make([]github.Issue, 0, len(issues))
	for _, it := range issues {
		if pos, ok := rank[strings.ToLower(issueID(it))]; ok {
			ordered = append(ordered, ranked{it, pos})
			continue
		}
		rest = append(rest, it)
	}
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].rank < ordered[j].rank })
	pinned = make([]github.Issue, len(ordered))
	for i, e := range ordered {
		pinned[i] = e.it
	}
	return pinned, rest
}

// visibleIssuesPartitioned is the single source of truth for the
// Issues row pipeline: the pinned section on top (config order),
// then the filtered-and-sorted rest. The `/` filter applies to both
// segments; the sort cycle re-orders only the rest, like the Repos
// tab. selectedIssue, the Update cursor bounds and renderIssuesTab
// all consume it so the cursor can never disagree with the paint.
// Mirror of repos.visibleReposPartitioned (2-way, no watched
// section).
func visibleIssuesPartitioned(open []github.Issue, query string, mode IssuesSort, pinned []string) (rows []github.Issue, pinCount, restCount int) {
	filtered := filterIssues(open, query)
	pinnedRows, rest := partitionIssuesByPinned(filtered, pinned)
	rest = sortIssues(rest, mode)
	rows = make([]github.Issue, 0, len(pinnedRows)+len(rest))
	rows = append(rows, pinnedRows...)
	rows = append(rows, rest...)
	return rows, len(pinnedRows), len(rest)
}

// togglePinIssueCmd asks the root model to flip an issue's pinned
// state. `pin == true` means "add to the pinned list"; false means
// "remove". The root holds the canonical pinnedIssues slice and runs
// the config writeback + toast in one place. Mirror of togglePinCmd.
func togglePinIssueCmd(id string, pin bool) tea.Cmd {
	return func() tea.Msg {
		return pinIssueToggledMsg{id: id, pin: pin}
	}
}

// pinIssueToggledMsg is fired from the Issues tab (and the action
// menu) asking the root model to update the pinned-issues list.
// Carries the "owner/name#N" identity — already complete, no URL
// parse needed (unlike pinToggledMsg).
type pinIssueToggledMsg struct {
	id  string
	pin bool
}

func (im IssuesModel) renderIssuesTab(stats *github.Stats, available, availableHeight int, pinned []string) string {
	if stats == nil {
		return mutedStyle.Render("(no issue data yet — waiting for first refresh)")
	}
	if len(stats.OpenIssuesList) == 0 {
		return mutedStyle.Render("(no open issues you authored)")
	}

	// Single source of truth for the row pipeline (pinned section on
	// top, then filtered+sorted rest). pinCount positions the divider.
	rows, pinCount, _ := visibleIssuesPartitioned(stats.OpenIssuesList, im.query, im.sort, pinned)

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

	table := renderIssuesTable(rows[offset:end], cursor-offset, im.sort, pinCount-offset)

	hint := keyHints(
		"↑↓", "move",
		"g/G", "top/bottom",
		"s", "sort",
		"/", "search",
		"enter", "details",
		"o", "github",
		"c", "copy",
		"P", "pin",
	)

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

// renderIssuesTable lays out the column header and one line per issue
// for the visible slice. `cursorRow` is zero-based within the slice
// (caller already computed the offset). `pinDivider` is the index (in
// the visible window, post-offset) AFTER which a muted rule marks the
// pinned/rest boundary; ≤0 or ≥len(issues) suppresses it — an empty
// pinned section simply collapses. Mirror of renderReposTable.
func renderIssuesTable(issues []github.Issue, cursorRow int, sortMode IssuesSort, pinDivider int) string {
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

		// Insert the pinned/rest divider exactly once, after the last
		// pinned row, and only when a rest row follows. Re-uses the
		// header-width rule so the divider spans the same band.
		if pinDivider > 0 && i == pinDivider-1 && i+1 < len(issues) {
			out = append(out, rule)
		}
	}
	return strings.Join(out, "\n")
}
