package ui

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/gfazioli/octoscope/internal/config"
	"github.com/gfazioli/octoscope/internal/github"
	"github.com/muesli/termenv"
)

// noon is midday on a calendar date, in the machine's own zone: the zone
// the dashboard reads the date in.
func noon(year int, month time.Month, d int) time.Time {
	return time.Date(year, month, d, 12, 0, 0, 0, time.Local)
}

// TestChristmasSeason pins the stretch the christmas theme is in season:
// 16 November to 11 January, both days whole, every year.
func TestChristmasSeason(t *testing.T) {
	for _, tc := range []struct {
		name string
		at   time.Time
		want string
	}{
		{"the day before", noon(2026, time.November, 15), ""},
		{"the last second before", time.Date(2026, time.November, 15, 23, 59, 59, 0, time.Local), ""},
		{"the first second", time.Date(2026, time.November, 16, 0, 0, 0, 0, time.Local), "christmas"},
		{"the first day", noon(2026, time.November, 16), "christmas"},
		{"the end of November", noon(2026, time.November, 30), "christmas"},
		{"Christmas Day", noon(2026, time.December, 25), "christmas"},
		{"New Year's Eve", noon(2026, time.December, 31), "christmas"},
		{"New Year's Day", noon(2027, time.January, 1), "christmas"},
		{"the last day", noon(2027, time.January, 11), "christmas"},
		{"the last second", time.Date(2027, time.January, 11, 23, 59, 59, 0, time.Local), "christmas"},
		{"the day after", noon(2027, time.January, 12), ""},
		{"February", noon(2027, time.February, 16), ""},
		{"midsummer", noon(2027, time.July, 1), ""},
		{"the October before", noon(2026, time.October, 31), ""},
		{"the following year", noon(2027, time.November, 16), "christmas"},
		{"a January years on", noon(2031, time.January, 5), "christmas"},
	} {
		if got := seasonalTheme(tc.at); got != tc.want {
			t.Errorf("%s (%s): seasonalTheme = %q, want %q", tc.name, tc.at.Format("2006-01-02 15:04:05"), got, tc.want)
		}
	}
}

// TestSeasonsAreRealThemes: every season names a built-in theme, and its
// span reads the way the settings panel and --theme list print it.
func TestSeasonsAreRealThemes(t *testing.T) {
	for _, s := range seasons {
		if !IsValidTheme(s.theme) {
			t.Errorf("season %q is not a built-in theme", s.theme)
		}
		if s.theme == defaultTheme {
			t.Errorf("the default theme cannot be its own stand-in")
		}
	}
	s, ok := seasonOf("christmas")
	if !ok || s.span() != "16 Nov – 11 Jan" {
		t.Errorf("christmas season = %q (found %v), want 16 Nov – 11 Jan", s.span(), ok)
	}
	if _, ok := seasonOf("phosphor"); ok {
		t.Error("phosphor is not seasonal")
	}
	if got := seasonsSummary(); got != "christmas, 16 Nov – 11 Jan" {
		t.Errorf("seasonsSummary = %q", got)
	}
}

// newSeasonalModel is a model launched at now with opts, the theme
// restored afterwards so no other test inherits it.
func newSeasonalModel(t *testing.T, now time.Time, opts Options) Model {
	t.Helper()
	t.Setenv("GITHUB_TOKEN", "test-token-not-used")
	t.Cleanup(func() { _ = applyTheme(defaultTheme, "") })
	client, err := github.New("octocat", github.Options{})
	if err != nil {
		t.Fatalf("github.New: %v", err)
	}
	opts.Now = now
	return NewModel(client, "test", opts)
}

