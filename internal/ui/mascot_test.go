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

// mascotArts is every drawing, named, for the tests that hold both to
// the same rules.
var mascotArts = []struct {
	name string
	art  mascotArt
	// lensCell is the cell of the top row the lens occupies, by look.
	lensCell map[int]int
}{
	{"launch", mascotLaunch, map[int]int{-1: 3, 0: 5, 1: 7}},
	{"mini", mascotMini, map[int]int{-1: 2, 0: 4, 1: 6}},
}

// TestMascotEveryPoseIsWellFormed holds every pose of both drawings, in
// both modes, to what a terminal can draw: whole cells, no cell asking
// for two colours and an empty pixel at once, and — in silhouette — no
// lens colour at all, which is the promise a monochromatic theme makes.
func TestMascotEveryPoseIsWellFormed(t *testing.T) {
	for _, a := range mascotArts {
		rows, cols := a.art.height()/2, a.art.width()/2
		if a.art.height()%2 != 0 || a.art.width()%2 != 0 {
			t.Fatalf("%s: %d × %d pixels do not make whole cells", a.name, a.art.width(), a.art.height())
		}
		for _, silhouette := range []bool{false, true} {
			for _, p := range allMascotPoses() {
				cells := mascotCells(a.art, p, silhouette)
				if len(cells) != rows {
					t.Fatalf("%s %+v silhouette=%v: %d rows, want %d", a.name, p, silhouette, len(cells), rows)
				}
				for y, row := range cells {
					if len(row) != cols {
						t.Errorf("%s %+v row %d: %d cells, want %d", a.name, p, y, len(row), cols)
					}
					for x, c := range row {
						if c.mixed {
							t.Errorf("%s %+v silhouette=%v cell (%d,%d) mixes body, lens and empty", a.name, p, silhouette, x, y)
						}
						if silhouette && (c.fg == roleLens || c.bg == roleLens) {
							t.Errorf("%s %+v cell (%d,%d) uses the lens colour in silhouette", a.name, p, x, y)
						}
					}
				}
				for i, l := range renderMascot(a.art, p, silhouette) {
					if w := lipgloss.Width(l); w != cols {
						t.Errorf("%s %+v silhouette=%v line %d is %d cells wide, want %d", a.name, p, silhouette, i, w, cols)
					}
				}
			}
		}
	}
}

