package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/gfazioli/octoscope/internal/github"
)

// newSponsorModel builds a Model with the splash open (show_sponsor on,
// not public-only). GITHUB_TOKEN keeps auth.Token() subprocess-free.
func newSponsorModel(t *testing.T) Model {
	t.Helper()
	t.Setenv("GITHUB_TOKEN", "test-token-not-used")
	_ = applyTheme("octoscope", "")
	client, err := github.New("octocat", github.Options{})
	if err != nil {
		t.Fatalf("github.New: %v", err)
	}
	m := NewModel(client, "test", Options{ShowSponsor: true})
	if !m.sponsor.IsOpen() {
		t.Fatal("precondition: splash should be open after NewModel")
	}
	return m
}

func key(s string) tea.KeyMsg {
	switch s {
	case "ctrl+c":
		return tea.KeyMsg{Type: tea.KeyCtrlC}
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	default:
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
	}
}

// TestSponsorKeyRouting pins the modal's whole interaction contract,
// which the gating test can't reach: o/c fire a cmd and dismiss, any
// other key dismisses with no cmd, and ctrl+c is NOT absorbed (it falls
// through to the global quit handler, leaving the splash open).
func TestSponsorKeyRouting(t *testing.T) {
	cases := []struct {
		key           string
		wantStillOpen bool
		wantCmd       bool
	}{
		{"o", false, true},      // open browser + dismiss
		{"c", false, true},      // copy URL + dismiss
		{"x", false, false},     // arbitrary key dismisses, no cmd
		{"enter", false, false}, // same
		{"ctrl+c", true, true},  // NOT absorbed: splash stays, quit cmd returned
	}
	for _, c := range cases {
		t.Run(c.key, func(t *testing.T) {
			m := newSponsorModel(t)
			updated, cmd := m.Update(key(c.key))
			got := updated.(Model)
			if got.sponsor.IsOpen() != c.wantStillOpen {
				t.Errorf("%s: sponsor.IsOpen() = %v, want %v", c.key, got.sponsor.IsOpen(), c.wantStillOpen)
			}
			if (cmd != nil) != c.wantCmd {
				t.Errorf("%s: cmd != nil = %v, want %v", c.key, cmd != nil, c.wantCmd)
			}
		})
	}
}

// TestSponsorSplashRendersWhileLoading is the regression guard for the
// ghost-UI bug: the splash must be PAINTED during the first-fetch
// loading screen (stats == nil), not just after the dashboard loads —
// otherwise it's open in Update (absorbing keys) but invisible in View.
func TestSponsorSplashRendersWhileLoading(t *testing.T) {
	m := newSponsorModel(t) // loading == true, stats == nil
	out := ansi.Strip(m.View())
	if !strings.Contains(out, "Enjoying octoscope?") {
		t.Errorf("splash must render over the loading screen while open:\n%s", out)
	}

	// Contrast: with the splash off, the loading screen shows instead.
	t.Setenv("GITHUB_TOKEN", "test-token-not-used")
	client, _ := github.New("octocat", github.Options{})
	noSplash := NewModel(client, "test", Options{ShowSponsor: false})
	out = ansi.Strip(noSplash.View())
	if strings.Contains(out, "Enjoying octoscope?") {
		t.Errorf("splash should not render when show_sponsor is off:\n%s", out)
	}
	if !strings.Contains(out, "Loading") {
		t.Errorf("loading screen expected when splash is off:\n%s", out)
	}
}
