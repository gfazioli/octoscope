package ui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
)

// festively applies the christmas theme for one test.
func festively(t *testing.T) {
	t.Helper()
	t.Cleanup(func() { _ = applyTheme(defaultTheme, "") })
	if err := applyTheme("christmas", ""); err != nil {
		t.Fatal(err)
	}
}

// TestSantaMascotsMatchThePlainOnes: the festive drawings are the plain
// ones redressed — same size, same eyes — so every layout sum that was
// done for the mascot holds for Santa, and both actually wear the trim.
func TestSantaMascotsMatchThePlainOnes(t *testing.T) {
	for _, tc := range []struct {
		name         string
		plain, santa mascotArt
	}{
		{"launch", mascotLaunch, mascotLaunchSanta},
		{"mini", mascotMini, mascotMiniSanta},
	} {
		if tc.santa.width() != tc.plain.width() || tc.santa.height() != tc.plain.height() {
			t.Errorf("%s: Santa is %d × %d pixels, the plain mascot %d × %d", tc.name,
				tc.santa.width(), tc.santa.height(), tc.plain.width(), tc.plain.height())
		}
		if tc.santa.eyeX != tc.plain.eyeX || tc.santa.eyeY != tc.plain.eyeY {
			t.Errorf("%s: Santa's eyes moved", tc.name)
		}
		trim := 0
		for _, row := range mascotCells(tc.santa, mascotPose{}, false) {
			for _, c := range row {
				if c.fg == roleTrim || c.bg == roleTrim {
					trim++
				}
			}
		}
		if trim == 0 {
			t.Errorf("%s: Santa has no trim", tc.name)
		}
		for _, row := range mascotCells(tc.plain, mascotPose{}, false) {
			for _, c := range row {
				if c.fg == roleTrim || c.bg == roleTrim {
					t.Fatalf("%s: the plain mascot wears trim", tc.name)
				}
			}
		}
	}
}

// TestOnlyAFestiveThemeDressesTheMascot goes through the theme: the
// christmas theme draws Santa, every other built-in the plain mascot.
func TestOnlyAFestiveThemeDressesTheMascot(t *testing.T) {
	t.Cleanup(func() { _ = applyTheme(defaultTheme, "") })
	for _, name := range themeOrder {
		if err := applyTheme(name, ""); err != nil {
			t.Fatal(err)
		}
		launch, mini := activeMascots()
		santa := launch.head[0] == mascotLaunchSanta.head[0] && mini.head[0] == mascotMiniSanta.head[0]
		plain := launch.head[0] == mascotLaunch.head[0] && mini.head[0] == mascotMini.head[0]
		if want := name == "christmas"; santa != want || plain == want {
			t.Errorf("theme %s: santa=%v plain=%v", name, santa, plain)
		}
	}
	if got := mascotStyle(roleTrim, roleNone).GetForeground(); got != colSnow {
		t.Errorf("the trim draws in %v, want snow %v", got, colSnow)
	}
}

// TestFestiveThemesAreNotMonochromatic: the festive header is colour by
// nature — five bulb colours, snow — so a theme cannot promise both it
// and a single tonal palette.
func TestFestiveThemesAreNotMonochromatic(t *testing.T) {
	festive := 0
	for _, name := range themeOrder {
		th := themes[name]
		if th.Festive {
			festive++
			if th.Monochromatic {
				t.Errorf("theme %s is both festive and monochromatic", name)
			}
		}
	}
	if festive == 0 {
		t.Error("no festive theme: this test checked nothing")
	}
}

// TestLaunchHeaderGreetsOnTheDay goes through the model's clock: on
// Christmas Day the christmas theme's launch screen says so, in place of
// the tagline, and the day after Boxing Day the tagline is back. The
// default theme never greets.
func TestLaunchHeaderGreetsOnTheDay(t *testing.T) {
	m := newSeasonalModel(t, noon(2026, time.December, 25), Options{SeasonalTheme: true})
	header := ansi.Strip(m.renderLaunchHeader(80, ""))
	if !strings.Contains(header, "Merry Christmas!") || strings.Contains(header, launchTagline) {
		t.Errorf("25 Dec, christmas theme: want the greeting in place of the tagline:\n%s", header)
	}
	u, _ := m.Update(clockTickMsg(noon(2026, time.December, 27)))
	m = u.(Model)
	header = ansi.Strip(m.renderLaunchHeader(80, ""))
	if strings.Contains(header, "Merry Christmas!") || !strings.Contains(header, launchTagline) {
		t.Errorf("27 Dec: want the tagline back:\n%s", header)
	}

	plain := newSeasonalModel(t, noon(2026, time.December, 25), Options{SeasonalTheme: false})
	if header := ansi.Strip(plain.renderLaunchHeader(80, "")); strings.Contains(header, "Merry Christmas!") {
		t.Errorf("25 Dec, default theme: no greeting expected:\n%s", header)
	}
}

