package ui

import (
	"bytes"
	"fmt"
	"strings"
	"sync"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/formatters"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/alecthomas/chroma/v2/styles"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/gfazioli/octoscope/internal/github"
)

// PRDiffModel is the single-file diff viewer (v0.12.0), the
// deepest level of the PR drill-in. Opened by PRFilesModel when
// the user presses Enter on a file row.
//
// Renders the file's unified-diff patch with chroma's "diff"
// lexer (already in the binary via glamour) so additions /
// deletions / hunk headers get colour-highlighted. Wraps the
// rendered text in a bubbles/viewport for scrolling — most diffs
// fit on a screen but a 400-line refactor patch (just under
// patchLineCap) demands paging.
type PRDiffModel struct {
	open bool
	file github.FileChange

	owner  string
	repo   string
	number int

	viewport viewport.Model

	// Width-keyed cache of the rendered diff, mirroring the
	// markdown body cache on PRDetailModel. chroma highlighting
	// is fast (a few ms per file) but redoing it on every paint
	// shows up on long diffs and on resizes — caching keeps the
	// scroll buttery.
	bodyCache      string
	bodyCacheWidth int
}

// IsOpen reports whether the diff viewer is currently active.
func (dm PRDiffModel) IsOpen() bool {
	return dm.open
}

// Open seeds a fresh diff viewer with the file the user picked
// from the files list. width / height seed the viewport so the
// first paint already has correct dimensions.
func (dm PRDiffModel) Open(file github.FileChange, owner, repo string, number, width, height int) PRDiffModel {
	vp := viewport.New(width, max(1, height-2))
	return PRDiffModel{
		open:     true,
		file:     file,
		owner:    owner,
		repo:     repo,
		number:   number,
		viewport: vp,
	}
}

// Close returns a closed diff viewer (zero value).
func (dm PRDiffModel) Close() PRDiffModel {
	return PRDiffModel{}
}

// Update handles a single key event while the diff viewer owns
// the screen. esc backs out one level (to the files list),
// scroll keys feed the viewport, `o` opens the file on GitHub.
func (dm PRDiffModel) Update(msg tea.KeyMsg, width, height int) (PRDiffModel, tea.Cmd) {
	if !dm.open {
		return dm, nil
	}
	switch msg.String() {
	case "q":
		return dm.Close(), tea.Quit
	case "esc":
		return dm.Close(), nil
	case "o":
		url := fmt.Sprintf("https://github.com/%s/%s/pull/%d/files",
			dm.owner, dm.repo, dm.number)
		return dm, openURLCmd(url)
	case "c":
		return dm, copyPathCmd(dm.file.Path)
	}
	dm = dm.syncViewport(width, height)
	var cmd tea.Cmd
	dm.viewport, cmd = dm.viewport.Update(msg)
	return dm, cmd
}

// syncViewport refreshes the viewport's dimensions + content,
// computing the rendered body once and caching it.
func (dm PRDiffModel) syncViewport(width, height int) PRDiffModel {
	body := dm.bodyForWidth(width)
	dm.bodyCache = body
	dm.bodyCacheWidth = width
	dm.viewport.Width = width
	dm.viewport.Height = max(1, height-2)
	dm.viewport.SetContent(body)
	return dm
}

// bodyForWidth returns the rendered diff body, hitting the
// width-keyed cache when possible. Width affects only the
// header/footer chrome — the diff itself is rendered with
// chroma's terminal formatter which doesn't wrap.
func (dm PRDiffModel) bodyForWidth(width int) string {
	if dm.bodyCache != "" && dm.bodyCacheWidth == width {
		return dm.bodyCache
	}
	return renderDiff(dm.file)
}

// View renders the diff viewer. A heading row with the file
// path, the scrollable diff body, and a footer with key hints.
func (dm PRDiffModel) View(width, height int) string {
	if !dm.open {
		return ""
	}
	heading := boldStyle.Foreground(colAccent).Render(dm.file.Path)
	counts := mutedStyle.Render(fmt.Sprintf("  +%d -%d", dm.file.Additions, dm.file.Deletions))
	hints := mutedStyle.Render("↑↓ scroll · pgup/pgdn page · esc back · o open on github · c copy path")

	body := dm.bodyForWidth(width)
	vp := dm.viewport
	vp.Width = width
	vp.Height = max(1, height-2)
	vp.SetContent(body)

	return heading + counts + "\n" + vp.View() + "\n" + hints
}

// renderDiff is the pure function that turns a FileChange's
// Patch into a coloured diff body. Three paths:
//   - Truncated by our cap → banner pointing at the GitHub URL.
//   - Empty patch (binary file, GitHub-side too-large, or
//     content-less rename) → polite one-liner explaining the
//     section is empty for a reason.
//   - Otherwise → chroma highlight with the "diff" lexer and a
//     dark style consistent with the markdown rendering palette.
//
// Doesn't word-wrap: diffs are inherently column-aware (the
// leading +/-/space marker carries meaning, hunk headers align
// on @@) and wrapping breaks visual scanning. Lines longer than
// the viewport width simply overflow; the viewport scrolls
// horizontally if the user moves with the arrow keys past the
// edge.
func renderDiff(f github.FileChange) string {
	if f.Truncated {
		return mutedStyle.Render(
			"Diff too large to display in-app — open this file on github.com (press o) to read it there.",
		)
	}
	if strings.TrimSpace(f.Patch) == "" {
		switch f.Status {
		case "renamed", "copied":
			return mutedStyle.Render("(no content change — file renamed or copied without modifications)")
		case "removed":
			return mutedStyle.Render("(file removed — no remaining content to diff)")
		default:
			return mutedStyle.Render("(no patch available — likely a binary file or omitted by GitHub)")
		}
	}
	out, err := highlightDiff(f.Patch)
	if err != nil {
		return f.Patch
	}
	return out
}

// highlightDiff runs chroma against the diff lexer + a dark
// style + the terminal256 formatter (24-bit-capable terminals
// pick up colour anyway via lipgloss's auto-detection). Cached
// behind the same single-entry MRU idiom as the markdown
// renderer because building the style + formatter takes a
// non-trivial slice of a frame budget on bigger diffs.
func highlightDiff(patch string) (string, error) {
	lexer := lexers.Get("diff")
	if lexer == nil {
		lexer = lexers.Fallback
	}
	style := diffStyle()
	formatter := formatters.Get("terminal256")
	if formatter == nil {
		formatter = formatters.Fallback
	}

	iter, err := lexer.Tokenise(nil, patch)
	if err != nil {
		return "", err
	}
	var buf bytes.Buffer
	if err := formatter.Format(&buf, style, iter); err != nil {
		return "", err
	}
	return buf.String(), nil
}

// diffStyle returns the chroma style used for diff rendering.
// "monokai" reads well on the dark terminal background octoscope
// targets and its red/green for - / + lines is the universally
// recognised diff palette. Cached so we don't look it up on
// every call.
func diffStyle() *chroma.Style {
	diffStyleOnce.Do(func() {
		s := styles.Get("monokai")
		if s == nil {
			s = styles.Fallback
		}
		cachedDiffStyle = s
	})
	return cachedDiffStyle
}

var (
	diffStyleOnce   sync.Once
	cachedDiffStyle *chroma.Style
)

// max is a tiny helper for height clamping — Go 1.21+ has it in
// the stdlib but we keep the local copy for clarity at the call
// site.
func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
