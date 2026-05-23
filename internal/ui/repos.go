package ui

import (
	"fmt"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/gfazioli/octoscope/internal/browse"
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
	ReposSortCI      // v0.13.0 — surface failing CI first
	ReposSortRelease // v0.14.0 — most recent release first
)

// reposSortLabels is the human-readable name for each sort mode,
// shown in the tab header so the user knows what they're looking at.
var reposSortLabels = [...]string{
	ReposSortPushed:  "pushed",
	ReposSortStars:   "★ stars",
	ReposSortForks:   "⑂ forks",
	ReposSortName:    "name",
	ReposSortCI:      "CI",
	ReposSortRelease: "release",
}

// reposSortChevron is the arrow glyph drawn next to the sorted
// column header: ↓ for descending sorts (the default for all three
// metric columns) and ↑ for the ascending name sort. CI sorts
// failures-first, so the chevron points up to the "worst" rows.
var reposSortChevron = [...]string{
	ReposSortPushed:  "↓",
	ReposSortStars:   "↓",
	ReposSortForks:   "↓",
	ReposSortName:    "↑",
	ReposSortCI:      "↑",
	ReposSortRelease: "↓",
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

// selectedRepo returns the repo at the current cursor position
// inside the sorted-filtered-partitioned view, plus a bool
// indicating whether the selection is valid. Callers (action
// menu, drill-in) rely on this so they don't have to re-
// implement the sort+filter+pin pipeline. Returns ok=false on
// empty stats, empty filtered set, or out-of-range cursor (the
// view-level cursor clamp also covers this but we double-check
// here so callers can rely on a clean Repo).
//
// `pinned` is the ordered list of "owner/name" entries that
// render in the sticky top section. An empty pinned slice
// degenerates the partition into a single section — same
// behaviour as before v0.13.0.
func (rm ReposModel) selectedRepo(stats *github.Stats, pinned []string) (github.Repo, bool) {
	if stats == nil {
		return github.Repo{}, false
	}
	rows, _, _, _ := visibleReposPartitioned(stats.Repositories, stats.WatchedRepos, rm.query, rm.sort, pinned)
	if len(rows) == 0 {
		return github.Repo{}, false
	}
	idx := rm.cursor
	if idx < 0 {
		idx = 0
	}
	if idx >= len(rows) {
		idx = len(rows) - 1
	}
	return rows[idx], true
}

// visibleRepos is the canonical pipeline that produces the flat
// ordered slice the view paints and the cursor walks: filter →
// sort → partition (pinned on top in config order, rest after
// in the chosen sort).
//
// Exposed as a helper instead of inlined so selectedRepo,
// Update bounds, and renderReposTab share the exact same
// ordering — drifting their pipelines would make the cursor
// point at a different row than the highlighted one. Pure
// function of its inputs.
func visibleRepos(repos []github.Repo, query string, mode ReposSort, pinned []string) []github.Repo {
	filtered := filterRepos(repos, query)
	pinnedRows, rest := partitionByPinned(filtered, pinned)
	rest = sortRepos(rest, mode)
	out := make([]github.Repo, 0, len(pinnedRows)+len(rest))
	out = append(out, pinnedRows...)
	out = append(out, rest...)
	return out
}

// visibleReposPartitioned is the 3-way layout the Repos tab
// actually paints: pinned at top, owned rest in the middle,
// watched externals at the bottom. Each segment is independently
// filtered (search applies to all three) but only the rest is
// sorted by the active sort cycle — pinned and watched preserve
// the user's config ordering on purpose.
//
// Returns a flat slice (for the cursor + viewport math) plus
// the count of each section so the renderer can insert the
// muted rules between segments. Sections of zero length are
// silently absent from the output.
func visibleReposPartitioned(owned, watched []github.Repo, query string, mode ReposSort, pinned []string) (rows []github.Repo, pinCount, restCount, watchCount int) {
	filtered := filterRepos(owned, query)
	pinnedRows, rest := partitionByPinned(filtered, pinned)
	rest = sortRepos(rest, mode)
	watchRows := filterRepos(watched, query)
	rows = make([]github.Repo, 0, len(pinnedRows)+len(rest)+len(watchRows))
	rows = append(rows, pinnedRows...)
	rows = append(rows, rest...)
	rows = append(rows, watchRows...)
	return rows, len(pinnedRows), len(rest), len(watchRows)
}

// partitionByPinned splits `repos` into (pinned, rest):
//   - pinned: repos whose "owner/name" matches an entry in
//     `pins`, ordered by the position of the match in `pins`
//     so the user's listing intent in config is preserved
//   - rest: everything else, in input order
//
// Comparison is case-insensitive on "owner/name". Repos whose
// URL doesn't parse cleanly into owner/name fall through to
// rest — they cannot be pinned.
func partitionByPinned(repos []github.Repo, pins []string) (pinned, rest []github.Repo) {
	if len(pins) == 0 {
		return nil, repos
	}
	// rank[lowercased "owner/name"] = position in pins, used to
	// re-order the pinned slice to match the file's listing.
	rank := make(map[string]int, len(pins))
	for i, p := range pins {
		rank[strings.ToLower(p)] = i
	}
	type ranked struct {
		repo github.Repo
		rank int
	}
	var ordered []ranked
	rest = make([]github.Repo, 0, len(repos))
	for _, r := range repos {
		owner, name := github.SplitOwnerName(r.URL)
		if owner == "" || name == "" {
			rest = append(rest, r)
			continue
		}
		if pos, ok := rank[strings.ToLower(owner+"/"+name)]; ok {
			ordered = append(ordered, ranked{repo: r, rank: pos})
			continue
		}
		rest = append(rest, r)
	}
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].rank < ordered[j].rank })
	pinned = make([]github.Repo, len(ordered))
	for i, e := range ordered {
		pinned[i] = e.repo
	}
	return pinned, rest
}

