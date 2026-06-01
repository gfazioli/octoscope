package ui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// sponsorURL is the maintainer's GitHub Sponsors page. Hardcoded on
// purpose — NOT derived from the viewed profile: the prompt funds
// octoscope itself, so it always points at the maintainer regardless
// of which account the dashboard is currently showing.
const sponsorURL = "https://github.com/sponsors/gfazioli"

// SponsorModel is the one-time first-run splash inviting the user to
// sponsor octoscope. It follows the modal idiom of SettingsModel /
// ActionMenuModel: the root model opens it, it absorbs keys while open,
// and it renders into the content area with the banner, tab bar and
// footer staying pinned.
//
// Interaction is deliberately tiny: `o` opens the Sponsors page in the
// browser, `c` copies the URL, and ANY other key dismisses. Dismissal
// is session-only — the splash reappears on the next launch by design,
// unless the user opted out (show_sponsor=false / --no-sponsor) or is
// in --public-only mode.
type SponsorModel struct {
	open bool
	url  string
}

// IsOpen reports whether the splash is currently visible. The root
// model routes keystrokes to it while open (same dispatch idiom as the
// settings panel / action menu).
func (s SponsorModel) IsOpen() bool { return s.open }

// Open returns a fresh open splash pointing at url.
func (s SponsorModel) Open(url string) SponsorModel {
	return SponsorModel{open: true, url: url}
}

// Close returns a dismissed splash (zero value).
func (s SponsorModel) Close() SponsorModel { return SponsorModel{} }

// View renders the centered modal. Mirrors ActionMenuModel.View: a
// rounded, accent-bordered box built inline so it tracks the live theme
// accent, horizontally centered within width. Returns "" when closed.
func (s SponsorModel) View(width int) string {
	if !s.open {
		return ""
	}

	lines := []string{
		boldStyle.Foreground(colAccent).Render("♥  Enjoying octoscope?"),
		"",
		mutedStyle.Render("If you find it useful, please consider"),
		mutedStyle.Render("sponsoring its development:"),
		"",
		valueStyle.Render(s.url),
		"",
		keyHints(
			"o", "open",
			"c", "copy",
			"any key", "dismiss",
		),
	}

	panel := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(colAccent).
		Padding(1, 3).
		Render(strings.Join(lines, "\n"))

	if width <= 0 {
		return panel
	}
	return lipgloss.PlaceHorizontal(width, lipgloss.Center, panel)
}
