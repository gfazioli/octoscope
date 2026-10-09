package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/gfazioli/octoscope/internal/github"
)

// alertsMaxVisible is how many alerts the section lists before
// counting the rest as "+N more": the most severe, which is what the
// section is there to put in front of you.
const alertsMaxVisible = 5

// repoDetailAlerts renders the "Dependabot alerts" section of the Repos
// drill-in (#58), or "" when there is nothing to say. GitHub shows
// alerts to the people with write access or above, so on anyone else's
// repository the section is simply absent. What does earn a line:
// alerts switched off on a repository you can push to, a token short
// the permission, a failed request — and none open, which is a
// measured answer worth saying rather than an empty section.
func repoDetailAlerts(d *github.RepoDetail, width int) string {
	heading := subSectionTitleStyle.Render("Dependabot alerts")
	note := func(s string) string {
		return heading + "\n" + mutedStyle.Width(maxIntPositive(width-2)).Render(s)
	}
	switch d.AlertsAccess {
	case github.AccessOK:
		if d.Alerts == nil {
			return ""
		}
	case github.AccessDisabled:
		return besideOrUnder(heading, mutedStyle.Render("off for this repository"), width)
	case github.AccessTokenLacks:
		return note("GitHub refused this token the alerts. A fine-grained token needs the Dependabot alerts (read) permission and a classic one the repo or security_events scope; an organisation can also keep alerts to its admins.")
	case github.AccessFailed:
		return note("Couldn't load them: " + cleanErr(d.AlertsErr) + ". r reloads the view.")
	default:
		return ""
	}

	a := d.Alerts
	// "none open" only from a walk that reached the end: an incomplete
	// one that happened to count nothing has not shown there are none.
	if a.Total() == 0 && !a.Truncated {
		return besideOrUnder(heading, okStyle.Render("none open"), width)
	}
	lines := []string{besideOrUnder(heading, alertsSummary(a, width-2), width)}
	shown := a.Alerts
	if len(shown) > alertsMaxVisible {
		shown = shown[:alertsMaxVisible]
	}
	for _, al := range shown {
		lines = append(lines, alertRow(al, width))
	}
	if more := a.Total() - len(shown); more > 0 {
		lines = append(lines, mutedStyle.Render(fmt.Sprintf("    +%d more", more)))
	}
	return strings.Join(lines, "\n")
}

// alertsSummary is the heading's tally: the open total, then each
// severity that has any, most severe first. A walk that did not reach
// the end marks every count with a "+", since each is then a floor.
func alertsSummary(a *github.DependabotAlerts, width int) string {
	floor := ""
	if a.Truncated {
		floor = "+"
	}
	parts := []string{valueStyle.Render(fmt.Sprintf("%d%s open", a.Total(), floor))}
	for _, s := range []struct {
		n     int
		label string
	}{{a.Critical, "critical"}, {a.High, "high"}, {a.Medium, "medium"}, {a.Low, "low"}, {a.Other, "other"}} {
		if s.n > 0 {
			parts = append(parts, severityStyle(s.label).Render(fmt.Sprintf("%d%s %s", s.n, floor, s.label)))
		}
	}
	// Joined on one line when it fits, one count per line otherwise:
	// five severities at once do not fit a narrow terminal.
	if line := strings.Join(parts, mutedStyle.Render(" · ")); lipgloss.Width(line) <= width {
		return line
	}
	return strings.Join(parts, "\n")
}

// besideOrUnder puts what a heading says beside it when the line fits
// the width, and under it when it does not.
func besideOrUnder(heading, text string, width int) string {
	if line := heading + "   " + text; !strings.Contains(text, "\n") && lipgloss.Width(line) <= width-2 {
		return line
	}
	return heading + "\n" + text
}

// alertRow is one alert: severity, package, the advisory's summary
// linked to the alert on github.com, and whether a fixed version
// exists — the difference between "bump it" and "wait".
//
// Every column but the summary has a fixed width, so the fix column
// lines up row to row whatever the version string; and the summary is
// padded outside its link, so a terminal that underlines links does
// not underline the padding.
func alertRow(al github.DependabotAlert, width int) string {
	const sevW = 9
	pkgW, fixW := 24, 14
	// Narrow terminals narrow the package and fix columns rather than
	// drop either: "no fix yet" and "fix 5.0.11" both fit ten cells, and
	// whether a fix exists is half of what a row is for.
	if width < 80 {
		pkgW, fixW = 14, 10
	}
	// Below 60 the package column goes too: the advisory's summary
	// almost always names the package, and it needs the room more.
	if width < 60 {
		pkgW = 0
	}
	fix := "no fix yet"
	if al.FixedIn != "" {
		fix = "fix " + al.FixedIn
	}
	fix = truncate(fix, fixW)
	// Two of indent, the two fixed columns, and the fix column with
	// the gap before it when there is one; the summary takes the rest.
	sumW := width - 2 - 2 - sevW - pkgW - 2 - fixW
	if sumW < 1 {
		sumW = 1
	}
	summary := truncate(al.Summary, sumW)
	return "  " +
		severityStyle(al.Severity).Render(padRight(al.Severity, sevW)) +
		packageCell(al.Package, pkgW) +
		githubHyperlink(al.URL, summary) +
		strings.Repeat(" ", sumW-lipgloss.Width(summary)+2) + mutedStyle.Render(fix)
}

// packageCell is the package column, or nothing when it has no width.
func packageCell(pkg string, w int) string {
	if w == 0 {
		return ""
	}
	return padRight(truncate(pkg, w-2), w)
}

// severityStyle colours a severity through the theme's own slots, so a
// monochromatic theme keeps its palette; the word is always printed
// beside the colour, which is what carries the meaning there.
func severityStyle(sev string) lipgloss.Style {
	switch sev {
	case "critical", "high":
		return errorTextStyle
	case "medium":
		return warnStyle
	default:
		return mutedStyle
	}
}
