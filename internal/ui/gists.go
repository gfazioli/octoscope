package ui

import (
	"fmt"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/gfazioli/octoscope/internal/github"
)

// GistsSort controls the ordering of the Gists-tab list. Same shape as
// IssuesSort — three modes, cycled with `s`.
type GistsSort int

const (
	GistsSortUpdated GistsSort = iota
	GistsSortStars
	GistsSortName
)

var gistsSortLabels = [...]string{
	GistsSortUpdated: "updated",
	GistsSortStars:   "stars",
	GistsSortName:    "name",
}

var gistsSortChevron = [...]string{
	GistsSortUpdated: "↓",
	GistsSortStars:   "↓",
	GistsSortName:    "↑",
}

// GistsModel is the Gists-tab sub-state: cursor, sort cycle, search
// filter, input mode. No pinned section — gists have no equivalent of a
// repo you keep at the top, and the sticky-partition machinery is only
// worth its complexity where there is something to stick.
type GistsModel struct {
	cursor       int
	sort         GistsSort
	query        string
	searchActive bool
	// expanded shows the selected gist's files inline. Deliberately not
	// a drill-in sub-model: the canonical pattern has loading / error /
	// loaded states because it fetches, and there is nothing to fetch
	// here — the file list already arrived with the row. Inventing a
	// loading state for data in hand would be theatre.
	expanded bool
}

// IsInputMode reports whether the sub-model is absorbing keystrokes as
// text (for the search box).
func (gm GistsModel) IsInputMode() bool {
	return gm.searchActive
}

// gistLabel is what the list shows for a gist.
//
// GitHub's `name` is the hash, so the description is the only
// human-readable handle — and it is routinely empty (2 of 16 on a real
// account). The fallback walks to the first filename, which is what the
// web UI titles an untitled gist with too, and only then to the hash.
// The github package deliberately leaves Description empty rather than
// filling it in, because that would put a value into --json output that
// GitHub never returned; choosing a display string is this layer's job.
func gistLabel(g github.Gist) string {
	if s := strings.TrimSpace(g.Description); s != "" {
		return s
	}
	if len(g.Files) > 0 && g.Files[0].Name != "" {
		return g.Files[0].Name
	}
	return g.Name
}

// selectedGist returns the gist at the cursor inside the
// sorted-filtered view, so the action menu never reimplements the row
// pipeline.
func (gm GistsModel) selectedGist(stats *github.Stats) (github.Gist, bool) {
	if stats == nil {
		return github.Gist{}, false
	}
	rows := visibleGists(stats.Gists, gm.query, gm.sort)
	if len(rows) == 0 {
		return github.Gist{}, false
	}
	idx := gm.cursor
	if idx < 0 {
		idx = 0
	}
	if idx >= len(rows) {
		idx = len(rows) - 1
	}
	return rows[idx], true
}

// Update handles key events routed from the root model when the Gists
// tab is active. Row count and ordering come from the same pipeline the
// renderer uses, so Update and View can never disagree about which gist
// lives at index N.
func (gm GistsModel) Update(msg tea.Msg, stats *github.Stats) (GistsModel, tea.Cmd) {
	km, ok := msg.(tea.KeyMsg)
	if !ok {
		return gm, nil
	}
	if gm.searchActive {
		return gm.updateSearch(km), nil
	}

	var rows []github.Gist
	if stats != nil {
		rows = visibleGists(stats.Gists, gm.query, gm.sort)
	}
	n := len(rows)

	switch km.String() {
	case "up", "k":
		if gm.cursor > 0 {
			gm.cursor--
			gm.expanded = false
		}
	case "down", "j":
		if gm.cursor < n-1 {
			gm.cursor++
			gm.expanded = false
		}
	case "home", "g":
		gm.cursor, gm.expanded = 0, false
	case "end", "G":
		if n > 0 {
			gm.cursor, gm.expanded = n-1, false
		}
	case "s":
		gm.sort = (gm.sort + 1) % GistsSort(len(gistsSortLabels))
		gm.cursor, gm.expanded = 0, false
	case "/":
		gm.searchActive = true
		gm.expanded = false
	case "enter", "d":
		if n == 0 || gm.cursor >= n {
			return gm, nil
		}
		gm.expanded = !gm.expanded
	case "o":
		if n == 0 || gm.cursor >= n {
			return gm, nil
		}
		return gm, openURLCmd(rows[gm.cursor].URL)
	case "c":
		if n == 0 || gm.cursor >= n {
			return gm, nil
		}
		return gm, copyURLCmd(rows[gm.cursor].URL)
	case "esc":
		switch {
		case gm.expanded:
			gm.expanded = false
		case gm.query != "":
			gm.query = ""
			gm.cursor = 0
		}
	}
	return gm, nil
}