// togglePinList returns a fresh slice with `key` either added to
// the end (when `add` is true and it isn't already present) or
// removed (when `add` is false). Case-insensitive match. Input
// slice is not mutated.
//
// Adds to the end on purpose: pinning a new repo from the TUI
// puts it at the bottom of the pinned section, which feels right
// (most-recent intent floats to the most-recent slot). Users who
// want a different order can hand-edit the config file —
// pinned_repos is preserved by the saver.
func togglePinList(in []string, key string, add bool) []string {
	out := make([]string, 0, len(in)+1)
	keyLower := strings.ToLower(key)
	found := false
	for _, p := range in {
		if strings.ToLower(p) == keyLower {
			found = true
			if !add {
				continue
			}
			out = append(out, p) // preserve original case
			continue
		}
		out = append(out, p)
	}
	if add && !found {
		out = append(out, key)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// pinnedEqual reports whether two pinned slices contain the same
// entries in the same order (case-insensitive). Used to decide
// whether a pinToggledMsg actually changed anything before
// running a disk writeback.
func pinnedEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if !strings.EqualFold(a[i], b[i]) {
			return false
		}
	}
	return true
}

// isPinned reports whether the given repo URL matches one of
// the pinned "owner/name" identifiers. Case-insensitive. Used
// by the action-menu label ("Pin" vs "Unpin") and the row
// glyph in the Repos tab.
func isPinned(url string, pins []string) bool {
	if len(pins) == 0 {
		return false
	}
	owner, name := github.SplitOwnerName(url)
	if owner == "" || name == "" {
		return false
	}
	key := strings.ToLower(owner + "/" + name)
	for _, p := range pins {
		if strings.ToLower(p) == key {
			return true
		}
	}
	return false
}

