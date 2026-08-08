package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/gfazioli/octoscope/internal/github"
)

// heatmapLevels are the 5 foreground colours used to shade contribution
// cells when the active theme is NOT monochromatic. Indexed 0 (no
// contributions) .. 4 (busiest bucket). The ramp walks the accent-pink
// hue from a dark surface into the full brand colour, with deliberately
// wide steps so adjacent buckets stay visually distinct (the previous
// 4-shade ramp had too little contrast between "medium" and "busy").
//
// On monochromatic themes (`monochrome` / `phosphor` / `amber`) the
// heatmap re-routes through monoHeatColor so the gradient stays
// inside the theme's six palette slots — see heatColor.
var heatmapLevels = [5]lipgloss.Color{
	lipgloss.Color("#21262D"), // empty — barely-visible surface grey
	lipgloss.Color("#5E1230"), // faint
	lipgloss.Color("#993366"), // medium
	lipgloss.Color("#CC3380"), // bright
	lipgloss.Color("#F00050"), // accent / busiest
}

// heatColor returns the appropriate cell foreground colour for
// the given bucket, honouring the theme's Monochromatic flag.
// Callers should never index heatmapLevels directly.
func heatColor(level int) lipgloss.Color {
	if IsMonochromatic() {
		return monoHeatColor(level)
	}
	if level < 0 {
		level = 0
	}
	if level >= len(heatmapLevels) {
		level = len(heatmapLevels) - 1
	}
	return heatmapLevels[level]
}

// heatmapCell is the glyph drawn in each day cell. A single monospace
// square with a trailing space gives a consistent 2-column footprint
// per day so neighbouring cells don't blur into a solid bar.
const heatmapCell = "■ "

// heatmapCellW is the column width occupied by one day cell — kept as
// a constant so layout maths (column positions, trimming, month
// labels) stays in one place.
const heatmapCellW = 2

// heatmapEmpty fills the 2 columns for a day that falls outside the
// contribution window (first/last weeks don't always line up with
// Sunday/Saturday boundaries).
const heatmapEmpty = "  "

// renderActivityTab draws the 52-week contribution heatmap plus a
// summary line (total, current streak, longest streak, busiest day).
// Falls back to a muted "no data" line when the user has no public
// contribution history — common for private-only accounts.
func renderActivityTab(s *github.Stats, available int) string {
	weeks := s.ContributionWeeks
	if len(weeks) == 0 {
		return mutedStyle.Render("(no public contribution data for this profile)")
	}

	// Row label column: "Mon", "Wed", "Fri" are shown — the GitHub
	// convention. Empty labels for the other weekdays keep the grid
	// rows aligned.
	rowLabels := [7]string{"   ", "Mon", "   ", "Wed", "   ", "Fri", "   "}
	const labelGap = "  " // between label column and grid

	labelW := 3 + len(labelGap) // "Mon" + "  "
	gridBudget := available - labelW
	if gridBudget < heatmapCellW*4 {
		gridBudget = heatmapCellW * 4
	}
	weeksThatFit := gridBudget / heatmapCellW

	// Trim from the left (oldest weeks) when the grid is wider than the
	// terminal — keeping the most recent activity visible is more
	// useful than the entire range.
	shown := weeks
	if len(weeks) > weeksThatFit {
		shown = weeks[len(weeks)-weeksThatFit:]
	}

	maxCount := 0
	for _, w := range shown {
		for _, d := range w {
			if d.Count > maxCount {
				maxCount = d.Count
			}
		}
	}

	// Build one string per weekday row, concatenating week cells.
	var rows [7]strings.Builder
	for wd := 0; wd < 7; wd++ {
		rows[wd].WriteString(mutedStyle.Render(rowLabels[wd]))
		rows[wd].WriteString(labelGap)
	}
	for _, w := range shown {
		byWeekday := [7]*github.ContributionDay{}
		for i := range w {
			d := &w[i]
			if d.Weekday >= 0 && d.Weekday < 7 {
				byWeekday[d.Weekday] = d
			}
		}
		for wd := 0; wd < 7; wd++ {
			d := byWeekday[wd]
			if d == nil {
				rows[wd].WriteString(heatmapEmpty)
				continue
			}
			rows[wd].WriteString(styleCell(d.Count, maxCount))
		}
	}

	var grid []string
	grid = append(grid, renderMonthLabels(shown, labelW))
	for wd := 0; wd < 7; wd++ {
		grid = append(grid, rows[wd].String())
	}

	legend := renderHeatmapLegend()
	summary := renderContributionSummary(weeks)

	return strings.Join(grid, "\n") + "\n\n" + legend + "\n\n" + summary
}

