package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/gfazioli/octoscope/internal/github"
)

// newLaunchModel is a model on the loading screen: first fetch in
// flight, no stats, splash off.
func newLaunchModel(t *testing.T) Model {
	t.Helper()
	t.Setenv("GITHUB_TOKEN", "test-token-not-used")
	_ = applyTheme("octoscope", "")
	client, err := github.New("octocat", github.Options{})
	if err != nil {
		t.Fatalf("github.New: %v", err)
	}
	m := NewModel(client, "test", Options{})
	if !m.loading || m.stats != nil {
		t.Fatal("precondition: a new model is loading with no stats")
	}
	return m
}

func allMascotPoses() []mascotPose {
	var out []mascotPose
	for _, look := range []int{-1, 0, 1} {
		for _, blink := range []bool{false, true} {
			for _, curl := range []bool{false, true} {
				out = append(out, mascotPose{look: look, blink: blink, curl: curl})
			}
		}
	}
	return out
}

// TestMascotEveryPoseIsWellFormed holds every pose, in both modes, to
// what a terminal can draw: 5 rows of 11 cells, no cell asking for two
// colours and an empty pixel at once, and — in silhouette — no lens
// colour at all, which is the promise a monochromatic theme makes.
func TestMascotEveryPoseIsWellFormed(t *testing.T) {
	for _, silhouette := range []bool{false, true} {
		for _, p := range allMascotPoses() {
			cells := mascotCells(p, silhouette)
			if len(cells) != mascotPxH/2 {
				t.Fatalf("%+v silhouette=%v: %d rows, want %d", p, silhouette, len(cells), mascotPxH/2)
			}
			for y, row := range cells {
				if len(row) != mascotPxW/2 {
					t.Errorf("%+v row %d: %d cells, want %d", p, y, len(row), mascotPxW/2)
				}
				for x, c := range row {
					if c.mixed {
						t.Errorf("%+v silhouette=%v cell (%d,%d) mixes body, lens and empty", p, silhouette, x, y)
					}
					if silhouette && (c.fg == roleLens || c.bg == roleLens) {
						t.Errorf("%+v cell (%d,%d) uses the lens colour in silhouette", p, x, y)
					}
				}
			}
			for i, l := range renderMascot(p, silhouette) {
				if w := lipgloss.Width(l); w != mascotPxW/2 {
					t.Errorf("%+v silhouette=%v line %d is %d cells wide, want %d", p, silhouette, i, w, mascotPxW/2)
				}
			}
		}
	}
}

// TestMascotEyesAndPeriscopeFollowTheLook pins the glyph each eye cell
// and the periscope take per pose, so the animation cannot quietly
// collapse into one frame.
func TestMascotEyesAndPeriscopeFollowTheLook(t *testing.T) {
	type want struct {
		r      rune
		fg, bg mascotRole
	}
	eye := func(p mascotPose, silhouette bool, col int) want {
		c := mascotCells(p, silhouette)[2][col]
		return want{c.r, c.fg, c.bg}
	}
	colourful := []struct {
		name string
		p    mascotPose
		eye  want
	}{
		{"ahead: the whole eye lit", mascotPose{look: 0}, want{'█', roleLens, roleNone}},
		{"left: the left half lit", mascotPose{look: -1}, want{'▌', roleLens, roleNone}},
		{"right: the right half lit", mascotPose{look: 1}, want{'▐', roleLens, roleNone}},
		{"blink: lid over the eye", mascotPose{blink: true}, want{'▀', roleBody, roleLens}},
	}
	for _, tc := range colourful {
		for _, col := range []int{3, 7} {
			if got := eye(tc.p, false, col); got != tc.eye {
				t.Errorf("%s, eye cell %d: got %q fg=%d bg=%d, want %q fg=%d bg=%d",
					tc.name, col, got.r, got.fg, got.bg, tc.eye.r, tc.eye.fg, tc.eye.bg)
			}
		}
	}

	// Silhouette: the eyes are holes, and a sideways look moves the
	// hole across the cell boundary.
	if got := eye(mascotPose{}, true, 3); got.r != ' ' {
		t.Errorf("silhouette ahead: eye cell = %q, want a hole", got.r)
	}
	if got := eye(mascotPose{blink: true}, true, 3); got.r != '▀' || got.fg != roleBody {
		t.Errorf("silhouette blink: eye cell = %q, want ▀ in body", got.r)
	}
	if l, r := eye(mascotPose{look: -1}, true, 2), eye(mascotPose{look: -1}, true, 3); l.r != '▌' || r.r != '▐' {
		t.Errorf("silhouette left: cells 2-3 = %q%q, want ▌▐", l.r, r.r)
	}
	if l, r := eye(mascotPose{look: 1}, true, 3), eye(mascotPose{look: 1}, true, 4); l.r != '▌' || r.r != '▐' {
		t.Errorf("silhouette right: cells 3-4 = %q%q, want ▌▐", l.r, r.r)
	}

	// The lens sits where the eyes look.
	lens := func(p mascotPose) int {
		for x, c := range mascotCells(p, false)[0] {
			if c.fg == roleLens || c.bg == roleLens {
				return x
			}
		}
		return -1
	}
	for _, tc := range []struct{ look, cell int }{{-1, 3}, {0, 5}, {1, 7}} {
		if got := lens(mascotPose{look: tc.look}); got != tc.cell {
			t.Errorf("look %d: lens on cell %d, want %d", tc.look, got, tc.cell)
		}
	}
}