// Update handles key events routed from the root model when the
// Repos tab is active. Returns the updated sub-model and any command
// the root should batch into its own result.
//
// `pinned` is the live list of pinned "owner/name" identifiers so
// the cursor walks the same partitioned ordering the view paints.
// Without threading it through, the cursor pointing at row N would
// resolve to a different repo than the highlighted one once any
// repo got pinned.
func (rm ReposModel) Update(msg tea.Msg, stats *github.Stats, pinned []string) (ReposModel, tea.Cmd) {
	km, ok := msg.(tea.KeyMsg)
	if !ok {
		return rm, nil
	}

	if rm.searchActive {
		return rm.updateSearch(km), nil
	}

	// Row count drives cursor bounds. Built from the same pipeline
	// the renderer uses (visibleReposPartitioned) so Update + View
	// can never disagree on which repo lives at index N.
	var rows []github.Repo
	if stats != nil {
		rows, _, _, _ = visibleReposPartitioned(stats.Repositories, stats.WatchedRepos, rm.query, rm.sort, pinned)
	}
	n := len(rows)

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
	case "enter", "d":
		// v0.11.0: Enter and `d` both open the in-app drill-in
		// detail. Was openURLCmd through v0.10.1 — switching
		// because the TUI convention is "Enter = drill-in" (lazygit,
		// k9s, ranger). Browser access stays one keystroke away
		// via `o`.
		if n == 0 || rm.cursor >= n {
			return rm, nil
		}
		return rm, viewRepoDetailCmd(rows[rm.cursor])
	case "o":
		// Direct shortcut to open the row in the browser — what
		// `Enter` did pre-v0.11.0.
		if n == 0 || rm.cursor >= n {
			return rm, nil
		}
		return rm, openURLCmd(rows[rm.cursor].URL)
	case "c":
		// Direct shortcut to copy the row's URL.
		if n == 0 || rm.cursor >= n {
			return rm, nil
		}
		return rm, copyURLCmd(rows[rm.cursor].URL)
	case "P":
		// Toggle pin/unpin (v0.13.0). Capital P so it doesn't
		// collide with lowercase `p` (public-only) at the root
		// level. The actual list mutation + config writeback
		// happens in the root model's pinToggledMsg handler; this
		// sub-model only emits the request.
		if n == 0 || rm.cursor >= n {
			return rm, nil
		}
		return rm, togglePinCmd(rows[rm.cursor].URL, !isPinned(rows[rm.cursor].URL, pinned))
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
		case ReposSortCI:
			ar, br := ciSortRank(a.CIState), ciSortRank(b.CIState)
			if ar != br {
				return ar < br // failures (low rank) first
			}
		case ReposSortRelease:
			// Repos with no release sort to the bottom (zero time
			// would otherwise sort to the top under "most recent").
			az, bz := a.LatestReleasePublishedAt.IsZero(), b.LatestReleasePublishedAt.IsZero()
			if az != bz {
				return !az // a has a release and b doesn't → a first
			}
			if !az && !a.LatestReleasePublishedAt.Equal(b.LatestReleasePublishedAt) {
				return a.LatestReleasePublishedAt.After(b.LatestReleasePublishedAt)
			}
		default: // ReposSortPushed
			if !a.PushedAt.Equal(b.PushedAt) {
				return a.PushedAt.After(b.PushedAt)
			}
		}
		// Stable secondary sort: name first, then URL as a final
		// tie-breaker. Name alone isn't unique under
		// ownerAffiliations: OWNER (an org repo and a personal
		// repo may share a bare name), so equal-name rows would
		// otherwise shuffle non-deterministically between
		// refreshes. URL is GitHub's canonical per-repo identifier
		// and guarantees a total order.
		if !strings.EqualFold(a.Name, b.Name) {
			return strings.ToLower(a.Name) < strings.ToLower(b.Name)
		}
		return a.URL < b.URL
	})
	return out
}

// togglePinCmd asks the root model to flip a repo's pinned state.
// `pin == true` means "add to the pinned list"; false means
// "remove". The root handles writeback to disk and toast feedback.
func togglePinCmd(url string, pin bool) tea.Cmd {
	return func() tea.Msg {
		return pinToggledMsg{url: url, pin: pin}
	}
}

// pinToggledMsg is fired from the Repos tab (and the action menu)
// asking the root model to update the pinned list. Carries the
// repo URL because the root holds the canonical pinned slice and
// runs the config writeback in one place.
type pinToggledMsg struct {
	url string
	pin bool
}