func (gm GistsModel) updateSearch(km tea.KeyMsg) GistsModel {
	// Dispatch on km.Type so paste and fast multi-rune batches are
	// captured rather than dropped — see ReposModel.updateSearch.
	switch km.Type {
	case tea.KeyEnter:
		gm.searchActive = false
		gm.cursor = 0
	case tea.KeyEsc:
		gm.searchActive = false
		gm.query = ""
		gm.cursor = 0
	case tea.KeyBackspace:
		if r := []rune(gm.query); len(r) > 0 {
			gm.query = string(r[:len(r)-1])
			gm.cursor = 0
		}
	case tea.KeyRunes, tea.KeySpace:
		// Strip ANSI / C0 from pasted batches (sanitizeFilterInput).
		gm.query += sanitizeFilterInput(string(km.Runes))
		gm.cursor = 0
	}
	// Any of the above can move the cursor onto a different gist, and an
	// expansion that survives that is showing one gist's files under
	// another's name — nobody asked for it and it looks like a bug.
	gm.expanded = false
	return gm
}

// filterGists matches the query against what the user can actually see:
// the label the list renders, plus every filename. Matching the raw
// description alone would make an untitled gist unfindable by the name
// shown on its own row.
func filterGists(gists []github.Gist, query string) []github.Gist {
	if query == "" {
		return gists
	}
	needle := strings.ToLower(query)
	out := make([]github.Gist, 0, len(gists))
	for _, g := range gists {
		if strings.Contains(strings.ToLower(gistLabel(g)), needle) {
			out = append(out, g)
			continue
		}
		for _, f := range g.Files {
			if strings.Contains(strings.ToLower(f.Name), needle) {
				out = append(out, g)
				break
			}
		}
	}
	return out
}

func sortGists(gists []github.Gist, mode GistsSort) []github.Gist {
	out := make([]github.Gist, len(gists))
	copy(out, gists)
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		switch mode {
		case GistsSortStars:
			if a.Stars != b.Stars {
				return a.Stars > b.Stars
			}
			return a.UpdatedAt.After(b.UpdatedAt)
		case GistsSortName:
			return strings.ToLower(gistLabel(a)) < strings.ToLower(gistLabel(b))
		default: // GistsSortUpdated
			return a.UpdatedAt.After(b.UpdatedAt)
		}
	})
	return out
}

// visibleGists is the single source of truth for the row pipeline. The
// cursor, the renderer and the action menu all consume this, which is
// the lesson from the v0.11.0 filtered-stats bug — two callers deriving
// the same list separately is how a highlighted row stops matching the
// selected one.
func visibleGists(gists []github.Gist, query string, mode GistsSort) []github.Gist {
	return sortGists(filterGists(gists, query), mode)
}