// TestMascotChoreography: one loop looks both ways and blinks, the
// tentacles alternate on every step, and the loop closes.
func TestMascotChoreography(t *testing.T) {
	seen := map[int]bool{}
	blinked := false
	n := len(mascotChoreography)
	for s := 0; s < n; s++ {
		p := mascotPoseAt(s)
		seen[p.look] = true
		blinked = blinked || p.blink
		if next := mascotPoseAt(s + 1); next.curl == p.curl {
			t.Errorf("steps %d and %d curl the same way", s, s+1)
		}
	}
	if !seen[-1] || !seen[0] || !seen[1] || !blinked {
		t.Errorf("one loop should look left, ahead and right and blink; looks=%v blinked=%v", seen, blinked)
	}
	if n%2 != 0 {
		t.Errorf("choreography has %d steps: an odd loop would flip the tentacles' phase every lap", n)
	}
	if mascotPoseAt(n) != mascotPoseAt(0) {
		t.Errorf("step %d = %+v, want the loop to restart at %+v", n, mascotPoseAt(n), mascotPoseAt(0))
	}
}

// TestLaunchMascotAdvancesOnAcceptedTicks: the mascot's clock is the
// spinner's accepted ticks. A stale tick — a duplicate chain — must not
// advance it, and nothing advances it once the dashboard is up.
func TestLaunchMascotAdvancesOnAcceptedTicks(t *testing.T) {
	m := newLaunchModel(t)
	// Init's tick carries tag 0, which the spinner never rejects; from
	// the second tick on a tag is checked, so that is where a duplicate
	// chain can be told apart.
	u, _ := m.Update(m.spinner.Tick())
	m = u.(Model)
	second := m.spinner.Tick()
	u, cmd := m.Update(second)
	m = u.(Model)
	if cmd == nil || m.launchTicks != 2 {
		t.Fatalf("accepted ticks: launchTicks=%d next=%v, want 2 and a next tick", m.launchTicks, cmd != nil)
	}
	u, _ = m.Update(second) // its tag is spent: the spinner rejects it
	m = u.(Model)
	if m.launchTicks != 2 {
		t.Errorf("a stale tick advanced the mascot: launchTicks=%d, want 2", m.launchTicks)
	}

	per := int(mascotStep / m.spinner.Spinner.FPS)
	m.launchTicks = per*2 - 1
	if m.launchStep() != 1 {
		t.Errorf("launchStep at %d ticks = %d, want 1 (%d ticks a step)", m.launchTicks, m.launchStep(), per)
	}

	m.stats = &github.Stats{}
	before := m.launchTicks
	u, _ = m.Update(m.spinner.Tick())
	m = u.(Model)
	if m.launchTicks != before {
		t.Errorf("a tick after the first paint advanced the mascot: %d → %d", before, m.launchTicks)
	}
}

