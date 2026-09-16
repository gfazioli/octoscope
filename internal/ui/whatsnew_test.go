package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

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
	// 8 since v0.29.0: Gists took 6 and Inbox 7, and What's new moved to
	// the end each time — on purpose, since those are content tabs and
	// What's new is meta. The assertion is on the *last* index rather than
	// only the count, because a tab appended after What's new would keep
	// the count right and put the meta tab in the middle.
	if tabCount != 8 {
		t.Fatalf("tabCount = %d, want 8", tabCount)
	}
	if TabWhatsNew != Tab(tabCount-1) {
		t.Errorf("TabWhatsNew is %d of %d — it belongs last", TabWhatsNew, tabCount)
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

	// Key "8" jumps to the What's new tab.
	updated, _ := m.Update(key("8"))
	m = updated.(Model)
	if m.activeTab != TabWhatsNew {
		t.Fatalf("after '8', activeTab = %d, want TabWhatsNew (%d)", m.activeTab, TabWhatsNew)
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

// The What's new tab was the only long static surface without a
// viewport, and renderWhatsNewTab took a width and no height — so it
// could neither clip nor scroll. 0.30.0 shipped an entry that rendered
// 57 lines against a tab budget of roughly 24, and the body pushed the
// banner, profile card and tab bar off the top of the terminal with no
// way to bring them back.
func TestWhatsNewNeverPushesTheChromeOffScreen(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "not-used")
	_ = applyTheme("octoscope", "")
	c, err := github.New("octocat", github.Options{})
	if err != nil {
		t.Fatalf("github.New: %v", err)
	}

	for _, h := range []int{24, 30, 40, 60} {
		m := NewModel(c, "0.30.0", Options{})
		m.stats = &github.Stats{Login: "octocat", Name: "Test User"}
		m.loading = false
		m.activeTab = TabWhatsNew
		m.width, m.height = 120, h
		syncWhatsNewViewport(&m)

		out := ansi.Strip(m.View())
		if n := len(strings.Split(out, "\n")); n > h {
			t.Errorf("at height %d the view rendered %d lines — the chrome is pushed off the top", h, n)
		}
		// The three pieces that vanished, named individually so a
		// failure says which one.
		for _, want := range []string{"octoscope  0.30.0", "What's new", "q quit"} {
			if !strings.Contains(out, want) {
				t.Errorf("at height %d, %q is missing from the view", h, want)
			}
		}
	}
}

// A scroll the footer advertises has to actually happen, and one it does
// not advertise has to not be needed.
func TestWhatsNewScrollsAndSaysSo(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "not-used")
	_ = applyTheme("octoscope", "")
	c, err := github.New("octocat", github.Options{})
	if err != nil {
		t.Fatalf("github.New: %v", err)
	}
	m := NewModel(c, "0.30.0", Options{})
	m.stats = &github.Stats{Login: "octocat", Name: "Test User"}
	m.loading = false
	m.activeTab = TabWhatsNew
	m.width, m.height = 120, 30
	syncWhatsNewViewport(&m)

	if !strings.Contains(ansi.Strip(m.View()), "scroll") {
		t.Error("a body taller than the tab did not advertise scrolling")
	}
	first := ansi.Strip(m.View())

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = updated.(Model)
	if ansi.Strip(m.View()) == first {
		t.Error("down did not move the viewport")
	}

	// The support links still work — the viewport must not swallow them.
	for _, k := range []string{"o", "b", "c"} {
		mm := m
		_, cmd := mm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)})
		if cmd == nil {
			t.Errorf("%q stopped doing anything once the viewport took the keys", k)
		}
	}
}

