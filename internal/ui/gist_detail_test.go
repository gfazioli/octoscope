package ui

import (
	"context"
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/gfazioli/octoscope/internal/github"
)

func detail(files ...github.GistFileContent) *github.GistDetail {
	return &github.GistDetail{
		Name:        "hash1",
		Description: "settings sync",
		URL:         "https://gist.github.com/u/hash1",
		IsPublic:    false,
		Files:       files,
	}
}

func openDetail(files ...github.GistFileContent) GistDetailModel {
	gd := GistDetailModel{}.Open(github.Gist{Name: "hash1", Description: "settings sync"})
	return gd.applyFetched(detail(files...), nil)
}

// A gist with one file opens straight into it. Most gists are one file, and
// making everyone step through a single-row list to reach the only thing
// they came for is ceremony.
func TestGistDetailSkipsTheFileListForASingleFile(t *testing.T) {
	gd := openDetail(github.GistFileContent{Name: "a.go", Text: "package main", Language: "Go"})
	if gd.mode != gistModeContent {
		t.Error("a one-file gist did not open straight into its content")
	}

	multi := openDetail(
		github.GistFileContent{Name: "a.go", Text: "x"},
		github.GistFileContent{Name: "b.go", Text: "y"},
	)
	if multi.mode != gistModeFiles {
		t.Error("a multi-file gist should land on the file list first")
	}
}

// The whole reason this view exists: inside a file, `c` takes the code.
// Copying the URL there would leave the user exactly where the first cut of
// this tab left them — with a link to something they still cannot read.
func TestGistDetailCopiesTheFileBodyNotTheURL(t *testing.T) {
	const body = "package main\n\nfunc main() {}\n"
	gd := openDetail(github.GistFileContent{Name: "a.go", Text: body, Language: "Go"})

	got, cmd, consumed := gd.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("c")}, nil, 80, 24)
	if !consumed || cmd == nil {
		t.Fatal("c produced no command in content mode")
	}
	_ = got

	msg := cmd()
	copied, ok := msg.(urlCopiedMsg)
	if !ok {
		t.Fatalf("unexpected message type %T", msg)
	}
	if copied.noun != "a.go" {
		t.Errorf("toast says %q; it should name the file that was copied", copied.noun)
	}
}

// On the file list there is no single body to take, so `c` falls back to
// the gist's URL — the same thing the list tab's `c` does, rather than
// silently copying whichever file happens to be under the cursor.
func TestGistDetailCopiesTheURLFromTheFileList(t *testing.T) {
	gd := openDetail(
		github.GistFileContent{Name: "a.go", Text: "x"},
		github.GistFileContent{Name: "b.go", Text: "y"},
	)
	_, cmd, _ := gd.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("c")}, nil, 80, 24)
	if cmd == nil {
		t.Fatal("c produced no command on the file list")
	}
	copied := cmd().(urlCopiedMsg)
	if copied.noun != "URL" {
		t.Errorf("file-list c copied %q, want the URL", copied.noun)
	}
}

// esc backs out one level at a time. Collapsing both would make it
// unpredictable — and a one-file gist has no list to fall back to, so it
// closes in one press.
func TestGistDetailEscBacksOutOneLevel(t *testing.T) {
	multi := openDetail(
		github.GistFileContent{Name: "a.go", Text: "x"},
		github.GistFileContent{Name: "b.go", Text: "y"},
	)
	multi.mode = gistModeContent

	back, _, _ := multi.Update(tea.KeyMsg{Type: tea.KeyEsc}, nil, 80, 24)
	if !back.IsOpen() || back.mode != gistModeFiles {
		t.Error("esc in a multi-file gist should return to the file list, not close")
	}
	closed, _, _ := back.Update(tea.KeyMsg{Type: tea.KeyEsc}, nil, 80, 24)
	if closed.IsOpen() {
		t.Error("a second esc should close the drill-in")
	}

	single := openDetail(github.GistFileContent{Name: "a.go", Text: "x"})
	out, _, _ := single.Update(tea.KeyMsg{Type: tea.KeyEsc}, nil, 80, 24)
	if out.IsOpen() {
		t.Error("a one-file gist has no list to go back to; esc should close it")
	}
}

