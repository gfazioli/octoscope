// Package ui contains the BubbleTea model, its update/view, and the
// Lipgloss styles that give the TUI its visual identity. Styles here
// are package-level vars, not consts, because applyTheme rebuilds
// them when the user switches theme at runtime. See themes.go for
// the palette definitions and applyTheme.
package ui

import "github.com/charmbracelet/lipgloss"

// Colour palette. Populated by applyTheme — never written outside
// the ui package. Reads from these are safe at any time after init()
// because themes.go's init() applies the default theme before any
// view function runs.
var (
	colAccent lipgloss.Color
	colValue  lipgloss.Color
	colOK     lipgloss.Color
	colWarn   lipgloss.Color
	colError  lipgloss.Color
	colMuted  lipgloss.Color
)

// Compound styles. These bake in colour values when constructed, so
// they have to be reconstructed every time the active theme changes
// — that's what rebuildStyles does. Code that needs a one-off style
// builds it inline (e.g. lipgloss.NewStyle().Foreground(colAccent))
// and picks up the live colour values on each call.
var (
	titleStyle           lipgloss.Style
	boldStyle            lipgloss.Style
	mutedStyle           lipgloss.Style
	errorStyle           lipgloss.Style
	okStyle              lipgloss.Style
	warnStyle            lipgloss.Style
	valueStyle           lipgloss.Style
	boxStyle             lipgloss.Style
	sectionTitleStyle    lipgloss.Style
	subSectionTitleStyle lipgloss.Style
	summaryBoxStyle      lipgloss.Style
	bannerStyle          lipgloss.Style
	profileCardStyle     lipgloss.Style
	footerBarStyle       lipgloss.Style
	outerStyle           lipgloss.Style
	activeTabStyle       lipgloss.Style
	inactiveTabStyle     lipgloss.Style
	tabRuleStyle         lipgloss.Style
	heatmapLegendStyle   lipgloss.Style
)

// rebuildStyles regenerates every compound style from the current
// palette. Called from applyTheme after the colour vars are updated.
// Keep this list in sync with the var block above — anything that
// references a colXxx must be rebuilt here, otherwise theme switches
// will leave it on the previous palette's value.
func rebuildStyles() {
	titleStyle = lipgloss.NewStyle().
		Bold(true).
		Foreground(colAccent).
		PaddingLeft(1)

	boldStyle = lipgloss.NewStyle().Bold(true)

	mutedStyle = lipgloss.NewStyle().Foreground(colMuted)

	errorStyle = lipgloss.NewStyle().Foreground(colError).Bold(true)

	okStyle = lipgloss.NewStyle().Foreground(colOK)
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

	// subSectionTitleStyle styles inline section titles that live
	// *inside* a section (Languages, Top repositories) so they read
	// as headers rather than muted prose. Same accent + bold as
	// sectionTitleStyle but without the top margin — they share the
	// vertical rhythm of the surrounding content.
	subSectionTitleStyle = lipgloss.NewStyle().
		Bold(true).
		Foreground(colAccent)

	// summaryBoxStyle wraps a single-line takeaway under a row of
	// stat cards. Same muted border as the cards so it reads as
	// belonging to the row above; centred content + matching width
	// signals "this is the takeaway" rather than another stat.
	// Padding mirrors boxStyle so lipgloss size math is consistent.
	summaryBoxStyle = lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(colMuted).
		Foreground(colMuted).
		Padding(0, 2).
		Align(lipgloss.Center)

	// bannerStyle frames the "⌖ octoscope <version>" top banner. Rounded
	// accent border matches the landing page's logo frame, and marks
	// the banner zone even when the crosshair glyph falls back to a
	// plain rendering on older terminals.
	bannerStyle = lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(colAccent).
		Padding(0, 2)

	// profileCardStyle wraps the user's profile info in a bordered box
	// so it reads as "the subject of this dashboard" rather than just
	// prose. Uses a neutral border so the accent colour stays reserved
	// for the app identity (banner + section titles).
	profileCardStyle = lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(colMuted).
		Padding(0, 2).
		MarginTop(1)

	// footerBarStyle is the full-width footer separator + content.
	// Top-border-only so it reads as a divider rather than a heavy
	// box. Width is set at render time from terminal size.
	footerBarStyle = lipgloss.NewStyle().
		BorderStyle(lipgloss.NormalBorder()).
		BorderTop(true).
		BorderForeground(colMuted).
		PaddingTop(1).
		Width(0)

	// outerStyle is the outermost wrapper — left/right padding so
	// content doesn't touch the terminal edge, top padding for
	// breathing room above the banner.
	outerStyle = lipgloss.NewStyle().Padding(1, 2)

	// activeTabStyle highlights the selected tab in the tab bar:
	// bold accent with a single-character left marker to make the
	// selection unmistakable even on terminals that mute underline.
	activeTabStyle = lipgloss.NewStyle().
		Foreground(colAccent).
		Bold(true)

	// inactiveTabStyle dims the non-selected tabs.
	inactiveTabStyle = lipgloss.NewStyle().Foreground(colMuted)

	// tabRuleStyle renders the thin horizontal rule below the tab
	// bar. Muted so it reads as a divider, not a heading.
	tabRuleStyle = lipgloss.NewStyle().Foreground(colMuted)

	// heatmapLegendStyle is the muted caption under the heatmap
	// ("less ░▒▓█ more"). Kept distinct so we can swap legend format
	// without touching the grid rendering.
	heatmapLegendStyle = lipgloss.NewStyle().Foreground(colMuted)
}
