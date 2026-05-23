package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/gfazioli/octoscope/internal/github"
)

// sparkBars are the unicode block glyphs used to draw the
// star-history sparkline, sorted from least to most intense.
// One bar per weekly bucket; the 0 slot is intentionally a
// "tick" rune rather than a blank space so empty weeks still
// register as part of the 52-week axis — a young repo with
// stars only in the last fortnight should still look like a
// 52-cell timeline ending in a peak, not a tiny bar floating
// halfway across the line.
var sparkBars = [8]rune{
	'·', // empty week — muted tick on the timeline axis
	'▁', '▂', '▃', '▄', '▅', '▆', '▇',
}

// starSparkBuckets is the number of weekly buckets in the
// rendered sparkline. 52 weeks = a full year, matching the
// FetchStarHistory window (`starHistoryWindow = 365d`).
const starSparkBuckets = 52

// renderStarSparkline produces the body of the "Star history
// (12mo)" section of the Repos drill-in: a 52-char sparkline
// summarising weekly star counts, plus a footer line with the
// total and last-star age. Returns "" when the repo had no
// stars in the window — caller hides the section in that case.
//
// The sparkline is intentionally narrow (52 cells) so it fits
// inside the detail-view viewport regardless of terminal width.
// Bigger renderings (with axis ticks, hover) belong on
// starchart.cc — we link there as a fallback.
func renderStarSparkline(stars []time.Time, truncated bool) string {
	if len(stars) == 0 {
		return ""
	}
	buckets := bucketStars(stars, time.Now(), starSparkBuckets)
	spark := styledSparkline(sparklineString(buckets))

	// Total inside the window + last-star recency, both useful
	// at-a-glance metrics under the bars.
	var lastAgo string
	if len(stars) > 0 {
		// stars is DESC ordered (newest first) from
		// FetchStarHistory, so stars[0] is the most recent.
		lastAgo = formatRelativeAgo(stars[0])
	}
	suffix := ""
	if truncated {
		suffix = mutedStyle.Render(" (cap reached)")
	}
	footer := fmt.Sprintf("+%d in last 12mo · last star %s", len(stars), lastAgo)
	return spark + "\n" + mutedStyle.Render(footer) + suffix
}

// bucketStars distributes star timestamps into `n` weekly
// buckets ending at `now`. Index 0 is the oldest week (≈12mo
// ago); index n-1 is the most recent week. Stars outside the
// window are silently dropped — the caller will already have
// capped them via starHistoryWindow.
func bucketStars(stars []time.Time, now time.Time, n int) []int {
	out := make([]int, n)
	if n <= 0 {
		return out
	}
	// Each bucket covers (now - (n-i) * week, now - (n-i-1) * week].
	const week = 7 * 24 * time.Hour
	for _, t := range stars {
		delta := now.Sub(t)
		if delta < 0 {
			delta = 0
		}
		idx := n - 1 - int(delta/week)
		if idx < 0 || idx >= n {
			continue
		}
		out[idx]++
	}
	return out
}

// sparklineString maps a slice of counts to the 8-glyph block
// scale, returning the plain glyph sequence (no ANSI styling).
// Counts are normalised against the slice's own maximum so a
// quiet repo's sparkline still has visible peaks (the maximum
// bar reads as ▇, not just one cell above empty). Empty weeks
// render as a `·` tick so the 52-week axis stays visually
// continuous even on young repos.
//
// Plain string by design — styling lives in styledSparkline so
// the test layer asserts on stable glyphs without depending on
// lipgloss colour-mode detection.
func sparklineString(buckets []int) string {
	max := 0
	for _, n := range buckets {
		if n > max {
			max = n
		}
	}
	if max == 0 {
		return ""
	}
	runes := make([]rune, len(buckets))
	for i, n := range buckets {
		if n <= 0 {
			runes[i] = sparkBars[0]
			continue
		}
		// Scale 1..max → 1..7 (we leave 0 for "empty"); use
		// integer arithmetic so the result is deterministic.
		idx := 1 + (n-1)*(len(sparkBars)-2)/maxIntPositive(max-1)
		if idx >= len(sparkBars) {
			idx = len(sparkBars) - 1
		}
		runes[i] = sparkBars[idx]
	}
	return string(runes)
}

// styledSparkline paints the plain glyph string from
// sparklineString: empty `·` ticks in muted, filled bars in
// accent+bold. Separate from sparklineString so the binning
// logic stays unit-testable without lipgloss colour-mode
// dependencies.
func styledSparkline(plain string) string {
	if plain == "" {
		return ""
	}
	var b strings.Builder
	emptyGlyph := mutedStyle.Render(string(sparkBars[0]))
	bar := boldStyle.Foreground(colAccent)
	for _, r := range plain {
		if r == sparkBars[0] {
			b.WriteString(emptyGlyph)
		} else {
			b.WriteString(bar.Render(string(r)))
		}
	}
	return b.String()
}

// maxIntPositive returns n if positive, 1 otherwise. Guards
// integer divisions in the sparkline scale when max == 1
// (every bucket has at most one star).
func maxIntPositive(n int) int {
	if n < 1 {
		return 1
	}
	return n
}

// repoDetailStarHistory is the section heading + sparkline body
// wrapper called from RepoDetailModel.computeBody. Returns "" so
// the caller can simply skip the section when the repo has no
// stars in the window.
func repoDetailStarHistory(d *github.RepoDetail) string {
	body := renderStarSparkline(d.StarHistory, d.StarHistoryTruncated)
	if body == "" {
		return ""
	}
	return subSectionTitleStyle.Render("Star history (12mo)") + "\n" + body
}
