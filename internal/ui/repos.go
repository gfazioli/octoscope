package ui

import (
	"fmt"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/gfazioli/octoscope/internal/github"
	"github.com/mattn/go-runewidth"
)

// ReposSort identifies which column the Repos tab sorts by. "pushed"
// is the default because on an active account "what am I working on
// now?" matters more than "what is top by stars over time".
type ReposSort int

const (
	ReposSortPushed ReposSort = iota
	ReposSortStars
	ReposSortForks
	ReposSortName
)

// reposSortLabels is the human-readable name for each sort mode,
// shown in the tab header so the user knows what they're looking at.
var reposSortLabels = [...]string{
	ReposSortPushed: "pushed",
	ReposSortStars:  "★ stars",
	ReposSortForks:  "⑂ forks",
	ReposSortName:   "name",
}

// reposSortChevron is the arrow glyph drawn next to the sorted
// column header: ↓ for descending sorts (the default for all three
// metric columns) and ↑ for the ascending name sort.
var reposSortChevron = [...]string{
	ReposSortPushed: "↓",
	ReposSortStars:  "↓",
	ReposSortForks:  "↓",
	ReposSortName:   "↑",
}

// ReposModel is the Repos-tab sub-state: cursor position, sort mode,
// and an optional search filter. Kept as plain fields (rather than a
// full bubbles/list + textinput) because the dataset is capped at 100
// rows and the custom column rendering would fight a generic list.
type ReposModel struct {
	cursor       int
	sort         ReposSort
	query        string // case-insensitive substring match on repo name
	searchActive bool   // true while the user is typing in the search box
}

// IsInputMode reports whether the sub-model is currently absorbing
// keystrokes as text (for the search box). The root model uses this
// to bypass its global hotkeys ("q", "1"–"5", "tab", …) so they
// become regular characters in the input buffer.
func (rm ReposModel) IsInputMode() bool {
	return rm.searchActive
}

// Update handles key events routed from the root model when the
// Repos tab is active. Returns the updated sub-model and any command
// the root should batch into its own result.
func (rm ReposModel) Update(msg tea.Msg, stats *github.Stats) (ReposModel, tea.Cmd) {
	km, ok := msg.(tea.KeyMsg)
	if !ok {
		return rm, nil
	}

	if rm.searchActive {
		return rm.updateSearch(km), nil
	}

	// Filtered-then-sorted row count drives cursor bounds. Computing it
	// here (rather than passing pre-filtered data in) keeps Update a
	// pure function of the ReposModel + raw stats.
	n := 0
	if stats != nil {
		n = len(filterRepos(stats.Repositories, rm.query))
	}

	switch km.String() {
	case "up", "k":
		if rm.cursor > 0 {
			rm.cursor--
		}
	case "down", "j":
		if rm.cursor < n-1 {
			rm.cursor++
		}
	case "home", "g":
		rm.cursor = 0
	case "end", "G":
		if n > 0 {
			rm.cursor = n - 1
		}
	case "s":
		rm.sort = (rm.sort + 1) % ReposSort(len(reposSortLabels))
		rm.cursor = 0
	case "/":
		rm.searchActive = true
	case "esc":
		// Outside search mode, Esc clears the current filter if any.
		// When no filter is set, Esc is a no-op so the user doesn't
		// lose context.
		if rm.query != "" {
			rm.query = ""
			rm.cursor = 0
		}
	}
	return rm, nil
}

// updateSearch routes keystrokes received while the search box is
// active. Letters append, backspace removes, Enter commits, Esc
// cancels (clearing the partial query).
func (rm ReposModel) updateSearch(km tea.KeyMsg) ReposModel {
	switch km.String() {
	case "enter":
		rm.searchActive = false
		rm.cursor = 0
	case "esc":
		rm.searchActive = false
		rm.query = ""
		rm.cursor = 0
	case "backspace":
		if len(rm.query) > 0 {
			rm.query = rm.query[:len(rm.query)-1]
			rm.cursor = 0
		}
	default:
		// Treat any single-rune key press as a literal character.
		// Multi-rune strings ("left", "ctrl+c", etc.) are ignored so
		// they don't pollute the query.
		if len(km.Runes) == 1 {
			rm.query += string(km.Runes)
			rm.cursor = 0
		}
	}
	return rm
}

