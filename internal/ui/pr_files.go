package ui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/gfazioli/octoscope/internal/github"
	"github.com/mattn/go-runewidth"
)

// PRFilesModel is the files-list sub-view of the PR drill-in
// (v0.12.0). Nested inside PRDetailModel: PRDetailModel keys this
// open with `f` once a PR detail has loaded with a non-empty
// Files slice, and routes key events to it while it owns the
// screen.
//
// Full-screen sub-view by design — the v0.12.0 UX question landed
// on "drill-in nel drill-in" rather than an inline expandable
// section. The PR detail's banner / title row stays painted by
// the parent, so we render below it: a heading row that names the
// PR, a navigable file list, and a footer with key hints.
//
// Selecting a file with `enter` (or `space`) opens the file's
// diff in PRDiffModel — another level of nesting handled here so
// PRDetailModel stays a single transparent wrapper.
type PRFilesModel struct {
	open   bool
	files  []github.FileChange
	cursor int

	// Header context — owner / repo / number copied from the PR
	// at Open() time so the heading line stays informative even
	// after the user has navigated several levels deep.
	owner  string
	repo   string
	number int

	diff PRDiffModel
}

// IsOpen reports whether the files list is currently active.
func (fm PRFilesModel) IsOpen() bool {
	return fm.open
}

// Open seeds a fresh files list. Caller has already verified
// that len(files) > 0; an empty list would render a useless
// "no files" placeholder, which we'd rather avoid by suppressing
// the `f` keybind upstream.
func (fm PRFilesModel) Open(files []github.FileChange, owner, repo string, number int) PRFilesModel {
	return PRFilesModel{
		open:   true,
		files:  files,
		cursor: 0,
		owner:  owner,
		repo:   repo,
		number: number,
	}
}

// Close returns a closed files list (zero value), tearing down
// any nested diff viewer along with it.
func (fm PRFilesModel) Close() PRFilesModel {
	return PRFilesModel{}
}

// Update handles one key event while the list is the active
// surface. If the nested diff viewer is open it receives the
// message first; otherwise the list dispatches navigation /
// open / back keys itself.
func (fm PRFilesModel) Update(msg tea.KeyMsg, width, height int) (PRFilesModel, tea.Cmd) {
	if !fm.open {
		return fm, nil
	}
	if fm.diff.IsOpen() {
		var cmd tea.Cmd
		fm.diff, cmd = fm.diff.Update(msg, width, height)
		// Diff viewer signals "close me" by reporting !IsOpen()
		// after the update. The wrapper just keeps the (now-empty)
		// diff field and re-renders the list — no extra plumbing
		// needed.
		return fm, cmd
	}
	switch msg.String() {
	case "q":
		return fm.Close(), tea.Quit
	case "esc":
		return fm.Close(), nil
	case "up", "k":
		if fm.cursor > 0 {
			fm.cursor--
		}
		return fm, nil
	case "down", "j":
		if fm.cursor < len(fm.files)-1 {
			fm.cursor++
		}
		return fm, nil
	case "home", "g":
		fm.cursor = 0
		return fm, nil
	case "end", "G":
		fm.cursor = len(fm.files) - 1
		return fm, nil
	case "enter", " ":
		if len(fm.files) == 0 {
			return fm, nil
		}
		fm.diff = fm.diff.Open(fm.files[fm.cursor], fm.owner, fm.repo, fm.number, width, height)
		return fm, nil
	case "o":
		if len(fm.files) == 0 {
			return fm, nil
		}
		url := fileBlobURL(fm.owner, fm.repo, fm.number, fm.files[fm.cursor].Path)
		return fm, openURLCmd(url)
	case "c":
		if len(fm.files) == 0 {
			return fm, nil
		}
		return fm, copyPathCmd(fm.files[fm.cursor].Path)
	}
	return fm, nil
}

// View renders the files list (or the nested diff viewer, when
// open). Width and height are the area the PRDetailModel hands
// us below its sticky title row.
func (fm PRFilesModel) View(width, height int) string {
	if !fm.open {
		return ""
	}
	if fm.diff.IsOpen() {
		return fm.diff.View(width, height)
	}

	// No breadcrumb / heading here — the parent PRDetailModel's
	// title bar already shows "▸ PRs / owner/repo#NN / Files"
	// when we're the active sub-view. A second header would
	// duplicate the context.
	count := mutedStyle.Render(fmt.Sprintf("%d files changed", len(fm.files)))
	hints := keyHints(
		"↑↓", "move",
		"enter", "inspect",
		"o", "open on github",
		"c", "copy path",
		"esc", "back",
		"q", "quit",
	)

	rowsBudget := height - 4 // count + blank + footer + blank
	if rowsBudget < 1 {
		rowsBudget = 1
	}
	rows := renderFileRows(fm.files, fm.cursor, width, rowsBudget)

	return count + "\n\n" + rows + "\n" + hints
}

