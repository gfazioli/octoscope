package ui

import (
	"fmt"
	"time"

	"github.com/charmbracelet/lipgloss"
)

// A seasonal theme is a built-in palette that turns itself on for part
// of the year. It only ever stands in for the default look: someone who
// picked a palette — in the config, in the settings panel, with --theme,
// or through NO_COLOR — or gave the default one an accent of their own
// keeps it, and seasonal_theme = false turns the whole thing off. The
// stand-in is never written to the config file, so the default theme is
// back the day the season ends.

// defaultTheme is the palette octoscope starts in, and the only one a
// seasonal theme takes the place of.
const defaultTheme = "octoscope"

// yearDay is a calendar day with no year.
type yearDay struct {
	month time.Month
	day   int
}

func (d yearDay) String() string { return fmt.Sprintf("%d %s", d.day, d.month.String()[:3]) }

// ordinal orders the days of one year.
func (d yearDay) ordinal() int { return int(d.month)*100 + d.day }

// season is the stretch of every year a theme is in season: from and to
// are both inclusive, and to may fall in the year after from.
type season struct {
	theme    string
	from, to yearDay
}

// seasons lists the seasonal themes. The stretches must not overlap:
// seasonalTheme returns the first that matches.
var seasons = []season{
	{theme: "christmas", from: yearDay{time.November, 16}, to: yearDay{time.January, 11}},
}

// span is the season in words, e.g. "16 Nov – 11 Jan".
func (s season) span() string { return s.from.String() + " – " + s.to.String() }

// includes reports whether now's calendar date, read in now's own
// location, falls in the season.
func (s season) includes(now time.Time) bool {
	day := yearDay{now.Month(), now.Day()}.ordinal()
	from, to := s.from.ordinal(), s.to.ordinal()
	if from <= to {
		return from <= day && day <= to
	}
	return day >= from || day <= to
}

// seasonOf returns the season of a seasonal theme.
func seasonOf(theme string) (season, bool) {
	for _, s := range seasons {
		if s.theme == theme {
			return s, true
		}
	}
	return season{}, false
}

// seasonalTheme returns the theme in season at now, or "" when none is.
func seasonalTheme(now time.Time) string {
	for _, s := range seasons {
		if s.includes(now) {
			return s.theme
		}
	}
	return ""
}

// seasonsSummary describes every seasonal theme for the settings
// panel's help line, e.g. "christmas, 16 Nov – 11 Jan".
func seasonsSummary() string {
	out := ""
	for i, s := range seasons {
		if i > 0 {
			out += " · "
		}
		out += s.theme + ", " + s.span()
	}
	return out
}

// themeNow is the theme the dashboard draws in on the model's clock: the
// stored choice, unless that is the untouched default and a seasonal
// theme is in season. A run that named its theme with --theme, or that
// NO_COLOR stripped of colour, is left alone; so is a default theme
// with an accent override, which is a look someone made — and a
// seasonal palette with a stranger's accent is one nobody designed.
func (m Model) themeNow() string {
	if m.seasonal && !m.themePinned && !m.noColor && m.theme == defaultTheme && m.accentColor == "" {
		if s := seasonalTheme(m.clock); s != "" {
			return s
		}
	}
	return m.theme
}

// paintTheme applies themeNow, and re-foregrounds the spinner so it
// tracks the accent. It is a no-op while that theme and accent are
// already the ones on screen, which is what lets the one-second clock
// call it: a session left running across the first or the last day of a
// season changes with the date instead of at the next launch.
func (m *Model) paintTheme() {
	name := m.themeNow()
	if name == m.paintedTheme && m.accentColor == m.paintedAccent {
		return
	}
	_ = applyTheme(name, m.accentColor)
	m.paintedTheme, m.paintedAccent = name, m.accentColor
	m.spinner.Style = lipgloss.NewStyle().Foreground(colAccent)
}
