package ui

import (
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/gfazioli/octoscope/internal/github"
)

// trafficDays is the window GitHub keeps traffic for, and so the
// number of cells in each traffic sparkline: one per day.
const trafficDays = 14

// repoDetailTraffic renders the "Traffic (14 days)" section of the
// Repos drill-in (#73), or "" when there is nothing to say. GitHub
// serves traffic only to people with push access, so for most
// repositories a viewer opens the section is simply absent — the same
// silence github.com keeps. Two outcomes do earn a line: a viewer who
// has push access but whose token lacks the permission (the one case
// the reader can fix), and a request that failed.
func repoDetailTraffic(d *github.RepoDetail, width int) string {
	heading := subSectionTitleStyle.Render("Traffic (14 days)")
	note := func(s string) string {
		return heading + "\n" + mutedStyle.Width(maxIntPositive(width-2)).Render(s)
	}
	switch d.TrafficAccess {
	case github.AccessOK:
		if d.Traffic == nil {
			return ""
		}
	case github.AccessTokenLacks:
		return note("You can push here, but your token can't read the traffic: a fine-grained token needs the Administration (read) permission, a classic one the repo scope.")
	case github.AccessFailed:
		return note("Couldn't load it: " + cleanErr(d.TrafficErr) + ". r reloads the view.")
	default:
		return ""
	}

	t := d.Traffic
	if t.Views == 0 && t.Clones == 0 {
		return note("No views or clones in the last 14 days.")
	}
	end := trafficEnd(t)
	row := func(label string, days []github.TrafficDay, total, uniques int) string {
		r := mutedStyle.Render(padRight(label, 8)) +
			styledTrafficSpark(trafficBuckets(days, end)) + "  " +
			valueStyle.Render(formatCompact(total))
		// The unique count is the part that gives way on a narrow
		// terminal: the total and the days are the section.
		if withUniques := r + mutedStyle.Render(" · "+formatCompact(uniques)+" unique"); lipgloss.Width(withUniques) <= width-2 {
			return withUniques
		}
		return r
	}
	return heading + "\n" +
		row("views", t.DailyViews, t.Views, t.ViewsUnique) + "\n" +
		row("clones", t.DailyClones, t.Clones, t.ClonesUnique)
}

// trafficEnd is the last day of the window: the latest day either
// series reports. Both series come from the same 14-day window, so
// taking the later of the two lines them up cell for cell even when
// one of them stops early.
func trafficEnd(t *github.Traffic) time.Time {
	var end time.Time
	for _, days := range [][]github.TrafficDay{t.DailyViews, t.DailyClones} {
		if n := len(days); n > 0 && days[n-1].Day.After(end) {
			end = days[n-1].Day
		}
	}
	return end
}

// trafficBuckets lays a series out on the trafficDays cells ending at
// end, one cell per UTC day, oldest first. A day the series does not
// mention stays zero, so a gap in GitHub's answer reads as a quiet day
// in its place rather than shifting the days after it.
func trafficBuckets(days []github.TrafficDay, end time.Time) []int {
	out := make([]int, trafficDays)
	endDay := end.UTC().Truncate(24 * time.Hour)
	for _, d := range days {
		age := int(endDay.Sub(d.Day.UTC().Truncate(24*time.Hour)) / (24 * time.Hour))
		idx := trafficDays - 1 - age
		if idx < 0 || idx >= trafficDays {
			continue
		}
		out[idx] += d.Count
	}
	return out
}

// styledTrafficSpark draws one traffic row: the star-history glyphs,
// scaled to the row's own busiest day. A row with no activity at all
// is a line of muted ticks, so the two rows keep the same width.
func styledTrafficSpark(buckets []int) string {
	if plain := sparklineString(buckets); plain != "" {
		return styledSparkline(plain)
	}
	return mutedStyle.Render(strings.Repeat(string(sparkBars[0]), len(buckets)))
}