// TestMascotEyesAndPeriscopeFollowTheLook pins the glyph each eye cell
// and the lens take per pose, for both drawings, so the animation
// cannot quietly collapse into one frame.
func TestMascotEyesAndPeriscopeFollowTheLook(t *testing.T) {
	type want struct {
		r      rune
		fg, bg mascotRole
	}
	for _, a := range mascotArts {
		row := a.art.eyeY / 2
		eye := func(p mascotPose, silhouette bool, col int) want {
			c := mascotCells(a.art, p, silhouette)[row][col]
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
		for _, x := range a.art.eyeX {
			col := x / 2
			for _, tc := range colourful {
				if got := eye(tc.p, false, col); got != tc.eye {
					t.Errorf("%s, %s, eye cell %d: got %q fg=%d bg=%d, want %q fg=%d bg=%d",
						a.name, tc.name, col, got.r, got.fg, got.bg, tc.eye.r, tc.eye.fg, tc.eye.bg)
				}
			}
			// Silhouette: the eyes are holes, and a sideways look moves the
			// hole across the cell boundary.
			if got := eye(mascotPose{}, true, col); got.r != ' ' {
				t.Errorf("%s silhouette ahead: eye cell %d = %q, want a hole", a.name, col, got.r)
			}
			if got := eye(mascotPose{blink: true}, true, col); got.r != '▀' || got.fg != roleBody {
				t.Errorf("%s silhouette blink: eye cell %d = %q, want ▀ in body", a.name, col, got.r)
			}
			if l, r := eye(mascotPose{look: -1}, true, col-1), eye(mascotPose{look: -1}, true, col); l.r != '▌' || r.r != '▐' {
				t.Errorf("%s silhouette left: cells %d-%d = %q%q, want ▌▐", a.name, col-1, col, l.r, r.r)
			}
			if l, r := eye(mascotPose{look: 1}, true, col), eye(mascotPose{look: 1}, true, col+1); l.r != '▌' || r.r != '▐' {
				t.Errorf("%s silhouette right: cells %d-%d = %q%q, want ▌▐", a.name, col, col+1, l.r, r.r)
			}
		}

		// The lens sits where the eyes look.
		for look, want := range a.lensCell {
			got := -1
			for x, c := range mascotCells(a.art, mascotPose{look: look}, false)[0] {
				if c.fg == roleLens || c.bg == roleLens {
					got = x
					break
				}
			}
			if got != want {
				t.Errorf("%s look %d: lens on cell %d, want %d", a.name, look, got, want)
			}
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

// TestMascotsAdvanceOnAcceptedTicks: the mascots' clock is the spinner's
// accepted ticks — during the first fetch and during every refresh after
// it. A stale tick, from a duplicate chain, must not advance it; and
// with nothing in flight both rest facing ahead, whatever the count.
func TestMascotsAdvanceOnAcceptedTicks(t *testing.T) {
	m := newLaunchModel(t)
	// Init's tick carries tag 0, which the spinner never rejects; from
	// the second tick on a tag is checked, so that is where a duplicate
	// chain can be told apart.
	u, _ := m.Update(m.spinner.Tick())
	m = u.(Model)
	second := m.spinner.Tick()
	u, cmd := m.Update(second)
	m = u.(Model)
	if cmd == nil || m.mascotTicks != 2 {
		t.Fatalf("accepted ticks: mascotTicks=%d next=%v, want 2 and a next tick", m.mascotTicks, cmd != nil)
	}
	u, _ = m.Update(second) // its tag is spent: the spinner rejects it
	m = u.(Model)
	if m.mascotTicks != 2 {
		t.Errorf("a stale tick advanced the mascot: mascotTicks=%d, want 2", m.mascotTicks)
	}

	per := int(mascotStep / m.spinner.Spinner.FPS)
	m.mascotTicks = per*2 + 1 // step 2: looking right
	if got := m.mascotPoseNow(); got.look != 1 {
		t.Errorf("pose at %d ticks = %+v, want step 2's look right (%d ticks a step)", m.mascotTicks, got, per)
	}

	// A refresh after the first paint: stats on screen, a fetch in flight.
	m.stats = &github.Stats{}
	before := m.mascotTicks
	u, _ = m.Update(m.spinner.Tick())
	m = u.(Model)
	if m.mascotTicks != before+1 {
		t.Errorf("a tick during a refresh did not advance the mascot: %d → %d", before, m.mascotTicks)
	}

	// Nothing in flight: rest, not wherever the last fetch left it.
	m.loading = false
	if got := m.mascotPoseNow(); got != (mascotPose{}) {
		t.Errorf("pose with nothing loading = %+v, want the rest pose", got)
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
		return ansi.Strip(renderMascot(mascotLaunch, mascotPoseAt(0), silhouette)[2])
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

// TestDashboardHeaderPairsTheMiniMascotWithTheBanner: on the dashboard
// the mini mascot stands beside the banner, where there is room, and
// the banner stands alone where there is not. The launch header never
// leaks onto the dashboard.
func TestDashboardHeaderPairsTheMiniMascotWithTheBanner(t *testing.T) {
	m := loadedModel(t) // 120 columns
	out := ansi.Strip(m.View())
	rest := renderMascot(mascotMini, mascotPose{}, false)
	banner := strings.Split(renderBanner(m.version), "\n")
	for i := range rest {
		pair := ansi.Strip(rest[i]) + headerMascotGap + ansi.Strip(banner[i])
		if !strings.Contains(out, pair) {
			t.Errorf("header row %d: want the mini mascot beside the banner, %q, in:\n%s", i, pair, out)
		}
	}
	if strings.Contains(out, launchTagline) {
		t.Errorf("the launch header leaked onto the dashboard:\n%s", out)
	}

	// 11 cells of mascot and gap beside the 24-cell banner need 35; a
	// 34-column terminal leaves 30 inside outerStyle.
	u, _ := m.Update(tea.WindowSizeMsg{Width: 34, Height: 40})
	narrow := ansi.Strip(u.(Model).View())
	if strings.Contains(narrow, ansi.Strip(rest[1])) {
		t.Errorf("a 34-column terminal still shows the mini mascot:\n%s", narrow)
	}
	if !strings.Contains(narrow, "⌖") {
		t.Errorf("a 34-column terminal lost the banner:\n%s", narrow)
	}
}

// TestDashboardHeaderKeepsTheBannerHeight: the mini mascot is exactly
// as tall as the banner's box, so the profile card, the tab bar and
// everything below stay where they were.
func TestDashboardHeaderKeepsTheBannerHeight(t *testing.T) {
	m := loadedModel(t)
	for _, loading := range []bool{false, true} {
		m.loading = loading
		for _, w := range []int{116, 30} {
			if got, want := lipgloss.Height(m.renderHeader(w)), lipgloss.Height(renderBanner(m.version)); got != want {
				t.Errorf("loading=%v width %d: header is %d rows, the banner %d", loading, w, got, want)
			}
		}
	}
}

// TestDashboardHeaderMascotRestsUnlessRefreshing: with nothing in flight
// the mini mascot faces ahead; during a refresh it follows the
// choreography, like the launch one.
func TestDashboardHeaderMascotRestsUnlessRefreshing(t *testing.T) {
	m := loadedModel(t)
	per := int(mascotStep / m.spinner.Spinner.FPS)
	m.mascotTicks = per*2 + 1 // step 2: looking right, were anything loading
	pose := func(p mascotPose) string {
		return ansi.Strip(strings.Join(renderMascot(mascotMini, p, false), "\n"))
	}
	header := func() string { return ansi.Strip(m.renderHeader(116)) }
	contains := func(h, art string) bool {
		for _, row := range strings.Split(art, "\n") {
			if !strings.Contains(h, row) {
				return false
			}
		}
		return true
	}
	m.loading = false
	if h := header(); !contains(h, pose(mascotPose{})) {
		t.Errorf("idle header should show the rest pose:\n%s", h)
	}
	m.loading = true
	if h := header(); !contains(h, pose(mascotPoseAt(2))) || contains(h, pose(mascotPose{})) {
		t.Errorf("refreshing header should show step 2's pose, not the rest pose:\n%s", h)
	}
}