// Binary and truncated files are declined rather than rendered. Neither can
// be inferred from the size, which is why they arrive as flags.
func TestGistDetailDeclinesBinaryAndFlagsTruncation(t *testing.T) {
	img := renderGistFileBody(github.GistFileContent{Name: "logo.png", IsImage: true}, 80)
	if !strings.Contains(ansi.Strip(img), "binary") {
		t.Errorf("a binary file was not declined:\n%s", img)
	}

	cut := renderGistFileBody(github.GistFileContent{
		Name: "big.txt", Text: "some content", IsTruncated: true,
	}, 80)
	if !strings.Contains(ansi.Strip(cut), "truncated") {
		t.Errorf("a truncated file did not say so, so its last line reads as the end:\n%s", cut)
	}

	empty := renderGistFileBody(github.GistFileContent{Name: "e.txt", Text: "   "}, 80)
	if !strings.Contains(ansi.Strip(empty), "empty file") {
		t.Errorf("an empty file rendered as blank space:\n%s", empty)
	}
}

// The breadcrumb grows a segment per level, so the title says where you are
// rather than only which gist you are in.
func TestGistDetailTitleGrowsPerLevel(t *testing.T) {
	gd := openDetail(
		github.GistFileContent{Name: "cloudSettings", Text: "x"},
		github.GistFileContent{Name: "extensions.json", Text: "y"},
	)
	list := ansi.Strip(gd.renderTitle())
	if !strings.Contains(list, "Gists / settings sync") || strings.Contains(list, "cloudSettings") {
		t.Errorf("file-list title should stop at the gist: %q", list)
	}

	gd.mode = gistModeContent
	content := ansi.Strip(gd.renderTitle())
	if !strings.Contains(content, "settings sync / cloudSettings") {
		t.Errorf("content title should name the file too: %q", content)
	}
	// A secret gist says so here, since the row that said it is no longer
	// on screen.
	if !strings.Contains(content, "secret") {
		t.Errorf("a secret gist does not disclose that in the drill-in: %q", content)
	}
}

// The loading and error states exist because this one fetches — the state
// the inline expansion it replaced never needed.
func TestGistDetailStates(t *testing.T) {
	loading := GistDetailModel{}.Open(github.Gist{Name: "h", Description: "d"})
	if !strings.Contains(ansi.Strip(loading.View(80, 24)), "Loading") {
		t.Error("the loading state does not say so")
	}

	failed := loading.applyFetched(nil, context.DeadlineExceeded)
	out := ansi.Strip(failed.View(80, 24))
	if !strings.Contains(out, "Could not load") {
		t.Errorf("the error state does not explain itself:\n%s", out)
	}
	if !strings.Contains(out, "esc") {
		t.Errorf("the error state offers no way out:\n%s", out)
	}
}

// Scrolling has to work, and the way it broke is worth pinning: the
// viewport sync ran in View, which takes a value receiver — so the
// dimensions and content landed on a copy that was discarded at the end of
// the frame, leaving the stored viewport 0x0 and empty. Update then had
// nothing to scroll and the arrow keys did nothing.
//
// Asserting on the model Update returns is what catches that; asserting on
// View's output would have passed, because View re-synced its own copy
// every frame and looked perfectly correct.
func TestGistDetailContentScrolls(t *testing.T) {
	var body strings.Builder
	for i := 0; i < 200; i++ {
		fmt.Fprintf(&body, "line %03d\n", i)
	}
	gd := openDetail(github.GistFileContent{Name: "long.txt", Text: body.String()})

	// A key press has to leave the STORED model with a usable viewport.
	gd, _, _ = gd.Update(tea.KeyMsg{Type: tea.KeyDown}, nil, 80, 20)
	if gd.viewport.Height == 0 {
		t.Fatal("the stored viewport has no height, so nothing can scroll — " +
			"the sync landed on a discarded copy")
	}
	if gd.viewport.YOffset == 0 {
		t.Error("down did not move the viewport")
	}

	before := gd.viewport.YOffset
	gd, _, _ = gd.Update(tea.KeyMsg{Type: tea.KeyPgDown}, nil, 80, 20)
	if gd.viewport.YOffset <= before {
		t.Errorf("page-down did not advance: %d -> %d", before, gd.viewport.YOffset)
	}

	// Compare against the offset AFTER the page-down, not the one from
	// before it — otherwise a no-op Up passes on the strength of the
	// page-down's movement. Caught by CodeRabbit, in a test written
	// specifically to catch a bug of that shape.
	afterPage := gd.viewport.YOffset
	gd, _, _ = gd.Update(tea.KeyMsg{Type: tea.KeyUp}, nil, 80, 20)
	if gd.viewport.YOffset >= afterPage {
		t.Errorf("up did not move the viewport back: %d -> %d", afterPage, gd.viewport.YOffset)
	}

	// The render must show the scrolled position, not the top. Asserting
	// that View leaves YOffset alone would be inert: View has a value
	// receiver, so the caller's copy cannot change either way.
	out := ansi.Strip(gd.View(80, 24))
	if strings.Contains(out, "line 000") {
		t.Errorf("the render fell back to the first line despite a scroll offset of %d:\n%s",
			gd.viewport.YOffset, out)
	}
}