// renderMonthLabels produces the row of short month names above the
// grid. A month label is emitted at the first week whose first day
// falls in a new month — that's where the month "starts" visually in
// the heatmap — and positioned so its first letter sits directly
// above the week's column.
//
// Labels that would land too close to the previous one are skipped
// rather than overlapped: a "partial" month at the left/right edge
// of the 52-week window often has only 1-2 visible weeks, and
// forcing its label creates an "Apr May" squash that's unreadable.
// We prefer to drop the cramped edge label and let the next full
// month claim the space.
func renderMonthLabels(weeks [][]github.ContributionDay, labelW int) string {
	// Work in a fixed-width character buffer so we can place labels
	// by column index without fighting strings.Builder's append-only
	// nature.
	buf := make([]byte, labelW+len(weeks)*heatmapCellW)
	for i := range buf {
		buf[i] = ' '
	}

	const minLabelGap = 2 // spaces required between adjacent labels

	lastMonth := -1
	lastPlacedEnd := 0
	for i, w := range weeks {
		if len(w) == 0 {
			continue
		}
		m := int(w[0].Date.Month())
		if m == lastMonth || m == 0 {
			continue
		}
		lastMonth = m

		col := labelW + i*heatmapCellW
		label := w[0].Date.Format("Jan")
		if col+len(label) > len(buf) {
			continue
		}
		if col < lastPlacedEnd+minLabelGap {
			continue
		}
		// Skip months that span too few weeks in the visible grid to
		// fit their label cleanly. This drops the partial leading
		// month when its 1-2 weeks of April would collide with a full
		// May label, while still emitting every interior month.
		span := monthSpan(weeks, i, m)
		if span*heatmapCellW < len(label)+minLabelGap {
			continue
		}
		copy(buf[col:], label)
		lastPlacedEnd = col + len(label)
	}
	return mutedStyle.Render(string(buf))
}

// monthSpan counts how many consecutive weeks starting at index `from`
// stay in month `m`. Weeks with no days are treated as continuing the
// current month (they contribute columns to the grid but no data).
func monthSpan(weeks [][]github.ContributionDay, from, m int) int {
	span := 0
	for j := from; j < len(weeks); j++ {
		if len(weeks[j]) == 0 {
			span++
			continue
		}
		if int(weeks[j][0].Date.Month()) != m {
			break
		}
		span++
	}
	return span
}

// styleCell picks a bucket (0..4) for a day's contribution count and
// returns the cell glyph coloured accordingly. Buckets are 25%-wide
// slices of [0, maxCount] so the ramp adapts per-user: a light
// contributor and a prolific one both get meaningful contrast.
func styleCell(count, maxCount int) string {
	if count == 0 || maxCount == 0 {
		return lipgloss.NewStyle().Foreground(heatColor(0)).Render(heatmapCell)
	}
	// Clamp to 1..4 so any non-zero count renders with visible colour.
	bucket := 1 + (count-1)*4/maxIntOne(maxCount)
	if bucket > 4 {
		bucket = 4
	}
	if bucket < 1 {
		bucket = 1
	}
	return lipgloss.NewStyle().Foreground(heatColor(bucket)).Render(heatmapCell)
}

// maxIntOne returns n or 1 if n is zero, used to guard integer
// divisions in the bucket calculation.
func maxIntOne(n int) int {
	if n < 1 {
		return 1
	}
	return n
}

// renderHeatmapLegend draws the small "less ░ ▒ ▓ █ more" caption
// under the grid. Uses the heatmap palette so colour and shape read
// consistently with the cells above it.
func renderHeatmapLegend() string {
	var swatches []string
	for i := 0; i < 5; i++ {
		swatches = append(swatches,
			lipgloss.NewStyle().Foreground(heatColor(i)).Render(heatmapCell),
		)
	}
	return heatmapLegendStyle.Render("less ") +
		strings.Join(swatches, " ") +
		heatmapLegendStyle.Render(" more")
}

// renderContributionSummary draws the freeform stats line below the
// grid: total, current streak, longest streak, busiest day. All
// derived from the same flat day list so the numbers always match
// the grid the user just saw.
func renderContributionSummary(weeks [][]github.ContributionDay) string {
	days := flattenDays(weeks)
	total := 0
	busiestIdx := -1
	for i, d := range days {
		total += d.Count
		if busiestIdx == -1 || d.Count > days[busiestIdx].Count {
			busiestIdx = i
		}
	}

	current := currentStreak(days)
	longest := longestStreak(days)

	parts := []string{
		mutedStyle.Render("Total") + "   " + valueStyle.Render(fmt.Sprintf("%d", total)),
		mutedStyle.Render("Current streak") + "  " + valueStyle.Render(fmt.Sprintf("%d days", current)),
		mutedStyle.Render("Longest streak") + "  " + valueStyle.Render(fmt.Sprintf("%d days", longest)),
	}
	if busiestIdx >= 0 && days[busiestIdx].Count > 0 {
		d := days[busiestIdx]
		parts = append(parts,
			mutedStyle.Render("Busiest")+"  "+
				valueStyle.Render(fmt.Sprintf("%d", d.Count))+" on "+
				mutedStyle.Render(d.Date.Format("Mon Jan 2")),
		)
	}
	return strings.Join(parts, "   ")
}