// TestSeasonalThemeStandsInForTheDefaultOnly walks every way a theme is
// chosen, in season and out, through NewModel — the path main.go takes —
// and checks both what is painted and that the stored choice is never
// the stand-in.
func TestSeasonalThemeStandsInForTheDefaultOnly(t *testing.T) {
	inSeason, outOfSeason := noon(2026, time.December, 1), noon(2027, time.March, 1)
	for _, tc := range []struct {
		name    string
		now     time.Time
		opts    Options
		painted string
		stored  string
	}{
		{"in season, default theme", inSeason, Options{SeasonalTheme: true}, "christmas", "octoscope"},
		{"in season, default theme named in the config", inSeason, Options{SeasonalTheme: true, Theme: "octoscope"}, "christmas", "octoscope"},
		{"out of season", outOfSeason, Options{SeasonalTheme: true}, "octoscope", "octoscope"},
		{"in season, seasonal_theme off", inSeason, Options{SeasonalTheme: false}, "octoscope", "octoscope"},
		{"in season, another theme chosen", inSeason, Options{SeasonalTheme: true, Theme: "phosphor"}, "phosphor", "phosphor"},
		{"in season, --theme octoscope", inSeason, Options{SeasonalTheme: true, Theme: "octoscope", ThemeFromFlag: true}, "octoscope", "octoscope"},
		{"in season, an accent of their own on the default", inSeason, Options{SeasonalTheme: true, AccentColor: "#00F0F0"}, "octoscope", "octoscope"},
		{"in season, NO_COLOR", inSeason, Options{SeasonalTheme: true, Theme: "monochrome", NoColor: true}, "monochrome", "monochrome"},
		{"out of season, christmas chosen", outOfSeason, Options{SeasonalTheme: false, Theme: "christmas"}, "christmas", "christmas"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := newSeasonalModel(t, tc.now, tc.opts)
			if currentTheme.Name != tc.painted {
				t.Errorf("painted %q, want %q", currentTheme.Name, tc.painted)
			}
			if m.theme != tc.stored {
				t.Errorf("stored theme %q, want %q", m.theme, tc.stored)
			}
		})
	}
}

// TestSeasonalThemeIgnoresNoColorEvenOnTheDefaultTheme: under NO_COLOR
// the settings panel can still put the default theme back for the
// session; the season must not then repaint it in colour on its own.
func TestSeasonalThemeIgnoresNoColorEvenOnTheDefaultTheme(t *testing.T) {
	m := newSeasonalModel(t, noon(2026, time.December, 1), Options{SeasonalTheme: true, Theme: "monochrome", NoColor: true})
	m.theme = defaultTheme
	if got := m.themeNow(); got != defaultTheme {
		t.Errorf("NO_COLOR session on the default theme draws %q, want %q", got, defaultTheme)
	}
}

// TestSeasonalThemeFollowsTheClock: a session left running changes theme
// on the day, both ways, on the one-second tick it already has — and a
// tick that changes nothing leaves a palette set by hand alone.
func TestSeasonalThemeFollowsTheClock(t *testing.T) {
	m := newSeasonalModel(t, noon(2026, time.November, 15), Options{SeasonalTheme: true})
	tick := func(at time.Time) {
		t.Helper()
		u, cmd := m.Update(clockTickMsg(at))
		m = u.(Model)
		if cmd == nil {
			t.Fatal("the clock tick did not schedule the next one")
		}
	}
	if currentTheme.Name != "octoscope" {
		t.Fatalf("15 Nov: painted %q, want octoscope", currentTheme.Name)
	}
	tick(time.Date(2026, time.November, 15, 23, 59, 59, 0, time.Local))
	if currentTheme.Name != "octoscope" {
		t.Errorf("a second before the season: painted %q, want octoscope", currentTheme.Name)
	}
	tick(time.Date(2026, time.November, 16, 0, 0, 0, 0, time.Local))
	if currentTheme.Name != "christmas" {
		t.Errorf("midnight on 16 Nov: painted %q, want christmas", currentTheme.Name)
	}
	if got := m.spinner.Style.GetForeground(); got != themes["christmas"].Accent {
		t.Errorf("the spinner kept %v, want christmas's accent %v", got, themes["christmas"].Accent)
	}
	tick(time.Date(2027, time.January, 11, 23, 59, 59, 0, time.Local))
	if currentTheme.Name != "christmas" {
		t.Errorf("the last second of the season: painted %q, want christmas", currentTheme.Name)
	}
	tick(time.Date(2027, time.January, 12, 0, 0, 0, 0, time.Local))
	if currentTheme.Name != "octoscope" {
		t.Errorf("12 Jan: painted %q, want octoscope", currentTheme.Name)
	}
	if m.theme != "octoscope" {
		t.Errorf("the stored theme became %q", m.theme)
	}

	// Nothing to change: the tick must not touch the styles at all.
	_ = applyTheme("amber", "")
	tick(noon(2027, time.January, 13))
	if currentTheme.Name != "amber" {
		t.Errorf("an idle tick repainted the theme: %q", currentTheme.Name)
	}
}

