package ui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/gfazioli/octoscope/internal/github"
)

// behaviourModel returns a plain model past the loading screen with one
// repo in stats, so key-driven interactions have something to act on.
func behaviourModel(t *testing.T) Model {
	t.Helper()
	m := newPlainModel(t)
	u, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m = u.(Model)
	m.stats = &github.Stats{Repositories: []github.Repo{mkRepo("octocat", "proj", 10)}}
	m.loading = false
	return m
}

// TestTabNavigation asserts the behaviour render tests can't: the tab
// hotkeys actually move activeTab. Digits jump directly; tab / shift+tab
// cycle with wraparound.
func TestTabNavigation(t *testing.T) {
	m := behaviourModel(t)

	for _, tc := range []struct {
		key  string
		want Tab
	}{
		{"2", TabRepos},
		{"3", TabPRs},
		{"4", TabIssues},
		{"1", TabOverview},
	} {
		u, _ := m.Update(key(tc.key))
		m = u.(Model)
		if m.activeTab != tc.want {
			t.Errorf("after %q, activeTab = %d, want %d", tc.key, m.activeTab, tc.want)
		}
	}

	// tab advances, shift+tab retreats, both wrap.
	m.activeTab = TabWhatsNew // last tab
	u, _ := m.Update(key("tab"))
	m = u.(Model)
	if m.activeTab != TabOverview {
		t.Errorf("tab from the last tab should wrap to Overview, got %d", m.activeTab)
	}
	u, _ = m.Update(key("shift+tab"))
	m = u.(Model)
	if m.activeTab != TabWhatsNew {
		t.Errorf("shift+tab from Overview should wrap to the last tab, got %d", m.activeTab)
	}
}

// TestDrillInAbsorbsAndDismisses pins two invariants of the drill-in
// dispatch: while a detail is open esc closes it (and only it), and a
// tab-switch key is swallowed rather than navigating away underneath the
// open detail.
func TestDrillInAbsorbsAndDismisses(t *testing.T) {
	t.Run("esc closes the open detail", func(t *testing.T) {
		m := behaviourModel(t)
		m.activeTab = TabRepos
		m.repoDetail = m.repoDetail.Open(github.Repo{URL: "https://github.com/octocat/proj"}, StarModeDensity)

		u, _ := m.Update(key("esc"))
		m = u.(Model)
		if m.repoDetail.IsOpen() {
			t.Error("esc should close the open repo drill-in")
		}
	})

	t.Run("a tab-switch key is absorbed while a detail is open", func(t *testing.T) {
		m := behaviourModel(t)
		m.activeTab = TabRepos
		m.repoDetail = m.repoDetail.Open(github.Repo{URL: "https://github.com/octocat/proj"}, StarModeDensity)

		u, _ := m.Update(key("1")) // would jump to Overview if not absorbed
		m = u.(Model)
		if m.activeTab != TabRepos {
			t.Errorf("a tab key must not navigate under an open detail; activeTab = %d", m.activeTab)
		}
		if !m.repoDetail.IsOpen() {
			t.Error("the detail should stay open when a tab key is pressed")
		}
	})
}

// TestEnterOpensRepoDrillIn drives the full open gesture: Enter on a
// Repos row returns a command that yields the open message, and feeding
// it back opens the detail. Exercises the row cursor → detail wiring the
// render tests never touch.
func TestEnterOpensRepoDrillIn(t *testing.T) {
	m := behaviourModel(t)
	u, _ := m.Update(key("2")) // Repos tab
	m = u.(Model)

	_, cmd := m.Update(key("enter"))
	if cmd == nil {
		t.Fatal("Enter on a repo row should return a command")
	}
	msg := cmd()
	u, _ = m.Update(msg)
	m = u.(Model)
	if !m.repoDetail.IsOpen() {
		t.Errorf("Enter → its message should open the repo drill-in")
	}
}