// flattenDays collapses the weeks-of-days structure into a single
// chronological slice — convenient for streak calculations that don't
// care about week boundaries.
func flattenDays(weeks [][]github.ContributionDay) []github.ContributionDay {
	var out []github.ContributionDay
	for _, w := range weeks {
		out = append(out, w...)
	}
	return out
}

// currentStreak counts consecutive days ending at the most recent day
// with a contribution, stopping at the first zero-count day (walking
// backwards). The current day itself may be zero-count if the user
// hasn't pushed yet today — we still report the streak up to
// yesterday in that case rather than reporting zero.
func currentStreak(days []github.ContributionDay) int {
	if len(days) == 0 {
		return 0
	}
	// Skip a trailing zero if it corresponds to "today hasn't happened
	// yet" — i.e. the last day has count == 0. This preserves the
	// user's active streak instead of breaking it because they haven't
	// committed at midnight UTC yet.
	i := len(days) - 1
	if days[i].Count == 0 {
		i--
	}
	streak := 0
	for ; i >= 0; i-- {
		if days[i].Count == 0 {
			break
		}
		streak++
	}
	return streak
}

// longestStreak finds the longest consecutive run of days with a
// non-zero contribution count. O(n), single pass.
func longestStreak(days []github.ContributionDay) int {
	best, cur := 0, 0
	for _, d := range days {
		if d.Count > 0 {
			cur++
			if cur > best {
				best = cur
			}
		} else {
			cur = 0
		}
	}
	return best
}

// ActivitySub selects which half of the Activity tab is showing (#71).
// The heatmap and the feed are the same subject at two zoom levels —
// aggregate shape versus individual events — which is why the issue asked
// for a sub-tab here rather than an eighth entry in a tab row that is
// already six wide.
type ActivitySub int

const (
	ActivitySubHeatmap ActivitySub = iota
	ActivitySubFeed
)

var activitySubLabels = [...]string{
	ActivitySubHeatmap: "Heatmap",
	ActivitySubFeed:    "Feed",
}

// activitySubCount is the number of sub-views, derived so adding a label
// is the only edit needed.
var activitySubCount = ActivitySub(len(activitySubLabels))

// next cycles forward, wrapping. delta is +1 or -1.
func (a ActivitySub) next(delta int) ActivitySub {
	return (a + ActivitySub(delta) + activitySubCount) % activitySubCount
}

// activitySubBarHeight is the vertical cost of the sub-tab bar: the bar
// itself plus the blank line under it.
//
// It exists as a constant because two call sites subtract it — the
// renderer and syncActivityViewport — and a viewport whose height
// disagrees with the space it is drawn into scrolls to the wrong maximum
// without ever looking broken.
const activitySubBarHeight = 2

// activityBodyHeight is the room left for the sub-view once the bar has
// taken its share. Returns 0 (meaning "unknown, render inline") when the
// caller had no height either, preserving the pre-sub-tab behaviour on the
// first paint.
func activityBodyHeight(tabHeight int) int {
	if tabHeight <= 0 {
		return 0
	}
	h := tabHeight - activitySubBarHeight
	if h < 1 {
		h = 1
	}
	return h
}

// renderActivitySubTabs draws the sub-tab selector. Deliberately quieter
// than the main tab bar — no rule underneath, and the inactive entries stay
// muted — so the eye still reads the top-level row as the primary
// navigation.
func renderActivitySubTabs(active ActivitySub, available int) string {
	parts := make([]string, 0, len(activitySubLabels))
	for i, label := range activitySubLabels {
		if ActivitySub(i) == active {
			parts = append(parts, activeTabStyle.Render("▸ "+label))
		} else {
			parts = append(parts, inactiveTabStyle.Render(label))
		}
	}
	bar := strings.Join(parts, inactiveTabStyle.Render("  ·  "))
	// Both bindings are named. The README documents ←/→ *and* h/l, and a
	// hint that lists only half of what works is the kind of drift that
	// makes the in-app text stop being trusted.
	return bar + mutedStyle.Render("   ←/→ · h/l switch")
}