// TestSeasonalThemeNeverReachesTheConfigFile drives the settings panel's
// save in season: the file keeps the stored theme, not the stand-in, and
// carries the knob. Then the knob goes off through the same panel: the
// default theme is back at once and the file says false.
func TestSeasonalThemeNeverReachesTheConfigFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := config.Save(path, config.Defaults()); err != nil {
		t.Fatalf("seed Save: %v", err)
	}
	m := newSeasonalModel(t, noon(2026, time.December, 24), Options{SeasonalTheme: true, ConfigPath: path})
	m.stats = nil
	if currentTheme.Name != "christmas" {
		t.Fatalf("precondition: painted %q, want christmas", currentTheme.Name)
	}

	save := func(seasonal bool, theme string) config.Config {
		t.Helper()
		m.settings = m.settings.Open(m.interval, m.compact, m.client.PublicOnly(), theme, seasonal, m.accentColor, m.showSponsor, m.repos.commitCounts)
		_ = m.applySettingsAndClose()
		got, err := config.Load(path)
		if err != nil {
			t.Fatalf("reload: %v", err)
		}
		return got
	}

	got := save(true, m.theme)
	if got.Theme != "octoscope" || !got.SeasonalTheme {
		t.Errorf("saved in season: theme=%q seasonal_theme=%v, want octoscope and true", got.Theme, got.SeasonalTheme)
	}
	if currentTheme.Name != "christmas" {
		t.Errorf("saving repainted %q, want christmas still", currentTheme.Name)
	}

	got = save(false, m.theme)
	if got.Theme != "octoscope" || got.SeasonalTheme {
		t.Errorf("knob off: theme=%q seasonal_theme=%v, want octoscope and false", got.Theme, got.SeasonalTheme)
	}
	if currentTheme.Name != "octoscope" {
		t.Errorf("knob off: painted %q, want octoscope at once", currentTheme.Name)
	}

	got = save(true, m.theme)
	if currentTheme.Name != "christmas" || !got.SeasonalTheme {
		t.Errorf("knob back on: painted %q, seasonal_theme=%v", currentTheme.Name, got.SeasonalTheme)
	}

	// So does an accent of one's own on the default theme: the default
	// is back at once, wearing it, and comes off again with the accent.
	accent := func(value string) {
		t.Helper()
		m.settings = m.settings.Open(m.interval, m.compact, m.client.PublicOnly(), m.theme, true, value, m.showSponsor, m.repos.commitCounts)
		_ = m.applySettingsAndClose()
	}
	accent("#00F0F0")
	if currentTheme.Name != "octoscope" || colAccent != "#00F0F0" {
		t.Errorf("accent set in season: painted %q with accent %v, want octoscope in #00F0F0", currentTheme.Name, colAccent)
	}
	accent("")
	if currentTheme.Name != "christmas" || colAccent != themes["christmas"].Accent {
		t.Errorf("accent cleared in season: painted %q with accent %v, want christmas in its own", currentTheme.Name, colAccent)
	}

	// Choosing a theme by hand ends the stand-in, and that is what is saved.
	got = save(true, "amber")
	if currentTheme.Name != "amber" || got.Theme != "amber" {
		t.Errorf("amber chosen: painted %q, saved %q", currentTheme.Name, got.Theme)
	}
}

// TestSettingsPanelUnpinsAThemeFlag: --theme octoscope keeps the season
// out of the run, until the panel is used to say otherwise — by the
// seasonal switch, or by choosing a theme there.
func TestSettingsPanelUnpinsAThemeFlag(t *testing.T) {
	launch := func(seasonal bool) (*Model, func(theme string, seasonal bool)) {
		m := newSeasonalModel(t, noon(2026, time.December, 24), Options{SeasonalTheme: seasonal, Theme: "octoscope", ThemeFromFlag: true})
		m.stats = nil
		return &m, func(theme string, seasonal bool) {
			m.settings = m.settings.Open(m.interval, m.compact, m.client.PublicOnly(), theme, seasonal, m.accentColor, m.showSponsor, m.repos.commitCounts)
			_ = m.applySettingsAndClose()
		}
	}

	m, save := launch(false)
	save("octoscope", false) // nothing about the theme changed
	if !m.themePinned || currentTheme.Name != "octoscope" {
		t.Fatalf("a save that changed nothing: pinned=%v painted=%q", m.themePinned, currentTheme.Name)
	}
	save("octoscope", true)
	if m.themePinned || currentTheme.Name != "christmas" {
		t.Errorf("seasonal switched on in the panel: pinned=%v painted=%q, want christmas", m.themePinned, currentTheme.Name)
	}

	// The other half: the knob was on all along and the flag held the
	// season back. Choosing a theme in the panel is the newer word, so
	// coming back to the default there lets the season in.
	m, save = launch(true)
	if !m.themePinned || currentTheme.Name != "octoscope" {
		t.Fatalf("--theme octoscope in season: pinned=%v painted=%q", m.themePinned, currentTheme.Name)
	}
	save("amber", true)
	if m.themePinned || currentTheme.Name != "amber" {
		t.Errorf("amber chosen in the panel: pinned=%v painted=%q", m.themePinned, currentTheme.Name)
	}
	save("octoscope", true)
	if currentTheme.Name != "christmas" {
		t.Errorf("back to the default in the panel: painted %q, want christmas", currentTheme.Name)
	}
}

