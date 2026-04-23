// Package ui contains the BubbleTea model, its update/view, and the
// Lipgloss styles that give the TUI its visual identity.
package ui

import "github.com/charmbracelet/lipgloss"

// Colour palette. Values are ANSI 256 codes (numeric strings) or hex
// strings — Lipgloss accepts both and picks the best match per terminal.
var (
	colAccent = lipgloss.Color("#E91E63") // magenta-pink — the "o" in octoscope
	colValue  = lipgloss.Color("#00D9FF") // cyan — the number that pops
	colOK     = lipgloss.Color("#2ECC71") // green — authenticated / success
	colWarn   = lipgloss.Color("#F1C40F") // yellow — unauthenticated / stale
	colError  = lipgloss.Color("#FF5555") // red — fetch failed
	colMuted  = lipgloss.Color("241")     // grey — labels, footers
)

var (
	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(colAccent).
			PaddingLeft(1)

	boldStyle = lipgloss.NewStyle().Bold(true)

	mutedStyle = lipgloss.NewStyle().Foreground(colMuted)

	errorStyle = lipgloss.NewStyle().Foreground(colError).Bold(true)

	okStyle   = lipgloss.NewStyle().Foreground(colOK)
	warnStyle = lipgloss.NewStyle().Foreground(colWarn)

	valueStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(colValue)

	// boxStyle is the card around each stat. Width is fixed so that a
	// row of boxes aligns cleanly via lipgloss.JoinHorizontal.
	boxStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colMuted).
			Padding(0, 2).
			Width(20)

	// sectionTitleStyle precedes each stat block — short bold accent
	// line, no box, keeps visual hierarchy light.
	sectionTitleStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(colAccent).
				MarginTop(1)

	// bannerStyle frames the "octoscope <version>" top banner. Rounded
	// border matches the stat cards; accent colour distinguishes it
	// from the muted stat-card borders.
	bannerStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colAccent).
			Foreground(colAccent).
			Bold(true).
			Padding(0, 2)

	// profileCardStyle wraps the user's profile info in a bordered box
	// so it reads as "the subject of this dashboard" rather than
	// just prose. Uses a neutral border so the accent colour stays
	// reserved for the app identity (banner + section titles).
	profileCardStyle = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(colMuted).
				Padding(0, 2).
				MarginTop(1)

	// footerBarStyle is the full-width footer separator + content.
	// Rendered with a top border only so it reads as a divider
	// rather than a heavy box.
	footerBarStyle = lipgloss.NewStyle().
			BorderStyle(lipgloss.NormalBorder()).
			BorderTop(true).
			BorderForeground(colMuted).
			PaddingTop(1).
			Width(0) // width is set at render time from terminal size

	// outerStyle is the outermost wrapper — left/right padding so
	// content doesn't touch the terminal edge, top padding for
	// breathing room above the banner.
	outerStyle = lipgloss.NewStyle().Padding(1, 2)

	// activeTabStyle highlights the selected tab in the tab bar:
	// bold accent with a single-character left marker to make the
	// selection unmistakable even on terminals that mute underline/bold.
	activeTabStyle = lipgloss.NewStyle().
			Foreground(colAccent).
			Bold(true)

	// inactiveTabStyle dims the non-selected tabs. Same baseline as
	// mutedStyle but kept separate so we can evolve the tab look
	// independently.
	inactiveTabStyle = lipgloss.NewStyle().Foreground(colMuted)

	// tabRuleStyle renders the thin horizontal rule below the tab
	// bar. Muted so it reads as a divider, not a heading.
	tabRuleStyle = lipgloss.NewStyle().Foreground(colMuted)

	// heatmapLegendStyle is the muted caption under the heatmap
	// ("less ░▒▓█ more"). Kept distinct so we can swap legend format
	// without touching the grid rendering.
	heatmapLegendStyle = lipgloss.NewStyle().Foreground(colMuted)
)
