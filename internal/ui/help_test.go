package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/gfazioli/octoscope/internal/github"
)

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
