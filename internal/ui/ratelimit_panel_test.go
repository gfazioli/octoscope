package ui

import (
	"errors"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/gfazioli/octoscope/internal/github"
)

func sampleLimits() *github.RateLimits {
	reset := time.Now().Add(20 * time.Minute)
	return &github.RateLimits{
		Resources: []github.RateResource{
			{Name: "graphql", Limit: 5000, Used: 200, Remaining: 4800, Reset: reset},
			{Name: "core", Limit: 5000, Used: 4900, Remaining: 100, Reset: reset}, // 2% — error tier
			{Name: "search", Limit: 30, Used: 25, Remaining: 5, Reset: reset},     // 16.7% — warn tier
		},
		FetchedAt: time.Now(),
	}
}

// The panel's three-state machine: Open → loading, applyFetched →
// loaded (or error), and the key routing (esc close, r refetch,
// q quit).
func TestRateLimitPanelStateMachine(t *testing.T) {
	rl := RateLimitModel{}.Open()
	if !rl.IsOpen() || !rl.loading {
		t.Fatal("Open should yield an open, loading panel")
	}

	rl = rl.applyFetched(sampleLimits(), nil)
	if rl.loading || rl.err != nil || rl.limits == nil {
		t.Fatalf("applyFetched(success) state = %+v", rl)
	}

	// r → back to loading with a refetch cmd.
	rl2, cmd := rl.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("r")}, nil)
	if !rl2.loading || cmd == nil {
		t.Error("r should reset to loading and return a fetch cmd")
	}

	// esc closes.
	rl3, _ := rl.Update(tea.KeyMsg{Type: tea.KeyEsc}, nil)
	if rl3.IsOpen() {
		t.Error("esc should close the panel")
	}

	// q quits the whole app.
	_, quitCmd := rl.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")}, nil)
	if quitCmd == nil {
		t.Fatal("q should return a quit cmd")
	}
	if _, ok := quitCmd().(tea.QuitMsg); !ok {
		t.Error("q's cmd should produce tea.QuitMsg")
	}

	// Error state renders the retry hints.
	rlErr := RateLimitModel{}.Open().applyFetched(nil, errors.New("github rest 500: boom"))
	out := ansi.Strip(rlErr.View(100))
	if !strings.Contains(out, "Could not fetch rate limits") || !strings.Contains(out, "retry") {
		t.Errorf("error view should carry the failure + retry hint:\n%s", out)
	}
}

// The loaded view lists every resource with its counters, leads
// with graphql (the budget octoscope spends), and carries the
// "free endpoint" note so users know checking costs nothing.
func TestRateLimitPanelView(t *testing.T) {
	rl := RateLimitModel{}.Open().applyFetched(sampleLimits(), nil)
	out := ansi.Strip(rl.View(120))

	for _, want := range []string{
		"API rate limits",
		"graphql", "200/5000", "4800",
		"core", "4900/5000",
		"search", "25/30",
		"free endpoint",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("view should contain %q:\n%s", want, out)
		}
	}
	if gi, ci := strings.Index(out, "graphql"), strings.Index(out, "core"); gi > ci {
		t.Error("graphql should render before core")
	}
	// Loading state never shows the table.
	loading := ansi.Strip(RateLimitModel{}.Open().View(120))
	if strings.Contains(loading, "Resource") {
		t.Error("loading view should not render the table header")
	}
}

// Root-model wiring: `%` opens the panel and fires the fetch, the
// fetched msg lands only while the panel is open, and `%` typed
// into a list filter stays a literal character.
func TestRateLimitPanelRootWiring(t *testing.T) {
	pct := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("%")}

	t.Run("opens with fetch from a base tab", func(t *testing.T) {
		m := newTestModel(t, "", false, nil)
		updated, cmd := m.Update(pct)
		m2 := updated.(Model)
		if !m2.rateLimits.IsOpen() {
			t.Fatal("% should open the rate-limit panel")
		}
		if cmd == nil {
			t.Error("% should fire the /rate_limit fetch cmd")
		}
		if !strings.Contains(ansi.Strip(m2.View()), "API rate limits") {
			t.Error("View should render the panel while open")
		}
	})

	t.Run("fetched msg applies while open, dropped when closed", func(t *testing.T) {
		m := newTestModel(t, "", false, nil)
		m.rateLimits = m.rateLimits.Open()
		updated, _ := m.Update(rateLimitsFetchedMsg{limits: sampleLimits()})
		m2 := updated.(Model)
		if m2.rateLimits.loading || m2.rateLimits.limits == nil {
			t.Error("fetched msg should resolve the open panel's loading state")
		}

		closed := newTestModel(t, "", false, nil)
		updated, _ = closed.Update(rateLimitsFetchedMsg{limits: sampleLimits()})
		if updated.(Model).rateLimits.limits != nil {
			t.Error("a fetched msg landing after close must be dropped")
		}
	})

	t.Run("literal % while filtering", func(t *testing.T) {
		m := newTestModel(t, "", false, nil)
		m.activeTab = TabRepos
		m.repos.searchActive = true
		updated, _ := m.Update(pct)
		m2 := updated.(Model)
		if m2.rateLimits.IsOpen() {
			t.Error("% during filter input must not open the panel")
		}
		if m2.repos.query != "%" {
			t.Errorf("query = %q, want %%", m2.repos.query)
		}
	})

	t.Run("r inside the panel refetches instead of refreshing the dashboard", func(t *testing.T) {
		m := newTestModel(t, "", false, nil)
		m.rateLimits = m.rateLimits.Open().applyFetched(sampleLimits(), nil)
		updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("r")})
		m2 := updated.(Model)
		if m2.loading {
			t.Error("r inside the panel must not start a dashboard fetch")
		}
		if !m2.rateLimits.loading || cmd == nil {
			t.Error("r inside the panel should refetch the rate limits")
		}
	})
}
