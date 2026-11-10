package ui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/gfazioli/octoscope/internal/github"
)

// flat is rendered text with its styling gone and its wrapping undone, so
// a sentence can be looked for whatever width broke it.
func flat(s string) string { return strings.Join(strings.Fields(ansi.Strip(s)), " ") }

// seasonEdges returns the first and the last second of a seasonal theme's
// stretch, in local time, the way the app reads the date.
func seasonEdges(t *testing.T, theme string) (first, last time.Time) {
	t.Helper()
	s, ok := seasonOf(theme)
	if !ok {
		t.Fatalf("whatsNewInSeason names %q, which has no season", theme)
	}
	first = time.Date(2026, s.from.month, s.from.day, 0, 0, 0, 0, time.Local)
	last = time.Date(2027, s.to.month, s.to.day, 23, 59, 59, 0, time.Local)
	return first, last
}

// TestWhatsNewKeepsASeasonalThemeForItsFirstDay holds the surprise: a
// release that ships a seasonal theme says nothing about it in the What's
// new tab until the theme's first day, says it through its last, and goes
// back to what else the release did afterwards. Before that day the tab
// must not so much as name the theme: the plain entry is read by everyone
// who upgrades ahead of the season.
func TestWhatsNewKeepsASeasonalThemeForItsFirstDay(t *testing.T) {
	_ = applyTheme(defaultTheme, "")
	if len(whatsNewInSeason) == 0 {
		t.Fatal("whatsNewInSeason is empty: nothing here is being tested")
	}
	for version, byTheme := range whatsNewInSeason {
		plain, ok := whatsNew[version]
		if !ok {
			t.Fatalf("%s has an in-season entry and no plain one: out of season the tab would fall back to a link", version)
		}
		for theme, festive := range byTheme {
			if festive.headline == plain.headline {
				t.Fatalf("%s: the %s entry and the plain one share a headline, so nothing below tells them apart", version, theme)
			}
			first, last := seasonEdges(t, theme)
			for _, c := range []struct {
				when   string
				now    time.Time
				season bool
			}{
				{"the second before the first day", first.Add(-time.Second), false},
				{"the first second of the first day", first, true},
				{"the last second of the last day", last, true},
				{"the second after the last day", last.Add(time.Second), false},
			} {
				got := flat(renderWhatsNewTab(version, 76, c.now))
				want, other := plain, festive
				if c.season {
					want, other = festive, plain
				}
				if !strings.Contains(got, want.headline) || strings.Contains(got, other.headline) {
					t.Errorf("%s, %s (%s): want the headline %q and not %q:\n%s",
						version, c.when, c.now.Format("2 Jan 15:04:05"), want.headline, other.headline, got)
				}
				for _, it := range want.items {
					if !strings.Contains(got, it.title) {
						t.Errorf("%s, %s: the line %q is missing", version, c.when, it.title)
					}
				}
				if c.season {
					continue
				}
				// Out of season, nothing the season's entry alone says.
				said := map[string]bool{}
				for _, it := range plain.items {
					said[it.title] = true
				}
				for _, it := range festive.items {
					if !said[it.title] && strings.Contains(got, it.title) {
						t.Errorf("%s, %s: the tab already says %q", version, c.when, it.title)
					}
				}
				for _, word := range []string{theme, "season", "holiday"} {
					if strings.Contains(strings.ToLower(got), word) {
						t.Errorf("%s, %s: the tab says %q before the season starts:\n%s", version, c.when, word, got)
					}
				}
			}
		}
	}
}

// TestWhatsNewTurnsWithTheSeasonInARunningSession is the same turn in a
// session left open across midnight: the clock's tick is what moves it,
// and the tab's own viewport has to be given the new entry, not only the
// next frame, or it scrolls to the old entry's length. On the way out the
// entry gets shorter, so a tab left scrolled to the season's last lines
// goes back to the top instead of showing the plain entry's tail over
// blank rows.
func TestWhatsNewTurnsWithTheSeasonInARunningSession(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "test-token-not-used")
	t.Cleanup(func() { _ = applyTheme(defaultTheme, "") })
	client, err := github.New("octocat", github.Options{})
	if err != nil {
		t.Fatalf("github.New: %v", err)
	}
	for version, byTheme := range whatsNewInSeason {
		for theme, festive := range byTheme {
			first, _ := seasonEdges(t, theme)
			var m tea.Model = NewModel(client, version, Options{Now: first.Add(-time.Second)})
			m, _ = m.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
			read := func() (frame, viewport string) {
				mm := m.(Model)
				return flat(mm.renderWhatsNewScrolled(computeAvailable(mm.width), 30)), flat(mm.whatsNewVP.View())
			}
			plain := whatsNew[version]
			if frame, vp := read(); !strings.Contains(frame, plain.headline) || !strings.Contains(vp, plain.headline) {
				t.Fatalf("%s, the second before %s's first day: frame and viewport should read %q:\n%s\n%s", version, theme, plain.headline, frame, vp)
			}
			m, _ = m.Update(clockTickMsg(first))
			if frame, vp := read(); !strings.Contains(frame, festive.headline) || !strings.Contains(vp, festive.headline) {
				t.Errorf("%s, after the tick into %s's first day: frame and viewport should read %q:\n%s\n%s", version, theme, festive.headline, frame, vp)
			}

			// Out again, from the bottom of the season's entry, in a
			// window short enough for it to scroll.
			_, last := seasonEdges(t, theme)
			m = NewModel(client, version, Options{Now: last})
			m, _ = m.Update(tea.WindowSizeMsg{Width: 60, Height: 24})
			mm := m.(Model)
			mm.whatsNewVP.GotoBottom()
			if mm.whatsNewVP.YOffset == 0 {
				t.Fatalf("%s: the %s entry does not scroll in a 60x24 window, so this checks nothing", version, theme)
			}
			m, _ = mm.Update(clockTickMsg(last.Add(time.Second)))
			mm = m.(Model)
			if vp := flat(mm.whatsNewVP.View()); mm.whatsNewVP.YOffset != 0 || !strings.Contains(vp, plain.headline) {
				t.Errorf("%s, after the tick out of %s's last day: the viewport is at line %d and reads:\n%s", version, theme, mm.whatsNewVP.YOffset, vp)
			}
		}
	}
}
