package ui

import (
	"context"
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

	got, cmd, consumed := gd.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("c")}, 80, 24)
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
	_, cmd, _ := gd.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("c")}, 80, 24)
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

	back, _, _ := multi.Update(tea.KeyMsg{Type: tea.KeyEsc}, 80, 24)
	if !back.IsOpen() || back.mode != gistModeFiles {
		t.Error("esc in a multi-file gist should return to the file list, not close")
	}
	closed, _, _ := back.Update(tea.KeyMsg{Type: tea.KeyEsc}, 80, 24)
	if closed.IsOpen() {
		t.Error("a second esc should close the drill-in")
	}

	single := openDetail(github.GistFileContent{Name: "a.go", Text: "x"})
	out, _, _ := single.Update(tea.KeyMsg{Type: tea.KeyEsc}, 80, 24)
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
