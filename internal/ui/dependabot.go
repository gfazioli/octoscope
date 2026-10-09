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
		return heading + "   " + mutedStyle.Render("off for this repository")
	case github.AccessTokenLacks:
		return note("You can push here, but your token can't read the alerts: a fine-grained token needs the Dependabot alerts (read) permission, a classic one the repo or security_events scope.")
	case github.AccessFailed:
		return note("Couldn't load them: " + cleanErr(d.AlertsErr) + ". r reloads the view.")
	default:
		return ""
	}

	a := d.Alerts
	// "none open" only from a walk that reached the end: an incomplete
	// one that happened to count nothing has not shown there are none.
	if a.Total() == 0 && !a.Truncated {
		return heading + "   " + okStyle.Render("none open")
	}
	// The tally sits beside the heading when there is room and under
	// it when there is not.
	head := heading + "   " + alertsSummary(a)
	if lipgloss.Width(head) > width-2 {
		head = heading + "\n" + alertsSummary(a)
	}
	lines := []string{head}
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
func alertsSummary(a *github.DependabotAlerts) string {
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
	return strings.Join(parts, mutedStyle.Render(" · "))
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
	// Narrow terminals give up the fix column first, then package
	// width, so the summary — the part that says what is wrong — keeps
	// room and no row runs past its width.
	if width < 80 {
		pkgW, fixW = 16, 0
	}
	fix := ""
	if fixW > 0 {
		fix = "no fix yet"
		if al.FixedIn != "" {
			fix = "fix " + al.FixedIn
		}
		fix = truncate(fix, fixW)
	}
	// Two of indent, the two fixed columns, and the fix column with
	// the gap before it when there is one; the summary takes the rest.
	sumW := width - 2 - 2 - sevW - pkgW
	if fixW > 0 {
		sumW -= 2 + fixW
	}
	if sumW < 1 {
		sumW = 1
	}
	summary := truncate(al.Summary, sumW)
	row := "  " +
		severityStyle(al.Severity).Render(padRight(al.Severity, sevW)) +
		padRight(truncate(al.Package, pkgW-2), pkgW) +
		githubHyperlink(al.URL, summary)
	if fixW > 0 {
		row += strings.Repeat(" ", sumW-lipgloss.Width(summary)+2) + mutedStyle.Render(fix)
	}
	return row
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