func (gm GistsModel) renderGistsTab(stats *github.Stats, available, availableHeight int) string {
	if stats == nil {
		return mutedStyle.Render("(no gist data yet — waiting for first refresh)")
	}
	if len(stats.Gists) == 0 {
		// Silent on *why* it is empty, on purpose. The fetch is
		// best-effort, so this reads the same whether the account has no
		// gists or the query failed — and claiming "you have none" when
		// the fetch errored would be the kind of confident wrong answer
		// the scan's disclosures exist to avoid.
		return mutedStyle.Render("(no gists to show)")
	}

	rows := visibleGists(stats.Gists, gm.query, gm.sort)
	if len(rows) == 0 {
		return mutedStyle.Render(fmt.Sprintf("(no gists match %q — esc to clear)", gm.query))
	}

	cursor := gm.cursor
	if cursor >= len(rows) {
		cursor = len(rows) - 1
	}
	if cursor < 0 {
		cursor = 0
	}

	// Chrome is measured, not estimated: the tab always renders a title
	// line, the blank around the table, the table's own header and rule, a
	// trailing blank and the hint line — eight rows before a single gist.
	// An earlier guess of six is what let a tall expansion push the pinned
	// footer off a short terminal.
	const gistsChrome = 8
	chrome := gistsChrome
	if gm.searchActive || gm.query != "" {
		chrome++
	}

	content := availableHeight
	if content > 0 {
		content -= chrome
		if content < 4 {
			content = 4
		}
	}

	// When the expansion is open the two compete for the same rows, so
	// they split the budget rather than the expansion taking whatever it
	// wants and the list absorbing the overflow.
	listBudget, expandedFiles := content, 0
	if gm.expanded && availableHeight > 0 {
		expandedFiles = content/2 - 1
		if expandedFiles < 1 {
			expandedFiles = 1
		}
		if n := len(rows[cursor].Files); expandedFiles > n {
			expandedFiles = n
		}
		listBudget = content - expandedFiles - 1
		if listBudget < 1 {
			listBudget = 1
		}
	} else if gm.expanded {
		expandedFiles = len(rows[cursor].Files)
	}

	rowsVisible := len(rows)
	if availableHeight > 0 && listBudget < rowsVisible {
		rowsVisible = listBudget
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

	parts := []string{gm.renderHeaderLine(len(rows), len(stats.Gists), stats.GistsTotal, offset, end)}

	switch {
	case gm.searchActive:
		parts = append(parts, mutedStyle.Render("search: ")+
			gm.query+boldStyle.Foreground(colAccent).Render("█")+
			mutedStyle.Render("   (enter confirm · esc cancel)"))
	case gm.query != "":
		parts = append(parts, mutedStyle.Render("filter: ")+gm.query+
			mutedStyle.Render("   (esc to clear)"))
	}

	parts = append(parts, "", renderGistsTable(rows[offset:end], cursor-offset, gm.sort))

	if gm.expanded {
		parts = append(parts, "", renderGistFiles(rows[cursor], expandedFiles))
	}

	parts = append(parts, "", keyHints(
		"↑↓", "move",
		"g/G", "top/bottom",
		"s", "sort",
		"/", "search",
		"enter", "files",
		"o", "github",
		"c", "copy",
	))
	return strings.Join(parts, "\n")
}

// renderHeaderLine names the count, and says so when the list is a
// window rather than everything. total is what GitHub reported for the
// same query, so "showing 100 of 240" is honest about the page cap
// instead of letting a truncated list look complete.
func (gm GistsModel) renderHeaderLine(visible, fetched, total int, offset, end int) string {
	countLabel := fmt.Sprintf("%d gist", visible)
	if visible != 1 {
		countLabel = fmt.Sprintf("%d gists", visible)
	}
	// A filter and a truncated fetch are independent facts, and the two
	// used to be an if/else — so filtering a truncated list silently
	// dropped the truncation notice, which is the one of the two the
	// reader cannot otherwise discover.
	switch {
	case gm.query != "" && visible != fetched && total > fetched:
		countLabel = fmt.Sprintf("%d of %d matched, from %d fetched of %d", visible, fetched, fetched, total)
	case gm.query != "" && visible != fetched:
		countLabel = fmt.Sprintf("%d of %d gists", visible, fetched)
	case total > fetched:
		countLabel = fmt.Sprintf("%d of %d gists", fetched, total)
	}

	sortLabel := mutedStyle.Render("  sort: ") +
		valueStyle.Render(gistsSortLabels[gm.sort]+gistsSortChevron[gm.sort])

	window := ""
	if end-offset < visible {
		window = mutedStyle.Render(fmt.Sprintf("  rows %d-%d", offset+1, end))
	}
	return sectionTitleStyle.Render(countLabel) + sortLabel + window
}

func renderGistsTable(gists []github.Gist, cursorRow int, sortMode GistsSort) string {
	const (
		cursorW  = 2
		visW     = 7
		nameW    = 52
		filesW   = 6
		starsW   = 6
		updatedW = 10
	)

	decorate := func(label string, s GistsSort, width int) string {
		if s == sortMode {
			return padRightRaw(activeTabStyle.Render(label+" "+gistsSortChevron[sortMode]), width)
		}
		return mutedStyle.Render(padRight(label, width))
	}

	headerCells := []string{
		strings.Repeat(" ", cursorW),
		mutedStyle.Render(padRight("Vis", visW)),
		decorate("Gist", GistsSortName, nameW),
		mutedStyle.Render(padRight("Files", filesW)),
		decorate("Stars", GistsSortStars, starsW),
		decorate("Updated", GistsSortUpdated, updatedW),
	}
	header := strings.Join(headerCells, "  ")
	rule := tabRuleStyle.Render(strings.Repeat("─", lipgloss.Width(header)))

	out := []string{header, rule}
	for i, g := range gists {
		active := i == cursorRow

		marker := "  "
		if active {
			marker = activeTabStyle.Render("▸ ")
		}

		// "secret" rather than "private": that is GitHub's own word for
		// it, and a gist that is not public is not access-controlled —
		// anyone with the link can read it. Calling it private would
		// overstate what it protects.
		vis := "public"
		if !g.IsPublic {
			vis = "secret"
		}
		visCell := padRight(vis, visW)

		name := padRight(truncate(gistLabel(g), nameW), nameW)
		if active {
			name = boldStyle.Foreground(colAccent).Render(name)
		}
		// At the fetch cap the true count is unknowable — GitHub's
		// Gist.files is a plain list with no totalCount — so say "20+"
		// instead of printing a number that is probably wrong.
		fileCount := fmt.Sprintf("%d", len(g.Files))
		if len(g.Files) >= github.GistFilesLimit {
			fileCount = fmt.Sprintf("%d+", github.GistFilesLimit)
		}
		files := padRight(fileCount, filesW)
		stars := padRight(fmt.Sprintf("%d", g.Stars), starsW)
		updated := padRight(formatRelativeAgo(g.UpdatedAt), updatedW)

		if !active {
			visCell = mutedStyle.Render(visCell)
			files = mutedStyle.Render(files)
			stars = mutedStyle.Render(stars)
			updated = mutedStyle.Render(updated)
		}

		out = append(out, marker+visCell+"  "+name+"  "+files+"  "+stars+"  "+updated)
	}
	return strings.Join(out, "\n")
}

// renderGistFiles is the expanded view: the files already carried by the
// row, so opening it costs nothing and cannot fail.
func renderGistFiles(g github.Gist, max int) string {
	if len(g.Files) == 0 {
		return mutedStyle.Render("  (no files)")
	}
	shown := g.Files
	if max > 0 && max < len(shown) {
		shown = shown[:max]
	}
	var b strings.Builder
	header := "  files in " + gistLabel(g)
	switch {
	case len(shown) < len(g.Files):
		// Cut by the terminal, not by the fetch — a different limit from
		// GistFilesLimit and worth wording differently, so the reader can
		// tell "your window is short" from "GitHub gave us no more".
		header = fmt.Sprintf("  %d of %d files in %s", len(shown), len(g.Files), gistLabel(g))
	case len(g.Files) >= github.GistFilesLimit:
		header = fmt.Sprintf("  first %d files in %s", github.GistFilesLimit, gistLabel(g))
	}
	b.WriteString(mutedStyle.Render(header))
	for _, f := range shown {
		lang := f.Language
		if lang == "" {
			lang = "—"
		}
		b.WriteString("\n  " + valueStyle.Render(f.Name) +
			mutedStyle.Render(fmt.Sprintf("  %s  %s", lang, humanBytes(f.Size))))
	}
	return b.String()
}

// humanBytes renders a file size the way a person reads one. Sizes here
// are gist files, so kilobytes is the interesting scale.
func humanBytes(n int) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MiB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.1f KiB", float64(n)/(1<<10))
	default:
		return fmt.Sprintf("%d B", n)
	}
}
