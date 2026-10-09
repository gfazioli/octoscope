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
// alerts to the people who administer a repository, so on anyone
// else's the section is simply absent. What does earn a line: alerts
// switched off on a repository you administer, a token short the
// permission, a failed request — and none open, which is a measured
// answer worth saying rather than an empty section.
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
		return note("You administer this repository, but your token can't read its alerts: a fine-grained token needs the Dependabot alerts (read) permission, a classic one the repo or security_events scope.")
	case github.AccessFailed:
		return note("Couldn't load them: " + cleanErr(d.AlertsErr) + ". r reloads the view.")
	default:
		return ""
	}

	a := d.Alerts
	if a.Total() == 0 {
		return heading + "   " + okStyle.Render("none open")
	}
	lines := []string{heading + "   " + alertsSummary(a)}
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
// severity that has any, most severe first. A walk that stopped at its
// page cap says so with a "+", since the counts are then a floor.
func alertsSummary(a *github.DependabotAlerts) string {
	total := fmt.Sprintf("%d open", a.Total())
	if a.Truncated {
		total = fmt.Sprintf("%d+ open", a.Total())
	}
	parts := []string{valueStyle.Render(total)}
	for _, s := range []struct {
		n     int
		label string
	}{{a.Critical, "critical"}, {a.High, "high"}, {a.Medium, "medium"}, {a.Low, "low"}} {
		if s.n > 0 {
			parts = append(parts, severityStyle(s.label).Render(fmt.Sprintf("%d %s", s.n, s.label)))
		}
	}
	return strings.Join(parts, mutedStyle.Render(" · "))
}

// alertRow is one alert: severity, package, the advisory's summary
// linked to the alert on github.com, and whether a fixed version
// exists — the difference between "bump it" and "wait".
func alertRow(al github.DependabotAlert, width int) string {
	const sevW, pkgW = 9, 24
	fix := "no fix yet"
	if al.FixedIn != "" {
		fix = "fix " + al.FixedIn
	}
	fix = truncate(fix, 18)
	// Two of indent, the two fixed columns, two gaps of two, the fix
	// column and its gap; the summary takes what is left.
	sumW := width - 2 - sevW - pkgW - 4 - lipgloss.Width(fix) - 2
	if sumW < 12 {
		sumW = 12
	}
	return "  " +
		severityStyle(al.Severity).Render(padRight(al.Severity, sevW)) +
		padRight(truncate(al.Package, pkgW-2), pkgW) +
		githubHyperlink(al.URL, padRight(truncate(al.Summary, sumW), sumW)) + "  " +
		mutedStyle.Render(fix)
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