// renderFileRows turns the file slice into the visible viewport
// of rows, applying cursor highlighting and a window calculation
// so the cursor never falls off the visible area. A real
// bubbles/viewport would be overkill here — the list never has
// more than maxFiles (300) entries, well within plain-string
// territory.
func renderFileRows(files []github.FileChange, cursor, width, budget int) string {
	if len(files) == 0 {
		return mutedStyle.Render("  (no files)")
	}
	start := 0
	if cursor >= budget {
		start = cursor - budget + 1
	}
	end := start + budget
	if end > len(files) {
		end = len(files)
	}

	var b strings.Builder
	for i := start; i < end; i++ {
		b.WriteString(fileRow(files[i], width, i == cursor))
		if i < end-1 {
			b.WriteByte('\n')
		}
	}
	return b.String()
}

// fileRow renders one row: cursor marker, status glyph, path,
// +Additions / -Deletions counts. Mirrors the highlight idiom of
// the list tabs (marker "▸" + accent recolour on the active
// row) so the muscle memory carries over from Repos / PRs /
// Issues. Truncates the path on the left (…prefix) when it
// would overflow the available width — left-trim because the
// filename (rightmost segment) is what the user is reading;
// dropping it would defeat the row.
func fileRow(f github.FileChange, width int, active bool) string {
	statusGlyph := fileStatusGlyph(f.Status)
	addDel := okStyle.Render(fmt.Sprintf("+%d", f.Additions)) +
		" " + errorStyle.Render(fmt.Sprintf("-%d", f.Deletions))
	marker := "  "
	if active {
		marker = activeTabStyle.Render("▸ ")
	}
	// Reserve room for marker (2) + " glyph " (3) + addDel
	// (~18 cols worst-case on a busy PR).
	const overhead = 2 + 3 + 18
	pathBudget := width - overhead
	if pathBudget < 10 {
		pathBudget = 10
	}
	path := truncatePathLeft(f.Path, pathBudget)
	if active {
		path = boldStyle.Foreground(colAccent).Render(path)
	}
	return fmt.Sprintf("%s%s %s   %s", marker, statusGlyph, path, addDel)
}

// truncatePathLeft trims the prefix of `path` so the rendered
// result fits in at most `w` terminal cells, prepending an
// ellipsis when anything had to drop. UTF-8 / wide-rune safe:
// iterates the runes from right to left and tracks display
// width with runewidth, never slicing a multi-byte rune in
// half.
//
// Mirror of repos.go's `truncate` but trimming the *left*: for
// file paths the right end is the filename the user actually
// scans for, so the prefix is the cheaper thing to drop.
func truncatePathLeft(path string, w int) string {
	if w <= 1 {
		return "…"
	}
	if runewidth.StringWidth(path) <= w {
		return path
	}
	limit := w - 1 // room for the leading ellipsis
	runes := []rune(path)
	used := 0
	cut := len(runes)
	for i := len(runes) - 1; i >= 0; i-- {
		rw := runewidth.RuneWidth(runes[i])
		if used+rw > limit {
			break
		}
		used += rw
		cut = i
	}
	return "…" + string(runes[cut:])
}

// fileStatusGlyph maps GitHub's REST status enum to a one-rune
// indicator. Stays single-rune so cursor highlighting and the
// fileRow width budget stay predictable.
func fileStatusGlyph(status string) string {
	switch status {
	case "added":
		return okStyle.Render("A")
	case "removed":
		return errorStyle.Render("D")
	case "renamed":
		return boldStyle.Foreground(colAccent).Render("R")
	case "copied":
		return mutedStyle.Render("C")
	case "modified", "changed":
		return mutedStyle.Render("M")
	default:
		return mutedStyle.Render("·")
	}
}

// fileBlobURL builds the github.com URL of the PR's "Files
// changed" tab. Best-effort fallback when the in-app diff viewer
// doesn't fit a use case — drops the user on the page GitHub
// renders the same diff in a browser.
//
// No #diff-<sha> fragment: GitHub uses SHA-256 of the path for
// the per-file anchor, and synthesising it would buy us only an
// auto-scroll to the right file. Worth revisiting if users start
// asking for "deep-link to this specific file on github.com";
// today the page header has a navigable file tree anyway.
func fileBlobURL(owner, repo string, number int, _ string) string {
	return fmt.Sprintf("https://github.com/%s/%s/pull/%d/files",
		owner, repo, number)
}