// renderReposTab draws the header line, a windowed slice of the
// sorted-and-filtered rows (within the vertical budget passed in by
// the root view), and a short legend. availableHeight==0 means the
// caller doesn't know the terminal height yet; render everything
// and let the terminal scroll as a fallback.
func (rm ReposModel) renderReposTab(stats *github.Stats, available, availableHeight int, pinned []string) string {
	if stats == nil || len(stats.Repositories) == 0 {
		return mutedStyle.Render("(no repositories to show yet — waiting for first refresh)")
	}

	rows, pinCount, restCount, watchCount := visibleReposPartitioned(
		stats.Repositories, stats.WatchedRepos, rm.query, rm.sort, pinned,
	)
	_ = restCount // currently only the divider positions need it

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

	// Two dividers: after the last pinned row, and after the last
	// owned (rest) row. Watched-repo section sits at the bottom.
	watchStart := pinCount + restCount
	_ = watchCount // count is implicit from len(rows) - watchStart
	table := renderReposTable(rows[offset:end], cursor-offset, rm.sort, pinCount-offset, watchStart-offset)

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
// pinDivider / watchDivider are the indices (in the visible
// window, post-offset) AFTER which the renderer inserts a muted
// rule. They mark the pinned/rest boundary and the rest/watched
// boundary respectively. ≤0 or ≥len(repos) suppresses the
// corresponding divider — there's nothing to separate.
//
// Order matters: pinDivider must be ≤ watchDivider; the
// pinned segment always comes first.
func renderReposTable(repos []github.Repo, cursorRow int, sortMode ReposSort, pinDivider, watchDivider int) string {
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
		// CI column carries a 2-char "CI" header label (the bare
		// dot was too cryptic — feedback from first smoke) plus
		// a single coloured ● glyph per row, padded to the same
		// width so the row dot stays aligned to the first char
		// of the header label.
		ciW     = 2
		starsW  = 7
		forksW  = 6
		issuesW = 6
		prsW    = 5
		pushedW = 10 // "Xd ago" / "Xw ago" / "Xmo ago"
		cursorW = 2  // "▸ " / "  "
	)
	// reposReleaseW (package-level, see below) is the width of
	// the Release column; kept out of the local block so
	// formatLatestRelease can read the same value and the two
	// can never desync.
	const releaseW = reposReleaseW

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

	// The leading cursorW spaces are folded into the first cell
	// (CI column) rather than carried as a standalone slice entry.
	// Otherwise `strings.Join(... , "  ")` would insert an extra
	// 2-cell separator BETWEEN the cursor reserve and the CI
	// column, shifting the entire header right by two cells while
	// the row writer (marker + ci + "  " + name + …) stays put —
	// the visible result was a header ● floating two cells to the
	// right of every row dot. Folding keeps everything column-
	// aligned in a single pass without touching the other cells.
	headerCells := []string{
		strings.Repeat(" ", cursorW) + decorate("CI", ReposSortCI, ciW, "left"),
		decorate("Name", ReposSortName, nameW, "left"),
		mutedStyle.Render(padRight("Lang", langW)),
		decorate("★", ReposSortStars, starsW, "right"),
		decorate("⑂", ReposSortForks, forksW, "right"),
		mutedStyle.Render(padLeft("⚠", issuesW)),
		mutedStyle.Render(padLeft("⎇", prsW)),
		decorate("Pushed", ReposSortPushed, pushedW, "left"),
		decorate("Release", ReposSortRelease, releaseW, "left"),
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
		switch {
		case r.PrimaryLanguage == "":
			// Em-dash is 3 bytes in UTF-8 but 1 visible cell, so we
			// can't use byte-based padRight here — it would under-pad
			// by 2 cells and shift every column to its right.
			lang = padRightRaw(mutedStyle.Render("—"), langW)
		case IsMonochromatic():
			// Monochromatic theme suppresses GitHub's language hex
			// palette — render in plain foreground so the row stays
			// within the theme's six colour slots.
			lang = padRight(truncate(r.PrimaryLanguage, langW), langW)
		case r.LanguageColor != "":
			lang = padRightRaw(
				lipgloss.NewStyle().Foreground(lipgloss.Color(r.LanguageColor)).
					Render(truncate(r.PrimaryLanguage, langW)),
				langW,
			)
		default:
			lang = padRight(truncate(r.PrimaryLanguage, langW), langW)
		}

		stars := valueStyle.Render(padLeftStr(formatCompact(r.Stars), starsW))
		forks := padLeftStr(formatCompact(r.Forks), forksW)
		issues := padLeftStr(formatCompact(r.OpenIssues), issuesW)
		prs := padLeftStr(formatCompact(r.OpenPRs), prsW)
		pushed := padRight(formatRelativeAgo(r.PushedAt), pushedW)
		release := padRight(formatLatestRelease(r.LatestReleaseTag, r.LatestReleasePublishedAt), releaseW)

		if !active {
			// Dim non-selected secondary columns so the active row
			// pops, but keep stars in cyan always (the headline
			// metric most users scan for).
			forks = mutedStyle.Render(forks)
			issues = mutedStyle.Render(issues)
			prs = mutedStyle.Render(prs)
			pushed = mutedStyle.Render(pushed)
			release = mutedStyle.Render(release)
		}

		// Pad the dot to the full CI column width so the row
		// stays aligned with the 2-char "CI" header label —
		// padRightRaw is ANSI-aware so the trailing space lands
		// after the styled glyph rather than inside the escape.
		ci := padRightRaw(ciDot(r.CIState), 2)

		out = append(out, marker+ci+"  "+name+"  "+lang+"  "+stars+"  "+forks+"  "+issues+"  "+prs+"  "+pushed+"  "+release)

		// Insert the section dividers exactly once each, after
		// the last row of the pinned and rest segments. Re-uses
		// the header width so the rule spans the same band as
		// the table.
		rule := tabRuleStyle.Render(strings.Repeat("─", lipgloss.Width(header)))
		if pinDivider > 0 && i == pinDivider-1 && i+1 < len(repos) {
			out = append(out, rule)
		}
		if watchDivider > 0 && i == watchDivider-1 && i+1 < len(repos) && watchDivider != pinDivider {
			out = append(out, rule)
		}
	}
	return strings.Join(out, "\n")
}