// The instruction above the map asks for 3-5 lines an item, and 0.30.0's
// entry was written as paragraphs — 1981 characters against 647 for
// 0.26.0. The viewport makes that survivable rather than acceptable, so
// this is the guard the next entry gets measured against.
// Two different promises about title length, kept apart on purpose,
// because the first draft of this guard conflated them and a reviewer was
// right to say so.
//
// The renderer wraps an item's description to the pane and does NOT wrap
// its title (whatsnew.go: the title is written after a "• " prefix with no
// wrapping), so a title longer than the pane simply overhangs. At 50
// columns a 75-character title ran 25 past the edge while the body beneath
// it wrapped cleanly. TestWhatsNewEntriesStayShort cannot see that:
// overflowing is one line, and one line is cheap.
//
// This first test is a COPY BUDGET, not a fit guarantee. Nothing in the
// TUI declares a minimum terminal width — view.go only guards width <= 0 —
// so there is no width at which "it fits" can be asserted for every
// terminal. 60 is editorial: 59 cells is the longest title that has ever
// shipped, so anything past it is new territory and should be a decision
// rather than an accident.
//
// The measure is display cells of the line as RENDERED, "• " included, not
// runes of the string, via the repository's own cellWidth: a rune count
// is the wrong unit for terminal layout —
// CJK and emoji occupy two cells each, combining sequences fewer cells than
// runes — and it silently ignores the two cells the bullet always costs.
func TestWhatsNewTitlesStayWithinTheCopyBudget(t *testing.T) {
	const maxCells = 62 // 60 for the title, 2 for the "• " the renderer adds
	for v, e := range whatsNew {
		for _, it := range e.items {
			if n := cellWidth("• " + it.title); n > maxCells {
				t.Errorf("%s: the title renders %d cells, over the %d budget — "+
					"titles are not wrapped, so a long one overhangs a narrow pane: %q",
					v, n, maxCells, it.title)
			}
		}
	}
}

// And this one is the fit guarantee. Two widths, because they answer
// different questions and one of them is the regression that matters.
//
// The width handed to the renderer is the AVAILABLE width, not the
// terminal's: outerStyle pads two cells each side, so an 80-column
// terminal leaves 76 (computeAvailable). Passing 80 straight in — which
// the first version of this test did — quietly asserts against an
// 84-column terminal, and a 77-to-80-cell line sails through while
// overhanging the real thing. The padding is taken from the function that
// applies it rather than written as a number, so the two cannot drift.
//
// At 80 columns every rendered line fits, URLs included. Narrower, two
// lines still overhang and always will: the release-notes link and the
// sponsor link are written unwrapped on purpose so they stay
// copy-pasteable, which renderWhatsNewTab says where it writes them.
// Measured at a 50-column terminal: those two lines and nothing else.
// So the narrow pass asserts the same thing with the deliberate
// exceptions named — that is what keeps it a guard rather than a
// permanent known failure.
//
// The narrow pass is the one that would catch a title going back to being
// written unwrapped, which is the defect that started this.
//
// Every rendered line, not only titles, so a description the wrapper
// mishandles is covered too. And a version that is NOT in the map,
// because that path renders a different body — the "aren't bundled"
// fallback — which no other test here exercises.
func TestWhatsNewRendersInsideItsPane(t *testing.T) {
	_ = applyTheme("octoscope", "")

	versions := []string{"0.0.0-not-in-the-map"}
	for v := range whatsNew {
		versions = append(versions, v)
	}

	for _, term := range []int{80, 50} {
		available := computeAvailable(term)
		for _, v := range versions {
			for _, line := range strings.Split(ansi.Strip(renderWhatsNewTab(v, available)), "\n") {
				if term < 80 && strings.Contains(line, "https://") {
					continue // deliberately unwrapped, so it stays copy-pasteable
				}
				if n := cellWidth(line); n > available {
					t.Errorf("%s: a rendered line is %d cells, over the %d a %d-column "+
						"terminal leaves after padding: %q", v, n, available, term, line)
				}
			}
		}
	}
}

func TestWhatsNewEntriesStayShort(t *testing.T) {
	_ = applyTheme("octoscope", "")
	const maxLines = 48 // 0.29.0, the longest that shipped before the fix
	for v := range whatsNew {
		n := len(strings.Split(ansi.Strip(renderWhatsNewTab(v, 120)), "\n"))
		if n > maxLines {
			t.Errorf("the %s entry renders %d lines, over the %d-line budget — "+
				"keep it to 3-5 lines an item", v, n, maxLines)
		}
	}
}
