package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/gfazioli/octoscope/internal/github"
)

// newPlainModel builds a Model with no splash (ShowSponsor off) for
// driving key routing. GITHUB_TOKEN keeps auth.Token() subprocess-free.
func newPlainModel(t *testing.T) Model {
	t.Helper()
	t.Setenv("GITHUB_TOKEN", "test-token-not-used")
	_ = applyTheme("octoscope", "")
	client, err := github.New("octocat", github.Options{})
	if err != nil {
		t.Fatalf("github.New: %v", err)
	}
	return NewModel(client, "0.16.0", Options{})
}

// TestHelpOverlay covers the `?` help overlay: opening, rendering the
// keymap, dismiss-on-any-key, and that ctrl+c is NOT absorbed (it quits).
func TestHelpOverlay(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "test-token-not-used")
	_ = applyTheme("octoscope", "")
	client, err := github.New("octocat", github.Options{})
	if err != nil {
		t.Fatalf("github.New: %v", err)
	}
	base := NewModel(client, "0.16.0", Options{}) // splash off

	// `?` opens the overlay.
	u, _ := base.Update(key("?"))
	m := u.(Model)
	if !m.help.IsOpen() {
		t.Fatal("'?' should open the help overlay")
	}

	// It paints the keymap (even while still loading — open ⟺ visible).
	out := ansi.Strip(m.View())
	for _, want := range []string{"Keyboard shortcuts", "jump to tab", "settings", "quit"} {
		if !strings.Contains(out, want) {
			t.Errorf("help render missing %q:\n%s", want, out)
		}
	}

	// Any key dismisses.
	if u2, _ := m.Update(key("x")); u2.(Model).help.IsOpen() {
		t.Error("any key should dismiss the help overlay")
	}

	// ctrl+c is not absorbed — it falls through to quit, leaving help open.
	uc, cmd := m.Update(key("ctrl+c"))
	if !uc.(Model).help.IsOpen() {
		t.Error("ctrl+c should not dismiss help (it quits the app)")
	}
	if cmd == nil {
		t.Error("ctrl+c should return a quit cmd")
	}
}

// TestHelpNotOpenedWhileFiltering pins the safety invariant the `?`
// handler promises: while a list filter is being typed, `?` is a literal
// filter character, NOT a hotkey. Guards against an Update-dispatcher
// reorder that would let `?` hijack the search box.
func TestHelpNotOpenedWhileFiltering(t *testing.T) {
	m := newPlainModel(t)

	u, _ := m.Update(key("2")) // Repos tab
	m = u.(Model)
	u, _ = m.Update(key("/")) // enter filter mode
	m = u.(Model)
	if !m.repos.IsInputMode() {
		t.Fatal("'/' should put the Repos tab into filter input mode")
	}

	u, _ = m.Update(key("?")) // ? while typing a filter
	m = u.(Model)
	if m.help.IsOpen() {
		t.Error("'?' must NOT open help while a filter is being typed")
	}
	if !m.repos.IsInputMode() {
		t.Error("'?' should be consumed as a filter char, leaving input mode active")
	}
}

// TestHelpDoesNotOpenOverSettings pins the modal priority: a settings
// panel that is already open absorbs `?` (it does not open help on top).
func TestHelpDoesNotOpenOverSettings(t *testing.T) {
	m := newPlainModel(t)

	u, _ := m.Update(key(",")) // open settings
	m = u.(Model)
	if !m.settings.IsOpen() {
		t.Fatal("',' should open the settings panel")
	}

	u, _ = m.Update(key("?"))
	m = u.(Model)
	if m.help.IsOpen() {
		t.Error("'?' must not open help while the settings panel is open")
	}
	if !m.settings.IsOpen() {
		t.Error("settings should keep focus")
	}
}

// TestHelpRendersWithDashboardChrome exercises the LOADED render path
// (the modal switch), not just the loading-screen top-check: with stats
// present, the help overlay still paints.
func TestHelpRendersWithDashboardChrome(t *testing.T) {
	m := newPlainModel(t)
	u, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m = u.(Model)
	m.stats = &github.Stats{} // non-nil → past the loading top-check
	m.loading = false

	u, _ = m.Update(key("?"))
	m = u.(Model)
	out := ansi.Strip(m.View())
	if !strings.Contains(out, "Keyboard shortcuts") {
		t.Errorf("help should render via the loaded modal switch:\n%s", out)
	}
}
