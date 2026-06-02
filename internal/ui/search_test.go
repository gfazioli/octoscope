package ui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// TestUpdateSearchMultiRuneAndPaste pins the search box's input handling
// across all three list tabs: a pasted / fast multi-rune batch is
// appended whole (not dropped), bracketed-paste content lands without
// the brackets, backspace trims by rune (UTF-8 safe), and space still
// appends. These exercise the km.Type dispatch that replaced the old
// len(Runes)==1 guard.
func TestUpdateSearchMultiRuneAndPaste(t *testing.T) {
	runes := func(s string) tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)} }
	paste := func(s string) tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s), Paste: true} }
	space := tea.KeyMsg{Type: tea.KeySpace, Runes: []rune(" ")}
	bs := tea.KeyMsg{Type: tea.KeyBackspace}

	// Repos
	t.Run("repos", func(t *testing.T) {
		rm := ReposModel{searchActive: true}
		rm = rm.updateSearch(runes("oc"))
		rm = rm.updateSearch(runes("to")) // accumulates
		if rm.query != "octo" {
			t.Errorf("multi-rune append: query = %q, want %q", rm.query, "octo")
		}
		rm = rm.updateSearch(space)
		rm = rm.updateSearch(paste("scope"))
		if rm.query != "octo scope" {
			t.Errorf("space + paste: query = %q, want %q", rm.query, "octo scope")
		}
	})

	// PRs
	t.Run("prs", func(t *testing.T) {
		pm := PRsModel{searchActive: true}
		pm = pm.updateSearch(paste("dependabot"))
		if pm.query != "dependabot" {
			t.Errorf("paste: query = %q, want %q (no brackets)", pm.query, "dependabot")
		}
	})

	// Issues
	t.Run("issues", func(t *testing.T) {
		im := IssuesModel{searchActive: true}
		im = im.updateSearch(runes("bug"))
		if im.query != "bug" {
			t.Errorf("multi-rune: query = %q, want %q", im.query, "bug")
		}
	})

	// UTF-8-safe backspace (multibyte glyphs).
	t.Run("backspace is rune-aware", func(t *testing.T) {
		rm := ReposModel{searchActive: true, query: "café"}
		rm = rm.updateSearch(bs)
		if rm.query != "caf" {
			t.Errorf("backspace on 'café' = %q, want %q", rm.query, "caf")
		}
		rm.query = "a🚀"
		rm = rm.updateSearch(bs)
		if rm.query != "a" {
			t.Errorf("backspace on 'a🚀' = %q, want %q", rm.query, "a")
		}
	})

	// enter / esc still toggle.
	t.Run("enter and esc", func(t *testing.T) {
		rm := ReposModel{searchActive: true, query: "x"}
		rm = rm.updateSearch(tea.KeyMsg{Type: tea.KeyEnter})
		if rm.searchActive {
			t.Error("enter should leave search mode")
		}
		rm = ReposModel{searchActive: true, query: "x"}
		rm = rm.updateSearch(tea.KeyMsg{Type: tea.KeyEsc})
		if rm.searchActive || rm.query != "" {
			t.Errorf("esc should clear: active=%v query=%q", rm.searchActive, rm.query)
		}
	})
}
