package ui

import "github.com/charmbracelet/lipgloss"

// This file collects the small palette-translation helpers the
// renderer reaches for when the active theme is monochromatic
// (see Theme.Monochromatic — `monochrome`, `phosphor`, `amber`).
// The contract is "no external semantic colour leaks in": the
// GitHub language palette, the CI rollup green/red/yellow and
// the Activity heatmap gradient all collapse into shades drawn
// from the six theme slots so the theme's promise is respected
// end-to-end.

// monoRankColor picks a colour for the i-th item in an n-item
// ordered list when the active theme is monochromatic. Walks
// the theme's six palette slots from "most prominent" (Value)
// to "quiet" (Muted) so the eye still picks up the leader of
// the list. Used by the language bar in the Overview tab.
//
// rank is 0-indexed; n is the total count. Returns colValue
// for n<=1 (degenerate case, only one item to render). For
// n>1, the slot index scales linearly across the six slots.
func monoRankColor(rank, n int) lipgloss.Color {
	slots := []lipgloss.Color{colValue, colAccent, colOK, colWarn, colError, colMuted}
	if n <= 1 {
		return slots[0]
	}
	idx := rank * (len(slots) - 1) / (n - 1)
	if idx < 0 {
		idx = 0
	}
	if idx >= len(slots) {
		idx = len(slots) - 1
	}
	return slots[idx]
}

// monoHeatColor returns the colour for one cell of the Activity
// heatmap when the theme is monochromatic. heatLevel runs 0-4
// (the same buckets the regular gradient uses), 0 = empty, 4 =
// busiest. Scales from Muted (dim) up through Accent (most
// prominent) so the heatmap still reads as a heatmap on a
// `monochrome` / `phosphor` / `amber` background.
func monoHeatColor(level int) lipgloss.Color {
	scale := []lipgloss.Color{colMuted, colError, colWarn, colOK, colAccent}
	if level < 0 {
		level = 0
	}
	if level >= len(scale) {
		level = len(scale) - 1
	}
	return scale[level]
}
