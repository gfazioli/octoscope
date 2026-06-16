package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/gfazioli/octoscope/internal/github"
)

func TestRenderWhatsNewTab(t *testing.T) {
	_ = applyTheme("octoscope", "")

	t.Run("bundled version shows highlights + sponsor", func(t *testing.T) {
		out := ansi.Strip(renderWhatsNewTab("0.16.0", 80))
		for _, want := range []string{
			"What's new in v0.16.0",
			"Sponsor splash at launch",
			"What's new", // the tab's own highlight
			"Full release notes",
			"https://github.com/gfazioli/octoscope/releases",
			"https://github.com/sponsors/gfazioli",
			"https://donate.stripe.com/fZu4gy4Tn3b1dgudGx0co00",
			"Buy me a coffee",
			"o sponsor",
			"b coffee",
			"c copy",
		} {
			if !strings.Contains(out, want) {
				t.Errorf("render missing %q:\n%s", want, out)
			}
		}
	})

	t.Run("unknown version falls back to the releases link, not stale notes", func(t *testing.T) {
		out := ansi.Strip(renderWhatsNewTab("9.9.9", 80))
		if !strings.Contains(out, "What's new in v9.9.9") {
			t.Errorf("heading should use the running version:\n%s", out)
		}
		if !strings.Contains(out, "github.com/gfazioli/octoscope/releases") {
			t.Errorf("fallback should link to releases:\n%s", out)
		}
		if strings.Contains(out, "Sponsor splash at launch") {
			t.Errorf("must NOT show another version's bundled notes:\n%s", out)
		}
		// The sponsor section still renders regardless.
		if !strings.Contains(out, "Support octoscope") {
			t.Errorf("sponsor section should render even on the fallback:\n%s", out)
		}
	})
}

func TestWhatsNewTabWiring(t *testing.T) {
	if tabCount != 6 {
		t.Fatalf("tabCount = %d, want 6", tabCount)
	}
	if tabLabels[TabWhatsNew] != "What's new" {
		t.Errorf("tabLabels[TabWhatsNew] = %q, want \"What's new\"", tabLabels[TabWhatsNew])
	}

	t.Setenv("GITHUB_TOKEN", "test-token-not-used")
	client, err := github.New("octocat", github.Options{})
	if err != nil {
		t.Fatalf("github.New: %v", err)
	}
	m := NewModel(client, "0.16.0", Options{}) // splash off (ShowSponsor false)

	// Key "6" jumps to the What's new tab.
	updated, _ := m.Update(key("6"))
	m = updated.(Model)
	if m.activeTab != TabWhatsNew {
		t.Fatalf("after '6', activeTab = %d, want TabWhatsNew (%d)", m.activeTab, TabWhatsNew)
	}

	// On the What's new tab, o / b / c act on the support links; other keys no-op.
	if _, cmd := m.Update(key("o")); cmd == nil {
		t.Error("'o' on What's new should return an open-URL cmd")
	}
	if _, cmd := m.Update(key("b")); cmd == nil {
		t.Error("'b' on What's new should return an open-URL cmd")
	}
	if _, cmd := m.Update(key("c")); cmd == nil {
		t.Error("'c' on What's new should return a copy-URL cmd")
	}
	if _, cmd := m.Update(key("x")); cmd != nil {
		t.Error("'x' on What's new should be a no-op (nil cmd)")
	}
}
