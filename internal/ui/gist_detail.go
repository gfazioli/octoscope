package ui

// The Gists drill-in: the file list, and the file itself.
//
// Shape note, because it departs from PR detail → files → diff. A gist *is*
// a list of files, so a "detail" level above the file list would be an
// empty frame around one list — the two collapse into one model with two
// modes. `esc` still backs out one level at a time and the title still
// grows a segment per level, which is the part of the convention that
// matters to someone using it.
//
// A one-file gist opens straight to its content. Most gists are one file
// (13 of 16 on a real account), and making everyone step through a
// single-row list to reach the only thing they came for is ceremony.

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/alecthomas/chroma/v2/formatters"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/gfazioli/octoscope/internal/github"
)

// gistDetailMode is which of the two levels is showing.
type gistDetailMode int

const (
	gistModeFiles gistDetailMode = iota
	gistModeContent
)

// GistDetailModel is the drill-in. Three states — loading, error, loaded —
// because unlike the inline expansion it replaced, this one actually
// fetches.
type GistDetailModel struct {
	open    bool
	gist    github.Gist // the row that opened it, for the title while loading
	detail  *github.GistDetail
	err     error
	loading bool

	mode   gistDetailMode
	cursor int // selected file, in files mode

	viewport viewport.Model

	// Highlighting is expensive enough to matter while scrolling, and the
	// content never changes once fetched — so it is rendered once per
	// (file, width) rather than per frame. Same reasoning as the markdown
	// caches on PRDetailModel / IssueDetailModel.
	bodyCache      string
	bodyCacheKey   string
	bodyCacheWidth int
}

// IsOpen reports whether the drill-in is active.
func (gd GistDetailModel) IsOpen() bool { return gd.open }

// Open returns a fresh drill-in in the loading state.
func (gd GistDetailModel) Open(g github.Gist) GistDetailModel {
	return GistDetailModel{
		open:     true,
		gist:     g,
		loading:  true,
		viewport: viewport.New(0, 0),
	}
}

// Close returns the zero value, so nothing survives to the next open.
func (gd GistDetailModel) Close() GistDetailModel { return GistDetailModel{} }

// applyFetched lands the network result.
//
// A gist with exactly one file opens straight into it: the file list would
// have a single row and the only useful key on it would be "enter".
func (gd GistDetailModel) applyFetched(detail *github.GistDetail, err error) GistDetailModel {
	gd.loading = false
	gd.detail = detail
	gd.err = err
	gd.bodyCache, gd.bodyCacheKey, gd.bodyCacheWidth = "", "", 0
	if err == nil && detail != nil && len(detail.Files) == 1 {
		gd.mode = gistModeContent
	}
	return gd
}

// selectedFile is the file under the cursor, if any.
func (gd GistDetailModel) selectedFile() (github.GistFileContent, bool) {
	if gd.detail == nil || len(gd.detail.Files) == 0 {
		return github.GistFileContent{}, false
	}
	i := gd.cursor
	if i < 0 {
		i = 0
	}
	if i >= len(gd.detail.Files) {
		i = len(gd.detail.Files) - 1
	}
	return gd.detail.Files[i], true
}