// TestAccentAloneRepaints: an accent override typed in the panel, with
// the theme left as it is, repaints — and so does taking it away. The
// skip in paintTheme must look at the accent as well as the name.
func TestAccentAloneRepaints(t *testing.T) {
	m := newSeasonalModel(t, noon(2027, time.March, 1), Options{Theme: "amber"})
	m.stats = nil
	accent := func(value string) {
		m.settings = m.settings.Open(m.interval, m.compact, m.client.PublicOnly(), m.theme, m.seasonal, value, m.showSponsor, m.repos.commitCounts)
		_ = m.applySettingsAndClose()
	}
	accent("#00F0F0")
	if currentTheme.Name != "amber" || colAccent != "#00F0F0" {
		t.Errorf("accent set: painted %q with accent %v, want amber in #00F0F0", currentTheme.Name, colAccent)
	}
	if got := m.spinner.Style.GetForeground(); got != lipgloss.Color("#00F0F0") {
		t.Errorf("the spinner kept %v", got)
	}
	accent("")
	if colAccent != themes["amber"].Accent {
		t.Errorf("accent cleared: %v, want amber's own %v", colAccent, themes["amber"].Accent)
	}
}

// TestSettingsPanelSeasonalRow: the row is there, says which theme and
// when, and space flips it only while it has the focus.
func TestSettingsPanelSeasonalRow(t *testing.T) {
	sm := SettingsModel{}.Open(30*time.Second, false, false, "octoscope", true, "", true, false)
	view := sm.View(120)
	for _, want := range []string{"Seasonal theme", "christmas, 16 Nov – 11 Jan", "8 built-ins"} {
		if !strings.Contains(view, want) {
			t.Errorf("settings panel is missing %q:\n%s", want, view)
		}
	}
	// The new row must not be what sets the panel's width: a longer
	// help line here made the panel need six more columns than before.
	widest, seasonalRow := 0, 0
	for _, line := range strings.Split(ansi.Strip(view), "\n") {
		if !strings.Contains(line, "│") {
			continue // the frame's top and bottom, as wide as the panel by definition
		}
		w := lipgloss.Width(strings.TrimRight(line, " │"))
		widest = max(widest, w)
		if strings.Contains(line, "christmas, 16 Nov") {
			seasonalRow = w
		}
	}
	if seasonalRow == 0 || seasonalRow >= widest {
		t.Errorf("the seasonal row's help line is %d cells, the panel's widest line %d: it widens the panel", seasonalRow, widest)
	}
	space := key(" ")
	sm.focus = fieldTheme
	sm, _ = sm.Update(space)
	if !sm.Seasonal() {
		t.Error("space on the Theme row flipped the seasonal toggle")
	}
	sm.focus = fieldSeasonal
	sm, _ = sm.Update(space)
	if sm.Seasonal() {
		t.Error("space on the Seasonal theme row did not switch it off")
	}
	sm, _ = sm.Update(space)
	if !sm.Seasonal() {
		t.Error("space on the Seasonal theme row did not switch it back on")
	}
}

// TestThemeListTagsTheSeasonalTheme: --theme list says which palette is
// seasonal and when, with colour and without.
func TestThemeListTagsTheSeasonalTheme(t *testing.T) {
	for _, noColor := range []bool{false, true} {
		var b strings.Builder
		PrintThemeList(&b, noColor)
		found := false
		for _, line := range strings.Split(b.String(), "\n") {
			seasonal := strings.Contains(line, "seasonal, 16 Nov – 11 Jan")
			if strings.Contains(line, "christmas") {
				found = true
				if !seasonal {
					t.Errorf("noColor=%v: christmas is not tagged seasonal: %q", noColor, line)
				}
			} else if seasonal {
				t.Errorf("noColor=%v: a non-seasonal theme is tagged seasonal: %q", noColor, line)
			}
		}
		if !found {
			t.Errorf("noColor=%v: christmas is missing from the list:\n%s", noColor, b.String())
		}
	}
}