// TestFestiveBannerKeepsTheBannerBox: the candy-cane frame is the plain
// banner's box, cell for cell — only its colours differ.
func TestFestiveBannerKeepsTheBannerBox(t *testing.T) {
	festively(t)
	for _, content := range []string{"⌖  octoscope", "⌖  octoscope  0.41.0", "x"} {
		plain, festive := bannerStyle.Render(content), festiveBanner(content)
		if ansi.Strip(festive) != ansi.Strip(plain) {
			t.Errorf("content %q: the festive box differs from the plain one:\n%s\n%s", content, ansi.Strip(festive), ansi.Strip(plain))
		}
	}
	if got := ansi.Strip(renderBanner("0.41.0")); got != ansi.Strip(bannerStyle.Render("⌖  octoscope  0.41.0")) {
		t.Errorf("renderBanner in a festive theme changed the banner's text or box:\n%s", got)
	}
}

// TestFestiveBannerIsStripedInColour looks at the colours themselves, on
// a terminal that has them: the christmas banner's frame carries both
// the accent and snow, and the default theme's banner no snow at all.
func TestFestiveBannerIsStripedInColour(t *testing.T) {
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(prev) })
	festively(t)

	snow := strings.TrimSuffix(lipgloss.NewStyle().Foreground(colSnow).Render("x"), "x\x1b[0m")
	accent := strings.TrimSuffix(lipgloss.NewStyle().Foreground(themes["christmas"].Accent).Render("x"), "x\x1b[0m")
	if snow == "" || accent == "" || snow == accent {
		t.Fatalf("precondition: the two colours must render apart, got %q and %q", snow, accent)
	}
	for i, edge := range strings.Split(renderBanner("0.41.0"), "\n") {
		if !strings.Contains(edge, snow+"─") && !strings.Contains(edge, snow+"│") {
			t.Errorf("banner row %d has no snow stripe: %q", i, edge)
		}
		if !strings.Contains(edge, accent+"─") && !strings.Contains(edge, accent+"│") && !strings.Contains(edge, accent+"╭") && !strings.Contains(edge, accent+"╰") {
			t.Errorf("banner row %d has no accent stripe: %q", i, edge)
		}
	}
	if err := applyTheme(defaultTheme, ""); err != nil {
		t.Fatal(err)
	}
	if plain := renderBanner("0.41.0"); strings.Contains(plain, snow) {
		t.Errorf("the default theme's banner carries snow: %q", plain)
	}
}

// TestFestiveGarlandFillsItsBox: every row is exactly as wide as asked,
// at every width and tick — a row one cell out would shear the header.
func TestFestiveGarlandFillsItsBox(t *testing.T) {
	festively(t)
	for w := 0; w <= 200; w++ {
		for tick := int64(0); tick < 40; tick++ {
			rows := festiveGarland(w, 3, tick)
			if len(rows) != 3 {
				t.Fatalf("w=%d: %d rows, want 3", w, len(rows))
			}
			for y, row := range rows {
				if got := lipgloss.Width(row); got != w {
					t.Fatalf("w=%d tick=%d row %d is %d cells wide", w, tick, y, got)
				}
			}
		}
	}
}