// filterRepos returns only repos whose Name contains `query`,
// case-insensitive. An empty query short-circuits to a no-op copy,
// which keeps the rest of the pipeline (sort, viewport) oblivious to
// whether a filter is active.
func filterRepos(repos []github.Repo, query string) []github.Repo {
	if query == "" {
		return repos
	}
	needle := strings.ToLower(query)
	out := make([]github.Repo, 0, len(repos))
	for _, r := range repos {
		if strings.Contains(strings.ToLower(r.Name), needle) {
			out = append(out, r)
		}
	}
	return out
}

// sortRepos returns a fresh slice sorted according to `mode`. Never
// mutates the input so the caller can keep rendering the original
// order elsewhere if it ever wants to.
func sortRepos(repos []github.Repo, mode ReposSort) []github.Repo {
	out := make([]github.Repo, len(repos))
	copy(out, repos)
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		switch mode {
		case ReposSortStars:
			if a.Stars != b.Stars {
				return a.Stars > b.Stars
			}
		case ReposSortForks:
			if a.Forks != b.Forks {
				return a.Forks > b.Forks
			}
		case ReposSortName:
			return strings.ToLower(a.Name) < strings.ToLower(b.Name)
		default: // ReposSortPushed
			if !a.PushedAt.Equal(b.PushedAt) {
				return a.PushedAt.After(b.PushedAt)
			}
		}
		// Stable secondary sort on name so equal rows don't shuffle
		// between refreshes.
		return strings.ToLower(a.Name) < strings.ToLower(b.Name)
	})
	return out
}

