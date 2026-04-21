package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
)

// View renders the current model. Layout for v0.1.0:
//
//	 octoscope
//
//	 Display Name  @login                [• auth]
//
//	 ┌─────────┐ ┌─────────┐ ┌─────────┐ ┌─────────┐
//	 │ Followers │ Following │ Repos    │ Stars    │
//	 │   123     │    45     │   67     │   1.2k   │
//	 └─────────┘ └─────────┘ └─────────┘ └─────────┘
//
//	 ┌─────────┐ ┌─────────┐
//	 │ Issues   │ PRs       │
//	 │    7     │    3      │
//	 └─────────┘ └─────────┘
//
//	 Updated 12s ago • r refresh • q quit
func (m Model) View() string {
	var b strings.Builder

	b.WriteString(titleStyle.Render("octoscope") + "\n\n")

	// Loading state on first fetch — subsequent refreshes keep the last
	// stats on screen instead of flashing to "Loading…".
	if m.loading && m.stats == nil {
		b.WriteString(mutedStyle.Render("Loading…"))
		b.WriteString("\n\n" + mutedStyle.Render("q quit"))
		return b.String()
	}

	if m.err != nil && m.stats == nil {
		b.WriteString(errorStyle.Render("Could not fetch stats") + "\n")
		b.WriteString(mutedStyle.Render(m.err.Error()) + "\n\n")
		b.WriteString(mutedStyle.Render("r retry • q quit"))
		return b.String()
	}

	s := m.stats

	// Header line: name + @login + auth badge
	displayName := s.Name
	if displayName == "" {
		displayName = s.Login
	}
	authBadge := warnStyle.Render("● unauthenticated")
	if s.Authenticated {
		authBadge = okStyle.Render("● authenticated")
	}
	b.WriteString(
		boldStyle.Render(displayName) + "  " +
			mutedStyle.Render("@"+s.Login) + "  " +
			authBadge + "\n\n",
	)

	// Primary stats row
	b.WriteString(lipgloss.JoinHorizontal(lipgloss.Top,
		statBox("Followers", s.Followers),
		statBox("Following", s.Following),
		statBox("Public Repos", s.PublicRepos),
		statBox("Total Stars", s.TotalStars),
	) + "\n")

	// Secondary row — backlog signals
	b.WriteString(lipgloss.JoinHorizontal(lipgloss.Top,
		statBox("Open Issues", s.OpenIssues),
		statBox("Open PRs", s.OpenPRs),
	) + "\n\n")

	// Footer: freshness + hotkeys. If the last fetch errored but we
	// still have cached stats, flag it so the user knows the numbers
	// might be stale.
	var footer string
	age := time.Since(m.lastFetch).Truncate(time.Second)
	if m.err != nil {
		footer = errorStyle.Render("stale — last refresh errored") + "  " +
			mutedStyle.Render(fmt.Sprintf("r retry • q quit"))
	} else {
		footer = mutedStyle.Render(fmt.Sprintf(
			"Updated %s ago  •  auto-refresh %ds  •  r refresh  •  q quit",
			age, int(m.interval.Seconds()),
		))
	}
	b.WriteString(footer)

	return b.String()
}

// statBox renders a single card. Numeric values get thousand-separators
// once they cross 1000 so "1250" becomes "1,250" — easier to parse at
// a glance.
func statBox(label string, value int) string {
	return boxStyle.Render(
		mutedStyle.Render(label) + "\n" +
			valueStyle.Render(formatInt(value)),
	)
}

// formatInt adds a thin "," thousands separator. We avoid locale-aware
// formatting on purpose — terminal output stays predictable across
// LANG/LC_ALL variations on CI and shared machines.
func formatInt(n int) string {
	s := fmt.Sprintf("%d", n)
	if len(s) <= 3 {
		return s
	}
	// Insert a comma every 3 digits from the right.
	var out []byte
	for i, c := range []byte(s) {
		if i > 0 && (len(s)-i)%3 == 0 {
			out = append(out, ',')
		}
		out = append(out, c)
	}
	return string(out)
}