// reposReleaseW is the cell width of the Repos-tab "Release"
// column. Shared between the column layout in renderReposTable
// and formatLatestRelease's tag-budget math so the two can't
// desync if the width ever changes. v1.2.3 (6) + " · " (3) +
// "Xmo ago" (worst case 8) ≈ 17; 14 fits the common case
// without clipping and leaves the table dense.
const reposReleaseW = 14

// formatLatestRelease renders the Repos-tab release column:
// "tag · Xd" — tag truncated to fit, plus the relative age of
// publishedAt. Returns an em-dash when the repo has no release
// so the column stays visually present without making a claim.
//
// Lives next to ciDot because the same width budget logic
// applies and the two are siblings in the v0.13.0 + v0.14.0
// Repos-tab additions.
func formatLatestRelease(tag string, publishedAt time.Time) string {
	if tag == "" || publishedAt.IsZero() {
		return "—"
	}
	age := formatRelativeAgo(publishedAt)
	// Available cells: reposReleaseW minus age (≤8 chars worst
	// case like "12mo ago") minus the " · " separator (3) =
	// budget for the tag itself. Truncate the tag, not the age,
	// because the age conveys recency at a glance while a long
	// tag like "v123.456.789-rc.1" is rarely informative past
	// its prefix.
	const overhead = 3 // " · "
	tagBudget := reposReleaseW - cellWidth(age) - overhead
	if tagBudget < 4 {
		tagBudget = 4
	}
	return truncate(tag, tagBudget) + " · " + age
}

// ciDot maps the status-check rollup state to a single coloured
// glyph. The dot is the universally recognised CI indicator
// (green/red/yellow/grey) — same shape lazygit, gh dash and
// github.com itself use. Wide-rune-safe: stays at exactly 1 cell
// no matter the state, so the column never shifts.
//
// On monochromatic themes the four states fall back to distinct
// glyphs (✓ / ✕ / ⋯ / ·) instead of distinct colours — without
// chroma the eye can't tell a "green dot" from a "red dot" on a
// monochrome background.
func ciDot(state string) string {
	if IsMonochromatic() {
		switch state {
		case "SUCCESS":
			return okStyle.Render("✓")
		case "FAILURE", "ERROR":
			return errorStyle.Render("✕")
		case "PENDING", "EXPECTED":
			return warnStyle.Render("⋯")
		default:
			return mutedStyle.Render("·")
		}
	}
	switch state {
	case "SUCCESS":
		return okStyle.Render("●")
	case "FAILURE", "ERROR":
		return errorStyle.Render("●")
	case "PENDING", "EXPECTED":
		return warnStyle.Render("●")
	default:
		// "" (no rollup), or an enum value GitHub adds in the
		// future and we haven't mapped yet: dim dot so the column
		// stays visually aligned without making a claim.
		return mutedStyle.Render("·")
	}
}

// ciSortRank orders CI states for the "failures first" sort. Lower
// rank = surfaces earlier. Tied ranks fall through to the stable
// secondary name sort in sortRepos, so two failing repos stay in
// a predictable alphabetical order between refreshes.
func ciSortRank(state string) int {
	switch state {
	case "FAILURE", "ERROR":
		return 0
	case "PENDING", "EXPECTED":
		return 1
	case "SUCCESS":
		return 2
	default:
		return 3 // no rollup — push to the bottom
	}
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

// openURLCmd returns a tea.Cmd that hands the given URL to the user's
// default browser. Failures are swallowed: on the rare host where the
// platform launcher is missing, the worst case is "Enter does nothing"
// rather than a crash or an obtrusive error toast.
func openURLCmd(url string) tea.Cmd {
	if url == "" {
		return nil
	}
	return func() tea.Msg {
		_ = browse.OpenURL(url)
		return nil
	}
}