// TestSeasonalRepaintReachesCachedDetailBodies: the PR and issue
// drill-ins cache their rendered body by width alone, and until the
// seasonal theme nothing could change the palette while one was open.
// The clock can: a drill-in left open across midnight on the first or
// the last day must not keep its title in yesterday's colours under a
// header already in today's.
func TestSeasonalRepaintReachesCachedDetailBodies(t *testing.T) {
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(prev) })

	// What a TrueColor terminal is sent for a colour's foreground, as the
	// parameters alone ("38;2;233;30;99"): a bold title carries them
	// inside a longer sequence, which a whole escape would not match.
	fg := func(c lipgloss.Color) string {
		esc := strings.TrimSuffix(lipgloss.NewStyle().Foreground(c).Render("x"), "x\x1b[0m")
		return strings.TrimSuffix(strings.TrimPrefix(esc, "\x1b["), "m")
	}
	accent := func(t *Theme) lipgloss.Color { return t.Accent }
	muted := func(t *Theme) lipgloss.Color { return t.Muted }

	for _, tc := range []struct {
		name   string
		colour func(*Theme) lipgloss.Color // a slot the surface's body draws in
		open   func(m *Model)
		cached func(m Model) string
	}{
		{"PR drill-in", accent, func(m *Model) {
			pd := PRDetailModel{}.Open(github.PullRequest{Number: 7, URL: "https://github.com/octocat/hello/pull/7"})
			m.prDetail = pd.applyFetched(&github.PRDetail{Number: 7, Title: "A title", State: "OPEN"}, nil)
		}, func(m Model) string { return m.prDetail.bodyCache }},
		{"issue drill-in", accent, func(m *Model) {
			id := IssueDetailModel{}.Open(github.Issue{Number: 9, URL: "https://github.com/octocat/hello/issues/9"})
			m.issueDetail = id.applyFetched(&github.IssueDetail{Number: 9, Title: "A title", State: "OPEN", NameWithOwner: "octocat/hello"}, nil)
		}, func(m Model) string { return m.issueDetail.bodyCache }},
		{"PR diff", muted, func(m *Model) {
			// A rename with no patch: the diff's whole body is one muted note.
			file := github.FileChange{Path: "a.go", Status: "renamed"}
			pd := PRDetailModel{}.Open(github.PullRequest{Number: 7, URL: "https://github.com/octocat/hello/pull/7"})
			pd = pd.applyFetched(&github.PRDetail{Number: 7, Title: "A title", State: "OPEN"}, nil)
			pd.files = PRFilesModel{}.Open([]github.FileChange{file}, "octocat", "hello", 7)
			pd.files.diff = PRDiffModel{}.Open(file, "octocat", "hello", 7, 116, 20)
			m.prDetail = pd
		}, func(m Model) string { return m.prDetail.files.diff.bodyCache }},
		{"gist drill-in", muted, func(m *Model) {
			// An empty file: its body is the muted "(empty file)".
			gd := GistDetailModel{}.Open(github.Gist{Name: "hash1"})
			m.gistDetail = gd.applyFetched(&github.GistDetail{Name: "hash1", Files: []github.GistFileContent{{Name: "empty.txt"}}}, nil)
		}, func(m Model) string { return m.gistDetail.bodyCache }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			plain, festive := fg(tc.colour(themes[defaultTheme])), fg(tc.colour(themes["christmas"]))
			if plain == "" || plain == festive {
				t.Fatalf("precondition: the two colours must render apart, got %q and %q", plain, festive)
			}
			m := newSeasonalModel(t, noon(2026, time.November, 15), Options{SeasonalTheme: true})
			u, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
			m = u.(Model)
			m.stats, m.loading = &github.Stats{}, false
			tc.open(&m)
			// A scroll key is what fills the cache.
			u, _ = m.Update(key("down"))
			m = u.(Model)
			if !strings.Contains(tc.cached(m), plain) {
				t.Fatalf("precondition: the scroll key cached a body carrying the default palette, got %q", tc.cached(m))
			}
			if before := m.View(); !strings.Contains(before, plain) || strings.Contains(before, festive) {
				t.Fatalf("precondition: 15 Nov draws in the default palette only")
			}

			u, _ = m.Update(clockTickMsg(time.Date(2026, time.November, 16, 0, 0, 0, 0, time.Local)))
			m = u.(Model)
			after := m.View()
			if !strings.Contains(after, festive) {
				t.Fatal("precondition: 16 Nov draws in christmas's palette")
			}
			if strings.Contains(after, plain) {
				t.Errorf("a body rendered ahead of time kept the default palette after the season began")
			}
			// And it stays repainted once a key refills the cache.
			u, _ = m.Update(key("down"))
			if strings.Contains(u.(Model).View(), plain) {
				t.Errorf("the default palette is back after the next key")
			}
		})
	}
}