// TestFestiveLightsChase: a bulb every festiveBulbEvery cells on a wire,
// one in festiveChase of them out, and the dark spot one bulb further
// along on every tick, back where it started after festiveChase ticks.
func TestFestiveLightsChase(t *testing.T) {
	festively(t)
	const w = 64
	lights := func(tick int64) []rune { return []rune(ansi.Strip(festiveLights(w, tick))) }
	dark := func(tick int64) []int {
		var out []int
		for x, r := range lights(tick) {
			switch r {
			case '●', '○':
				if x%festiveBulbEvery != festiveBulbEvery/2 {
					t.Fatalf("tick %d: a bulb at cell %d, off the %d-cell beat", tick, x, festiveBulbEvery)
				}
				if r == '○' {
					out = append(out, x/festiveBulbEvery)
				}
			case '─':
				if x%festiveBulbEvery == festiveBulbEvery/2 {
					t.Fatalf("tick %d: no bulb at cell %d", tick, x)
				}
			default:
				t.Fatalf("tick %d: %q on the string of lights", tick, r)
			}
		}
		return out
	}
	const tick = 1_800_000_000 // a real clock reading, not a small number
	first := dark(tick)
	if want := w / festiveBulbEvery / festiveChase; len(first) != want {
		t.Fatalf("%d bulbs out, want %d of %d", len(first), want, w/festiveBulbEvery)
	}
	next := dark(tick + 1)
	for i := range first {
		if (next[i]-first[i]+festiveChase)%festiveChase != 1 {
			t.Errorf("the dark bulb at %d moved to %d, want one bulb along", first[i], next[i])
		}
	}
	if again := dark(tick + festiveChase); !equalInts(again, first) {
		t.Errorf("after %d ticks the dark bulbs are at %v, want %v again", festiveChase, again, first)
	}
	if festiveLights(w, tick) != festiveLights(w, tick) {
		t.Error("the same tick drew two different strings")
	}
}