// Update handles keys while the drill-in is open. The bool reports whether
// the drill-in consumed the key; false means "I closed, pass it on".
func (gd GistDetailModel) Update(msg tea.KeyMsg, client *github.Client, width, height int) (GistDetailModel, tea.Cmd, bool) {
	switch msg.String() {
	case "r":
		// The error state advertises retry, so retry has to exist. It
		// did not: the hint listed `r` while Update had no case for it
		// and the error guard below swallowed the key — a hint
		// describing an intention rather than a behaviour, which is the
		// second time that shape has shipped in this tab.
		// Only from the error state. Accepting it while a fetch is in
		// flight would start a second 30-second request per keypress,
		// and the view already says "Loading…" — there is nothing to
		// retry yet.
		if gd.err != nil {
			gd.loading, gd.err = true, nil
			return gd, fetchGistDetailCmd(client, gd.gist.Name), true
		}

	case "esc":
		// One level at a time — out of the file, then out of the gist.
		// Collapsing both would make esc unpredictable, and a one-file
		// gist has no file list to fall back to anyway.
		if gd.mode == gistModeContent && gd.detail != nil && len(gd.detail.Files) > 1 {
			gd.mode = gistModeFiles
			gd.viewport.SetYOffset(0)
			return gd, nil, true
		}
		return gd.Close(), nil, true

	case "o":
		if gd.detail != nil {
			return gd, openURLCmd(gd.detail.URL), true
		}
		return gd, openURLCmd(gd.gist.URL), true

	case "c":
		// The whole reason this view exists: inside a file, `c` copies the
		// **code**, not the link. On the file list there is no single body
		// to take, so it falls back to the gist's URL — the same thing the
		// list tab's `c` does.
		if gd.mode == gistModeContent {
			if f, ok := gd.selectedFile(); ok && !f.IsImage {
				return gd, copyTextCmd(f.Text, f.Name), true
			}
		}
		if gd.detail != nil {
			return gd, copyURLCmd(gd.detail.URL), true
		}
		return gd, copyURLCmd(gd.gist.URL), true
	}

	if gd.loading || gd.err != nil || gd.detail == nil {
		return gd, nil, true
	}

	if gd.mode == gistModeFiles {
		switch msg.String() {
		case "up", "k":
			if gd.cursor > 0 {
				gd.cursor--
			}
		case "down", "j":
			if gd.cursor < len(gd.detail.Files)-1 {
				gd.cursor++
			}
		case "home", "g":
			gd.cursor = 0
		case "end", "G":
			gd.cursor = len(gd.detail.Files) - 1
		case "enter", "d":
			gd.mode = gistModeContent
			gd.viewport.SetYOffset(0)
		}
		return gd, nil, true
	}

	// Content mode: the viewport owns scrolling.
	//
	// The sync has to happen HERE, on the model that is returned and
	// stored — not in View. View takes a value receiver, so syncing there
	// sets the dimensions and content on a copy that is discarded at the
	// end of the frame, leaving the stored viewport 0x0 and empty. It then
	// has nothing to scroll and the arrow keys do nothing, which is
	// exactly how this shipped and exactly what the maintainer hit.
	// IssueDetailModel.Update has always done it in this order.
	gd = gd.syncViewport(width, height)
	var cmd tea.Cmd
	gd.viewport, cmd = gd.viewport.Update(msg)
	return gd, cmd, true
}

// syncViewport refreshes the content and dimensions, rendering the body at
// most once per (file, width).
func (gd GistDetailModel) syncViewport(width, height int) GistDetailModel {
	if gd.loading || gd.err != nil || gd.detail == nil || gd.mode != gistModeContent {
		return gd
	}
	f, ok := gd.selectedFile()
	if !ok {
		return gd
	}
	if gd.bodyCache == "" || gd.bodyCacheKey != f.Name || gd.bodyCacheWidth != width {
		gd.bodyCache = renderGistFileBody(f, width)
		gd.bodyCacheKey = f.Name
		gd.bodyCacheWidth = width
	}
	gd.viewport.Width = width
	gd.viewport.Height = height
	gd.viewport.SetContent(gd.bodyCache)
	return gd
}

// View paints the drill-in.
func (gd GistDetailModel) View(width, height int) string {
	title := gd.renderTitle()

	switch {
	case gd.loading:
		return title + "\n\n" + mutedStyle.Render("Loading…")
	case gd.err != nil:
		return title + "\n\n" +
			errorStyle.Render("Could not load this gist.") + "\n" +
			mutedStyle.Render(gd.err.Error()) + "\n\n" +
			keyHints("r", "retry", "esc", "back")
	case gd.detail == nil:
		return title + "\n\n" + mutedStyle.Render("(nothing to show)")
	}

	if len(gd.detail.Files) == 0 {
		return title + "\n\n" + mutedStyle.Render("(this gist has no files)") +
			"\n\n" + keyHints("o", "github", "esc", "back")
	}

	if gd.mode == gistModeFiles {
		return title + "\n\n" + gd.renderFileList() + "\n\n" +
			keyHints("↑↓", "move", "enter", "open file", "o", "github", "c", "copy url", "esc", "back")
	}

	// First frame only: Update has not run yet, so the viewport is still
	// empty. Syncing here fills it; on every later frame Update has
	// already done it, and re-doing it would reset the scroll offset.
	//
	// A resize is therefore NOT handled here — View works on a copy, so a
	// sync at this point could not persist anyway. The root model re-syncs
	// on tea.WindowSizeMsg, which is where the Overview and Activity
	// viewports are already handled for the same reason.
	if gd.viewport.Height == 0 {
		gd = gd.syncViewport(width, height-4)
	}
	f, _ := gd.selectedFile()
	hints := []string{"↑↓", "scroll", "c", "copy file", "o", "github", "esc", "back"}
	if f.IsImage {
		hints = []string{"o", "github", "esc", "back"}
	}
	return title + "\n\n" + gd.viewport.View() + "\n" + keyHints(hints...)
}

