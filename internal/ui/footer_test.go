package ui

import (
	"strings"
	"testing"
	"time"

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
// hotkey line. On a plain list tab the full list-level set is
// advertised; the moment an overlay (a drill-in or any modal) captures
// the keyboard, the footer must collapse to exactly the keys that fire
// there — never the tab-switch / public / settings / help hotkeys the
// overlay swallows. q only appears where q genuinely quits (rate-limit
// panel, action menu, drill-in); help / sponsor dismiss on any key and
// settings takes q as field input, so none of those claim "q quit".
func TestFooterBarHotkeys(t *testing.T) {
	tests := []struct {
		name       string
		open       func(m Model) Model
		wantHave   []string
		wantAbsent []string
	}{
		{
			name:     "list context advertises the full hotkey set",
			open:     func(m Model) Model { return m },
			wantHave: []string{"switch", "public", "settings", "help", "quit"},
		},
		{
			name: "drill-in collapses to esc/r/q",
			open: func(m Model) Model {
				m.repoDetail = m.repoDetail.Open(github.Repo{URL: "https://github.com/octocat/hello"}, StarModeDensity)
				return m
			},
			wantHave:   []string{"back", "refresh", "quit"},
			wantAbsent: []string{"switch", "public", "settings", "help"},
		},
		{
			name:       "rate-limit panel keeps esc/r/q",
			open:       func(m Model) Model { m.rateLimits = RateLimitModel{}.Open(); return m },
			wantHave:   []string{"back", "refresh", "quit"},
			wantAbsent: []string{"switch", "public", "settings", "help"},
		},
		{
			name:       "action menu keeps esc/q",
			open:       func(m Model) Model { m.actionMenu = ActionMenuModel{}.Open("Actions", nil); return m },
			wantHave:   []string{"back", "quit"},
			wantAbsent: []string{"switch", "public", "settings", "help", "refresh"},
		},
		{
			name: "settings advertises only esc cancel (q is field input, not quit)",
			open: func(m Model) Model {
				m.settings = SettingsModel{}.Open(30*time.Second, false, false, "octoscope")
				return m
			},
			wantHave:   []string{"cancel"},
			wantAbsent: []string{"switch", "public", "settings", "help", "quit"},
		},
		{
			name:       "help advertises only esc close (any key dismisses, q does not quit)",
			open:       func(m Model) Model { m.help = HelpModel{}.Open(); return m },
			wantHave:   []string{"close"},
			wantAbsent: []string{"switch", "public", "settings", "help", "quit"},
		},
		{
			name:       "sponsor splash advertises only esc dismiss",
			open:       func(m Model) Model { m.sponsor = SponsorModel{}.Open("https://github.com/sponsors/gfazioli"); return m },
			wantHave:   []string{"dismiss"},
			wantAbsent: []string{"switch", "public", "settings", "help", "quit"},
		},
		{
			name: "list filter input advertises enter/esc, not global hotkeys",
			open: func(m Model) Model {
				u, _ := m.Update(key("2")) // Repos tab
				m = u.(Model)
				u, _ = m.Update(key("/")) // enter filter input
				m = u.(Model)
				if !m.listInputMode() {
					t.Fatal("'/' on the Repos tab should enter filter input mode")
				}
				return m
			},
			wantHave:   []string{"confirm", "cancel"},
			wantAbsent: []string{"switch", "public", "settings", "help", "quit"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := tt.open(loadedModel(t))
			got := ansi.Strip(renderFooterBar(m))
			for _, want := range tt.wantHave {
				if !strings.Contains(got, want) {
					t.Errorf("footer missing %q:\n%s", want, got)
				}
			}
			for _, gone := range tt.wantAbsent {
				if strings.Contains(got, gone) {
					t.Errorf("footer should not advertise %q here:\n%s", gone, got)
				}
			}
		})
	}
}