// renderReposTab draws the header line, a windowed slice of the
// sorted-and-filtered rows (within the vertical budget passed in by
// the root view), and a short legend. availableHeight==0 means the
// caller doesn't know the terminal height yet; render everything
// and let the terminal scroll as a fallback.
func (rm ReposModel) renderReposTab(stats *github.Stats, available, availableHeight int) string {
	if stats == nil || len(stats.Repositories) == 0 {
		return mutedStyle.Render("(no repositories to show yet — waiting for first refresh)")
	}

	rows := sortRepos(filterRepos(stats.Repositories, rm.query), rm.sort)

	// Clamp the cursor in case the repo count shrank across a refresh
	// (private repo flipped public, repo deleted, filter narrowed the
	// set, etc.) — cheaper than tracking cursor identity across
	// resorts/refilters.
	cursor := rm.cursor
	if cursor >= len(rows) {
		cursor = len(rows) - 1
	}
	if cursor < 0 {
		cursor = 0
	}

	// Overhead inside the tab's vertical budget: header line, blank
	// line, table header, rule under the header, blank line, hint
	// line (and the search-prompt line when active). Row area is
	// what's left.
	overhead := 6
	if rm.searchActive || rm.query != "" {
		overhead++ // search/filter line
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

	// Offset: keep cursor roughly centred, but anchor to top/bottom
	// near the edges so we don't waste half the window on blank
	// padding when the user lands on row 0 or the last row.
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

	headerLine := rm.renderHeaderLine(len(rows), len(stats.Repositories), offset, end)

	// Search-prompt or filter-indicator line sits between the header
	// and the table so the eye picks it up without scanning.
	var searchLine string
	switch {
	case rm.searchActive:
		searchLine = mutedStyle.Render("search: ") +
			rm.query + boldStyle.Foreground(colAccent).Render("█") +
			mutedStyle.Render("   (enter confirm · esc cancel)")
	case rm.query != "":
		searchLine = mutedStyle.Render("filter: ") + rm.query +
			mutedStyle.Render("   (esc to clear)")
	}

	table := renderReposTable(rows[offset:end], cursor-offset, rm.sort)

	hint := mutedStyle.Render("↑↓ move · g/G top/bottom · s sort · / search")

	parts := []string{headerLine}
	if searchLine != "" {
		parts = append(parts, searchLine)
	}
	parts = append(parts, "", table, "", hint)
	return strings.Join(parts, "\n")
}

// renderHeaderLine produces the "N repositories · sort … · a–b of N · s cycle" line.
// Shows both the filtered and total counts when a filter is active so
// the user knows how much they've narrowed down to.
func (rm ReposModel) renderHeaderLine(visible, total, offset, end int) string {
	countLabel := fmt.Sprintf("%d repositories", visible)
	if rm.query != "" && visible != total {
		countLabel = fmt.Sprintf("%d of %d repositories", visible, total)
	}

	sortLabel := reposSortLabels[rm.sort] + " " + reposSortChevron[rm.sort]

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

// renderReposTable lays out the column header and one line per repo
// for the visible slice. `cursorRow` is zero-based within the slice
// (caller already computed the offset). `sortMode` drives which
// column header gets the accent + chevron treatment.
func renderReposTable(repos []github.Repo, cursorRow int, sortMode ReposSort) string {
	nameW := len("Name")
	langW := len("Lang")
	for _, r := range repos {
		if len(r.Name) > nameW {
			nameW = len(r.Name)
		}
		if len(r.PrimaryLanguage) > langW {
			langW = len(r.PrimaryLanguage)
		}
	}
	// Hard caps so one pathological name doesn't push everything off
	// screen. Truncation is applied at render time with an ellipsis.
	const (
		nameCap = 30
		langCap = 14
	)
	if nameW > nameCap {
		nameW = nameCap
	}
	if langW > langCap {
		langW = langCap
	}

	const (
		starsW  = 7
		forksW  = 6
		issuesW = 6
		prsW    = 5
		pushedW = 10 // "Xd ago" / "Xw ago" / "Xmo ago"
		cursorW = 2  // "▸ " / "  "
	)

	// Column header: plain mutedStyle, except the sorted column gets
	// activeTabStyle + a trailing chevron so the eye finds it.
	decorate := func(label string, s ReposSort, width int, align string) string {
		if s == sortMode {
			// Active column: accent + chevron, padded to width.
			content := label + " " + reposSortChevron[sortMode]
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
		decorate("Name", ReposSortName, nameW, "left"),
		mutedStyle.Render(padRight("Lang", langW)),
		decorate("★", ReposSortStars, starsW, "right"),
		decorate("⑂", ReposSortForks, forksW, "right"),
		mutedStyle.Render(padLeft("⚠", issuesW)),
		mutedStyle.Render(padLeft("⎇", prsW)),
		decorate("Pushed", ReposSortPushed, pushedW, "left"),
	}
	header := strings.Join(headerCells, "  ")

	rule := tabRuleStyle.Render(strings.Repeat("─", lipgloss.Width(header)))

	var out []string
	out = append(out, header, rule)

	for i, r := range repos {
		active := i == cursorRow

		marker := "  "
		if active {
			marker = activeTabStyle.Render("▸ ")
		}

		name := padRight(truncate(r.Name, nameW), nameW)
		if active {
			name = boldStyle.Foreground(colAccent).Render(name)
		}

		var lang string
		if r.PrimaryLanguage == "" {
			// Em-dash is 3 bytes in UTF-8 but 1 visible cell, so we
			// can't use byte-based padRight here — it would under-pad
			// by 2 cells and shift every column to its right.
			lang = padRightRaw(mutedStyle.Render("—"), langW)
		} else if r.LanguageColor != "" {
			lang = padRightRaw(
				lipgloss.NewStyle().Foreground(lipgloss.Color(r.LanguageColor)).
					Render(truncate(r.PrimaryLanguage, langW)),
				langW,
			)
		} else {
			lang = padRight(truncate(r.PrimaryLanguage, langW), langW)
		}

		stars := valueStyle.Render(padLeftStr(formatCompact(r.Stars), starsW))
		forks := padLeftStr(formatCompact(r.Forks), forksW)
		issues := padLeftStr(formatCompact(r.OpenIssues), issuesW)
		prs := padLeftStr(formatCompact(r.OpenPRs), prsW)
		pushed := padRight(formatRelativeAgo(r.PushedAt), pushedW)

		if !active {
			// Dim non-selected secondary columns so the active row
			// pops, but keep stars in cyan always (the headline
			// metric most users scan for).
			forks = mutedStyle.Render(forks)
			issues = mutedStyle.Render(issues)
			prs = mutedStyle.Render(prs)
			pushed = mutedStyle.Render(pushed)
		}

		out = append(out, marker+name+"  "+lang+"  "+stars+"  "+forks+"  "+issues+"  "+prs+"  "+pushed)
	}
	return strings.Join(out, "\n")
}

// cellWidth returns the visible cell width of s as rendered by a
// typical terminal, stripping ANSI escapes first so styled cells
// measure the same as plain ones. Uses runewidth because it treats
// modern emoji as wide (2 cells) — lipgloss.Width counts them as 1,
// which shifts right-side columns by 1 every time a title carries an
// emoji.
func cellWidth(s string) int {
	return runewidth.StringWidth(ansi.Strip(s))
}

// padRight / padLeft / padRightRaw / padLeftStr all pad by visible
// cell width (ANSI-aware, Unicode-aware). Using len() would miscount
// multi-byte runes and ANSI escapes, shifting right-side columns
// whenever a title contains emoji, accents, or CJK characters.
func padRight(s string, w int) string {
	vw := cellWidth(s)
	if vw >= w {
		return s
	}
	return s + strings.Repeat(" ", w-vw)
}

func padLeft(s string, w int) string {
	vw := cellWidth(s)
	if vw >= w {
		return s
	}
	return strings.Repeat(" ", w-vw) + s
}

func padRightRaw(s string, w int) string {
	return padRight(s, w)
}

func padLeftStr(s string, w int) string {
	return padLeft(s, w)
}

// truncate cuts `s` to at most `w` visible cells, appending an ellipsis
// when anything had to drop. Iterates by rune and measures cell width
// via runewidth so wide runes (CJK, emoji) are accounted for and no
// rune is ever sliced in half.
func truncate(s string, w int) string {
	if cellWidth(s) <= w {
		return s
	}
	if w <= 1 {
		return "…"
	}
	limit := w - 1
	var b strings.Builder
	used := 0
	for _, r := range s {
		rw := runewidth.RuneWidth(r)
		if used+rw > limit {
			break
		}
		b.WriteRune(r)
		used += rw
	}
	return b.String() + "…"
}

// formatRelativeAgo produces a compact relative-time label ("3d ago",
// "2w ago", "1y ago"). Shared with the PRs / Issues tabs via
// package-level visibility. Resolution tops out at "Xy ago" — anything
// older than a year reads the same visually.
func formatRelativeAgo(t time.Time) string {
	if t.IsZero() {
		return "—"
	}
	d := time.Since(t)
	switch {
	case d < time.Hour:
		m := int(d.Minutes())
		if m < 1 {
			m = 1
		}
		return fmt.Sprintf("%dm ago", m)
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	case d < 7*24*time.Hour:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	case d < 30*24*time.Hour:
		return fmt.Sprintf("%dw ago", int(d.Hours()/24/7))
	case d < 365*24*time.Hour:
		return fmt.Sprintf("%dmo ago", int(d.Hours()/24/30))
	default:
		return fmt.Sprintf("%dy ago", int(d.Hours()/24/365))
	}
}