// Retry must not stack requests: each press while a fetch is in flight
// would start another 30-second GraphQL call, and there is nothing to
// retry yet — the view already says it is loading.
func TestGistDetailRetryOnlyFromTheErrorState(t *testing.T) {
	loading := GistDetailModel{}.Open(github.Gist{Name: "h"})
	_, cmd, _ := loading.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("r")}, nil, 80, 24)
	if cmd != nil {
		t.Error("r during a fetch started a second one")
	}

	failed := loading.applyFetched(nil, context.DeadlineExceeded)
	got, cmd, _ := failed.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("r")}, nil, 80, 24)
	if cmd == nil {
		t.Error("r from the error state did not refetch")
	}
	if !got.loading || got.err != nil {
		t.Errorf("retry did not return to the loading state: loading=%v err=%v", got.loading, got.err)
	}
}

// A resize has to reach the viewport. View works on a copy, so it cannot
// persist one — the root model re-syncs on tea.WindowSizeMsg, the same way
// it already does for the Overview and Activity viewports.
func TestGistDetailRefitsOnResize(t *testing.T) {
	gd := openDetail(github.GistFileContent{Name: "a.txt", Text: strings.Repeat("x\n", 200)})
	gd, _, _ = gd.Update(tea.KeyMsg{Type: tea.KeyDown}, nil, 80, 20)
	first := gd.viewport.Height

	gd = gd.SyncSize(120, 40)
	if gd.viewport.Height == first {
		t.Errorf("the viewport kept its old height (%d) through a resize", first)
	}
	if gd.viewport.Width != 120 {
		t.Errorf("width = %d, want 120", gd.viewport.Width)
	}
}

// Two drill-ins must never be open at once, and the orders that decide
// which one wins must agree.
//
// They did not: View painted gistDetail before issueDetail while the key
// router reached issueDetail first, so with both open the keystrokes went
// to a view nobody could see. And none of the four competing open handlers
// closed gistDetail, so a delayed message could produce exactly that pair.
func TestOnlyOneDrillInIsEverOpen(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "test-token-not-used")
	client, err := github.New("octocat", github.Options{})
	if err != nil {
		t.Fatalf("github.New: %v", err)
	}
	m := NewModel(client, "0.29.0", Options{})

	// Open the gist drill-in, then let a competing open arrive late.
	updated, _ := m.Update(viewGistDetailMsg{gist: github.Gist{Name: "h", Description: "d"}})
	m = updated.(Model)
	if !m.gistDetail.IsOpen() {
		t.Fatal("the gist drill-in did not open")
	}

	updated, _ = m.Update(viewIssueDetailMsg{issue: github.Issue{
		Number: 1, URL: "https://github.com/o/r/issues/1",
	}})
	m = updated.(Model)
	if m.gistDetail.IsOpen() {
		t.Error("opening the issue drill-in left the gist one open — " +
			"View would paint the gist while Update fed the issue")
	}

	// And the reverse: a gist open must close whatever was there.
	updated, _ = m.Update(viewIssueDetailMsg{issue: github.Issue{
		Number: 2, URL: "https://github.com/o/r/issues/2",
	}})
	m = updated.(Model)
	updated, _ = m.Update(viewGistDetailMsg{gist: github.Gist{Name: "h2"}})
	m = updated.(Model)
	if m.issueDetail.IsOpen() {
		t.Error("opening the gist drill-in left the issue one open")
	}
}