func equalInts(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// TestFestiveSnowFalls: every flake on a row is on the row below, in the
// same column and the same shape, one tick later — and there is snow to
// say that about.
func TestFestiveSnowFalls(t *testing.T) {
	festively(t)
	const w, rows = 120, 4 // festiveSnowPeriod-1: the most rows the snow can fill
	flakes := 0
	// A real clock reading, and one from before 1970: a model built
	// without a clock must still draw snow rather than none.
	for _, start := range []int64{1_800_000_000, -62_135_596_800} {
		flakes += snowFallsFrom(t, w, rows, start)
	}
	if flakes < 100 {
		t.Errorf("only %d flakes fell: the snow is too thin to test, or to see", flakes)
	}
}

// snowFallsFrom checks two periods of snow starting at start, and
// returns how many flakes it followed down a row.
func snowFallsFrom(t *testing.T, w, rows int, start int64) int {
	t.Helper()
	flakes := 0
	for tick := start; tick < start+2*festiveSnowPeriod; tick++ {
		for row := 0; row < rows-1; row++ {
			above := []rune(ansi.Strip(festiveSnow(w, row, tick)))
			below := []rune(ansi.Strip(festiveSnow(w, row+1, tick+1)))
			for x, r := range above {
				if r == ' ' {
					continue
				}
				flakes++
				if below[x] != r {
					t.Fatalf("tick %d: the %q at row %d, column %d is %q a tick later one row down", tick, r, row, x, below[x])
				}
			}
		}
	}
	if flakes < 50 {
		t.Errorf("from tick %d only %d flakes fell", start, flakes)
	}
	return flakes
}

// TestFestiveHeaderKeepsTheHeaderBox renders the dashboard header in the
// christmas theme across widths: always the banner's height, nothing
// added to the banner past the edge, and the lights only where there is
// room for them — in which case they end exactly on it.
func TestFestiveHeaderKeepsTheHeaderBox(t *testing.T) {
	m := loadedModel(t)
	festively(t)
	want := lipgloss.Height(renderBanner(m.version))
	lit := 0
	for w := 20; w <= 200; w++ {
		header := m.renderHeader(w)
		if got := lipgloss.Height(header); got != want {
			t.Fatalf("width %d: the header is %d rows, the banner %d", w, got, want)
		}
		// The banner alone is wider than the narrowest terminals here, in
		// every theme and since before this one: what is held to
		// `available` is everything the header adds to it.
		banner := lipgloss.Width(renderBanner(m.version))
		for i, line := range strings.Split(header, "\n") {
			if got := lipgloss.Width(line); got > w && got > banner {
				t.Fatalf("width %d: row %d is %d cells wide", w, i, got)
			}
		}
		if strings.Contains(header, "●") || strings.Contains(header, "○") {
			lit++
			if got := lipgloss.Width(header); got != w {
				t.Fatalf("width %d: the lights stop at cell %d, short of the edge", w, got)
			}
		}
	}
	if lit < 100 {
		t.Errorf("the lights hung at only %d of 181 widths", lit)
	}
}

// TestFestiveHeaderMovesOnTheModelsClock: the lights and the snow are
// drawn for the model's clock, not for the wall's — the header a tick
// later is the garland of that second, and View stays a function of the
// model.
func TestFestiveHeaderMovesOnTheModelsClock(t *testing.T) {
	m := loadedModel(t)
	festively(t)
	const available = 116
	at := func(sec int64) string {
		u, _ := m.Update(clockTickMsg(time.Unix(sec, 0)))
		return ansi.Strip(u.(Model).renderHeader(available))
	}
	// Two instants far from the wall clock, a second apart.
	const then = 1_000_000_000
	first, second := at(then), at(then+1)
	if first == second {
		t.Fatal("the header did not move with the clock")
	}
	if again := at(then); again != first {
		t.Error("the same clock drew two different headers")
	}
	for sec, header := range map[int64]string{then: first, then + 1: second} {
		rows := strings.Split(header, "\n")
		w := available - lipgloss.Width(rows[1][:strings.LastIndex(rows[1], "│")+len("│")]) - len(headerMascotGap)
		for y, want := range festiveGarland(w, len(rows), sec) {
			if !strings.HasSuffix(rows[y], ansi.Strip(want)) {
				t.Errorf("clock %d, row %d: the header does not end in that second's garland:\n%q\n%q", sec, y, rows[y], ansi.Strip(want))
			}
		}
	}
}

// TestPlainThemesHaveNoFestiveHeader: nothing festive leaks into the
// other themes' dashboard — the header is the mascot and the banner, as
// it was.
func TestPlainThemesHaveNoFestiveHeader(t *testing.T) {
	m := loadedModel(t)
	t.Cleanup(func() { _ = applyTheme(defaultTheme, "") })
	for _, name := range themeOrder {
		if err := applyTheme(name, ""); err != nil {
			t.Fatal(err)
		}
		header := ansi.Strip(m.renderHeader(116))
		festive := strings.ContainsAny(header, "●○❅❆")
		if want := name == "christmas"; festive != want {
			t.Errorf("theme %s: festive header = %v, want %v:\n%s", name, festive, want, header)
		}
	}
}

// TestFestiveDashboardRenders goes through View, where the header meets
// the rest: in the christmas theme the dashboard carries the lights and
// Santa's mini mascot, and a narrow terminal still gets its banner.
func TestFestiveDashboardRenders(t *testing.T) {
	m := loadedModel(t) // 120 columns
	festively(t)
	// A bulb on its wire: the Overview's own ● and ○ icons are not that.
	lights := func(view string) bool {
		return strings.Contains(view, "─●─") || strings.Contains(view, "─○─")
	}
	out := ansi.Strip(m.View())
	if !lights(out) {
		t.Errorf("no lights on the festive dashboard:\n%s", out)
	}
	santa := renderMascot(mascotMiniSanta, mascotPose{}, false)
	if !strings.Contains(out, ansi.Strip(santa[0])+headerMascotGap) {
		t.Errorf("no Santa beside the banner:\n%s", out)
	}
	u, _ := m.Update(tea.WindowSizeMsg{Width: 34, Height: 40})
	narrow := ansi.Strip(u.(Model).View())
	if !strings.Contains(narrow, "⌖") || lights(narrow) {
		t.Errorf("a 34-column terminal should show the banner alone:\n%s", narrow)
	}
}

// TestFestiveGreeting pins the days with a tagline of their own.
func TestFestiveGreeting(t *testing.T) {
	for _, tc := range []struct {
		at   time.Time
		want string
	}{
		{noon(2026, time.December, 23), ""},
		{noon(2026, time.December, 24), "Merry Christmas!"},
		{noon(2026, time.December, 25), "Merry Christmas!"},
		{noon(2026, time.December, 26), "Merry Christmas!"},
		{noon(2026, time.December, 27), ""},
		{noon(2026, time.December, 31), "Happy New Year!"},
		{noon(2027, time.January, 1), "Happy New Year!"},
		{noon(2027, time.January, 2), ""},
		{noon(2026, time.November, 16), ""},
		{noon(2027, time.June, 25), ""},
	} {
		if got := festiveGreeting(tc.at); got != tc.want {
			t.Errorf("%s: greeting %q, want %q", tc.at.Format("2 Jan"), got, tc.want)
		}
	}
}
