package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/gfazioli/octoscope/internal/github"
)

// loadedModel returns a plain model past the loading screen (window
// sized, stats present) so renderFooterBar takes its normal path.
func loadedModel(t *testing.T) Model {
	t.Helper()
	m := newPlainModel(t)
	u, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m = u.(Model)
	m.stats = &github.Stats{}
	m.loading = false
	return m
}

// TestFooterBarListContext pins the default (no drill-in) footer: the
// full list-level hotkey set is advertised.
func TestFooterBarListContext(t *testing.T) {
	m := loadedModel(t)

	got := ansi.Strip(renderFooterBar(m))
	for _, want := range []string{"switch", "public", "settings", "help", "quit"} {
		if !strings.Contains(got, want) {
			t.Errorf("list-context footer missing %q:\n%s", want, got)
		}
	}
}

// TestFooterBarDrillInContext pins the contextual collapse: while a
// drill-in is open the footer drops the list-level hotkeys that don't
// fire at that depth (tab-switch / public / settings / help) and
// advertises esc-back instead. Regression guard for the "never
// advertise a key that does nothing" contract in the drill-in views.
func TestFooterBarDrillInContext(t *testing.T) {
	m := loadedModel(t)
	m.repoDetail = m.repoDetail.Open(github.Repo{URL: "https://github.com/octocat/hello"}, StarModeDensity)
	if !m.drillInOpen() {
		t.Fatal("repoDetail.Open should report drillInOpen()")
	}

	got := ansi.Strip(renderFooterBar(m))

	// The keys that actually work at drill-in depth are present.
	for _, want := range []string{"back", "refresh", "quit"} {
		if !strings.Contains(got, want) {
			t.Errorf("drill-in footer missing %q:\n%s", want, got)
		}
	}
	// The keys the drill-in swallows are gone — advertising them would lie.
	for _, gone := range []string{"switch", "public", "settings", "help"} {
		if strings.Contains(got, gone) {
			t.Errorf("drill-in footer should not advertise %q (it does nothing there):\n%s", gone, got)
		}
	}
}