// TestLaunchHeaderFitsItsTerminal renders the loading screen across
// widths: the mascot and its text side by side where they fit, the
// one-line banner where they do not, and never a line past the edge.
// 50 and 48 straddle the switch: 11 cells of mascot, 3 of gap and the
// 31-cell tagline need 45, and outerStyle takes 4. Nothing narrower
// than 30: the one-line fallback needs 28 cells inside outerStyle with
// this test's version string (30 with a real one) and overflows below
// that, exactly as the loading screen did before the mascot existed.
func TestLaunchHeaderFitsItsTerminal(t *testing.T) {
	for _, tc := range []struct {
		width  int
		mascot bool
	}{
		{120, true}, {80, true}, {50, true}, {48, false}, {30, false},
	} {
		m := newLaunchModel(t)
		u, _ := m.Update(tea.WindowSizeMsg{Width: tc.width, Height: 30})
		m = u.(Model)
		out := ansi.Strip(m.View())
		for i, l := range strings.Split(out, "\n") {
			if w := lipgloss.Width(l); w > tc.width {
				t.Errorf("width %d: line %d is %d cells: %q", tc.width, i, w, l)
			}
		}
		if got := strings.Contains(out, launchTagline); got != tc.mascot {
			t.Errorf("width %d: mascot header shown = %v, want %v:\n%s", tc.width, got, tc.mascot, out)
		}
		if got := strings.Contains(out, "⌖"); got == tc.mascot {
			t.Errorf("width %d: fallback banner shown = %v, want %v", tc.width, got, !tc.mascot)
		}
		if !strings.Contains(out, "Loading…") {
			t.Errorf("width %d: the loading line is missing:\n%s", tc.width, out)
		}
	}
}

// TestLaunchHeaderIsASilhouetteInMonochrome goes through the theme, not
// the helper: a monochromatic theme — and NO_COLOR, which forces one —
// must get the one-colour mascot.
func TestLaunchHeaderIsASilhouetteInMonochrome(t *testing.T) {
	m := newLaunchModel(t)
	t.Cleanup(func() { _ = applyTheme("octoscope", "") })

	eyes := func(silhouette bool) string {
		return ansi.Strip(renderMascot(mascotPoseAt(0), silhouette)[2])
	}
	if eyes(true) == eyes(false) {
		t.Fatal("precondition: the two modes must differ on the eye row")
	}
	for _, tc := range []struct {
		theme      string
		silhouette bool
	}{{"octoscope", false}, {"monochrome", true}, {"phosphor", true}, {"stranger-things", false}} {
		if err := applyTheme(tc.theme, ""); err != nil {
			t.Fatal(err)
		}
		out := ansi.Strip(m.renderLaunchHeader(80, ""))
		if !strings.Contains(out, eyes(tc.silhouette)) {
			t.Errorf("theme %s: want the silhouette=%v eye row %q in:\n%s", tc.theme, tc.silhouette, eyes(tc.silhouette), out)
		}
	}
}

// TestMascotFollowsTheTheme: the mascot is drawn in the active theme's
// palette — body in Accent, eyes and lens in Value — for every built-in
// theme and for an accent override, so switching theme recolours it.
func TestMascotFollowsTheTheme(t *testing.T) {
	t.Cleanup(func() { _ = applyTheme("octoscope", "") })
	check := func(name string, accent, value lipgloss.Color) {
		t.Helper()
		if got := mascotBodyStyle.GetForeground(); got != accent {
			t.Errorf("%s: body %v, want the theme's Accent %v", name, got, accent)
		}
		if got := mascotLensStyle.GetForeground(); got != value {
			t.Errorf("%s: lens %v, want the theme's Value %v", name, got, value)
		}
		if fg, bg := mascotBodyOnLensStyle.GetForeground(), mascotBodyOnLensStyle.GetBackground(); fg != accent || bg != value {
			t.Errorf("%s: lid %v on %v, want %v on %v", name, fg, bg, accent, value)
		}
	}
	for _, name := range themeOrder {
		if err := applyTheme(name, ""); err != nil {
			t.Fatal(err)
		}
		check(name, themes[name].Accent, themes[name].Value)
	}
	if err := applyTheme("amber", "#123456"); err != nil {
		t.Fatal(err)
	}
	check("amber + accent override", lipgloss.Color("#123456"), themes["amber"].Value)
}

// TestDashboardKeepsItsBanner: the mascot ends where the dashboard
// starts. The dashboard's banner is part of the landing's screenshot
// geometry, so it must stay the one-line banner.
func TestDashboardKeepsItsBanner(t *testing.T) {
	m := loadedModel(t)
	out := ansi.Strip(m.View())
	if !strings.Contains(out, "⌖") {
		t.Errorf("dashboard lost its banner:\n%s", out)
	}
	if strings.Contains(out, launchTagline) {
		t.Errorf("the launch header leaked onto the dashboard:\n%s", out)
	}
}
