package ui

import (
	"github.com/charmbracelet/lipgloss"
	"github.com/gfazioli/octoscope/internal/github"
)

// computeAvailable returns the usable horizontal width for tab body
// content: terminal width minus outerStyle's horizontal padding (2 on
// each side). Falls back to 80 when the first WindowSizeMsg hasn't
// arrived yet, and clamps to 20 so a deliberately narrow window still
// gets a sane minimum.
func computeAvailable(width int) int {
	if width <= 0 {
		return 80
	}
	a := width - 4
	if a < 20 {
		a = 20
	}
	return a
}

// computeTabHeight returns the vertical room for tab body content,
// after subtracting the heights of the always-visible chrome (banner,
// profile card, tab bar, footer) and the outerStyle's vertical
// padding plus a 1-line gap. Returns 0 when m.height is unknown
// (signal: no clamp / let the terminal scroll fallback). Floors at 6
// so even on very short terminals we still hand a usable budget to
// list tabs that paginate.
func computeTabHeight(m Model) int {
	if m.height <= 0 {
		return 0
	}
	available := computeAvailable(m.width)
	s := m.stats
	if s != nil && m.client.PublicOnly() {
		s = s.Public()
	}
	topLines := lipgloss.Height(renderBanner(m.version)) + 1 // banner + \n
	if s != nil {
		topLines += lipgloss.Height(renderProfileCard(s, available, m.client.PublicOnly())) + 1
	}
	topLines += lipgloss.Height(renderTabBar(m.activeTab, available)) + 1
	footerLines := lipgloss.Height(renderFooterBar(m))
	h := m.height - topLines - footerLines - 3 // 2 outer padding + 1 gap
	if h < 6 {
		h = 6
	}
	return h
}

// effectiveStats returns m.stats with the public-only filter applied
// when the toggle is on, matching what View renders. Centralised so
// scroll-sync paths and View stay in sync without duplicating the
// branching.
func (m Model) effectiveStats() *github.Stats {
	s := m.stats
	if s != nil && m.client.PublicOnly() {
		s = s.Public()
	}
	return s
}

// syncOverviewViewport rebuilds the Overview viewport's content,
// width, and height from the current model state. Called before
// delegating a key to the viewport in Update so its scroll logic
// (maxYOffset, line clamping) sees the same content the next View
// call will render. The viewport's YOffset is preserved across the
// SetContent so the user's scroll position survives refreshes /
// resizes (clamped automatically by the viewport when the new
// content is shorter than the previous offset).
func syncOverviewViewport(m *Model) {
	if m.stats == nil {
		return
	}
	available := computeAvailable(m.width)
	tabHeight := computeTabHeight(*m)
	if tabHeight <= 0 {
		// No height clamp known yet — render once at a reasonable
		// default so the viewport still has dimensions to scroll
		// against. View() will overwrite this once a WindowSizeMsg
		// arrives.
		tabHeight = 20
	}
	content := m.renderOverviewTab(m.effectiveStats(), available)
	m.overviewVP.Width = available
	m.overviewVP.Height = tabHeight
	m.overviewVP.SetContent(content)
}

// syncActivityViewport mirrors syncOverviewViewport for the Activity
// tab (52-week heatmap + summary). Same shape, different renderer.
func syncActivityViewport(m *Model) {
	if m.stats == nil {
		return
	}
	available := computeAvailable(m.width)
	tabHeight := computeTabHeight(*m)
	if tabHeight <= 0 {
		tabHeight = 20
	}
	content := renderActivityTab(m.effectiveStats(), available)
	m.activityVP.Width = available
	m.activityVP.Height = tabHeight
	m.activityVP.SetContent(content)
}
