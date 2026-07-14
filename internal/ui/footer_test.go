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

// TestFooterBarHotkeys pins the context-sensitivity of the footer's
// hotkey line. On a list tab the full list-level set is advertised;
// inside a drill-in the footer collapses to the keys that actually
// fire at that depth (esc back · r refresh · q quit) and drops the
// tab-switch / public / settings / help hotkeys the drill-in swallows.
// Regression guard for the "never advertise a key that does nothing"
// contract the drill-in views already follow.
func TestFooterBarHotkeys(t *testing.T) {
	tests := []struct {
		name       string
		drillIn    bool
		wantHave   []string
		wantAbsent []string
	}{
		{
			name:       "list context advertises the full hotkey set",
			drillIn:    false,
			wantHave:   []string{"switch", "public", "settings", "help", "quit"},
			wantAbsent: nil,
		},
		{
			name:       "drill-in context collapses to working keys only",
			drillIn:    true,
			wantHave:   []string{"back", "refresh", "quit"},
			wantAbsent: []string{"switch", "public", "settings", "help"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := loadedModel(t)
			if tt.drillIn {
				m.repoDetail = m.repoDetail.Open(github.Repo{URL: "https://github.com/octocat/hello"}, StarModeDensity)
				if !m.drillInOpen() {
					t.Fatal("repoDetail.Open should report drillInOpen()")
				}
			}

			got := ansi.Strip(renderFooterBar(m))

			for _, want := range tt.wantHave {
				if !strings.Contains(got, want) {
					t.Errorf("footer missing %q:\n%s", want, got)
				}
			}
			for _, gone := range tt.wantAbsent {
				if strings.Contains(got, gone) {
					t.Errorf("footer should not advertise %q (it does nothing there):\n%s", gone, got)
				}
			}
		})
	}
}