// renderTitle grows a segment per level, so the breadcrumb says where you
// are and the hints never advertise a level you are already inside.
func (gd GistDetailModel) renderTitle() string {
	label := gistLabel(gd.gist)
	crumb := "▸ Gists / " + label
	if gd.mode == gistModeContent {
		if f, ok := gd.selectedFile(); ok {
			crumb += " / " + f.Name
		}
	}
	vis := ""
	if gd.detail != nil && !gd.detail.IsPublic {
		vis = mutedStyle.Render("  secret")
	}
	return sectionTitleStyle.Render(crumb) + vis
}

func (gd GistDetailModel) renderFileList() string {
	var b strings.Builder
	for i, f := range gd.detail.Files {
		marker := "  "
		name := f.Name
		if i == gd.cursor {
			marker = activeTabStyle.Render("▸ ")
			name = boldStyle.Foreground(colAccent).Render(name)
		}
		lang := f.Language
		if lang == "" {
			lang = "—"
		}
		meta := fmt.Sprintf("  %s  %s", lang, humanBytes(f.Size))
		if f.IsImage {
			meta += "  (binary)"
		} else if f.IsTruncated {
			meta += "  (truncated by GitHub)"
		}
		b.WriteString(marker + name + mutedStyle.Render(meta))
		if i < len(gd.detail.Files)-1 {
			b.WriteString("\n")
		}
	}
	return b.String()
}

// renderGistFileBody is the file itself: syntax-highlighted where that
// makes sense, and honestly refused where it does not.
func renderGistFileBody(f github.GistFileContent, width int) string {
	if f.IsImage {
		return mutedStyle.Render(
			"This file is binary, so there is nothing readable to show here.\n" +
				"Press o to open the gist on GitHub.")
	}

	body := f.Text
	if strings.TrimSpace(body) == "" {
		return mutedStyle.Render("(empty file)")
	}

	out := body
	if !IsMonochromatic() {
		// A monochromatic theme promises a single tonal palette, and
		// chroma's output is full of external semantic colour — so under
		// those themes the file is shown plain rather than breaking the
		// promise. Same rule the language bars and the CI dot follow.
		if hi, err := highlightGistFile(f.Name, body); err == nil {
			out = hi
		}
	}

	if f.IsTruncated {
		// GitHub cut the file, so say so rather than let the last line look
		// like the end of the code.
		out += "\n\n" + warnStyle.Render(
			"— GitHub truncated this file, so the rest is not shown. Press o for the full gist.")
	}
	return out
}

// highlightGistFile picks a lexer from the filename, which is what a gist
// actually gives us to go on — the language name GitHub reports is a
// display string, while chroma matches on extension.
func highlightGistFile(filename, body string) (string, error) {
	lexer := lexers.Match(filename)
	if lexer == nil {
		lexer = lexers.Analyse(body)
	}
	if lexer == nil {
		lexer = lexers.Fallback
	}
	formatter := formatters.Get("terminal256")
	if formatter == nil {
		formatter = formatters.Fallback
	}
	iter, err := lexer.Tokenise(nil, body)
	if err != nil {
		return "", err
	}
	var buf bytes.Buffer
	if err := formatter.Format(&buf, diffStyle(), iter); err != nil {
		return "", err
	}
	return buf.String(), nil
}

// gistDetailFetchedMsg carries the drill-in fetch back to the root model.
// The gist's hash is the correlation key: it is unique, it is already on
// the row, and a late response for a gist the user has navigated away from
// has to be dropped rather than painted over the current one.
type gistDetailFetchedMsg struct {
	name   string
	detail *github.GistDetail
	err    error
}

// fetchGistDetailCmd pulls one gist's file contents. 30s, the same budget
// the other drill-ins use.
func fetchGistDetailCmd(client *github.Client, name string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		detail, err := client.FetchGistDetail(ctx, name)
		return gistDetailFetchedMsg{name: name, detail: detail, err: err}
	}
}

// SyncSize re-fits the viewport after a terminal resize. Exported for the
// root model's tea.WindowSizeMsg handler, which is the only place that
// knows the new dimensions — View cannot do it, because it works on a copy
// that is discarded before the next key arrives.
func (gd GistDetailModel) SyncSize(width, height int) GistDetailModel {
	if !gd.open {
		return gd
	}
	return gd.syncViewport(width, height)
}
