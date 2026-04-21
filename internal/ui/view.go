package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/gfazioli/octoscope/internal/github"
)

// View renders the current model. Layout for v0.2.0:
//
//  1. Profile header (name · @login · pronouns · bio)
//  2. Meta row (company · location · website · member since)
//  3. Social section (followers / following / stars received)
//  4. Activity section (PRs / merged / issues / commits) + languages bar
//  5. Operational section (repos / forks / open issues / open PRs)
//  6. Network section (organizations · social links)
//  7. Footer (freshness + hotkeys)
//
// Each section renders itself as a standalone block separated by a
// blank line. No hard wrapping — if the window is too narrow, Lipgloss
// lets the row overflow rather than truncate silently.
func (m Model) View() string {
	var b strings.Builder

	b.WriteString(titleStyle.Render("octoscope") + "\n\n")

	if m.loading && m.stats == nil {
		b.WriteString(mutedStyle.Render("Loading…"))
		b.WriteString("\n\n" + mutedStyle.Render("q quit"))
		return b.String()
	}

	if m.err != nil && m.stats == nil {
		b.WriteString(errorStyle.Render("Could not fetch stats") + "\n")
		b.WriteString(mutedStyle.Render(m.err.Error()) + "\n\n")
		b.WriteString(mutedStyle.Render("r retry · q quit"))
		return b.String()
	}

	s := m.stats

	b.WriteString(renderProfileHeader(s) + "\n")
	b.WriteString(renderMetaRow(s) + "\n\n")
	b.WriteString(renderSection("Social", renderSocial(s)) + "\n")
	b.WriteString(renderSection("Activity (last year for commits)", renderActivity(s)) + "\n")
	b.WriteString(renderSection("Operational", renderOperational(s)) + "\n")
	b.WriteString(renderSection("Network", renderNetwork(s)) + "\n")

	b.WriteString(renderFooter(m))

	return b.String()
}

// ---------- Section scaffolding ----------

// renderSection wraps a body with a small colored title above it. Keeps
// visual hierarchy consistent without resorting to heavy borders (which
// gobble horizontal space on narrow terminals).
func renderSection(title, body string) string {
	return sectionTitleStyle.Render(title) + "\n" + body
}

// ---------- Profile header ----------

func renderProfileHeader(s *github.Stats) string {
	name := s.Name
	if name == "" {
		name = s.Login
	}
	parts := []string{boldStyle.Render(name), mutedStyle.Render("@" + s.Login)}
	if s.Pronouns != "" {
		parts = append(parts, mutedStyle.Render("· "+s.Pronouns))
	}
	parts = append(parts, authBadge(s.Authenticated))
	header := strings.Join(parts, "  ")

	if s.Bio != "" {
		header += "\n" + s.Bio
	}
	return header
}

func authBadge(authenticated bool) string {
	if authenticated {
		return okStyle.Render("● authenticated")
	}
	return warnStyle.Render("● unauthenticated")
}

// renderMetaRow renders company, location, website, twitter and member
// years on one (wrap-aware) line. Only non-empty fields show up.
func renderMetaRow(s *github.Stats) string {
	var parts []string
	if s.Company != "" {
		parts = append(parts, mutedStyle.Render("🏢")+" "+s.Company)
	}
	if s.Location != "" {
		parts = append(parts, mutedStyle.Render("📍")+" "+s.Location)
	}
	if s.WebsiteURL != "" {
		parts = append(parts, mutedStyle.Render("🔗")+" "+s.WebsiteURL)
	}
	if !s.CreatedAt.IsZero() {
		years := time.Since(s.CreatedAt).Hours() / 24 / 365
		parts = append(parts, mutedStyle.Render(fmt.Sprintf("⏱ %.0f years on GitHub", years)))
	}
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, "   ")
}

// ---------- Stat sections ----------

func renderSocial(s *github.Stats) string {
	return lipgloss.JoinHorizontal(lipgloss.Top,
		statBox("Followers", s.Followers),
		statBox("Following", s.Following),
		statBox("Stars Received", s.TotalStars),
	)
}

func renderActivity(s *github.Stats) string {
	row := lipgloss.JoinHorizontal(lipgloss.Top,
		statBox("PRs Authored", s.PRsTotal),
		statBox("PRs Merged", s.PRsMerged),
		statBox("Issues Authored", s.IssuesAuthored),
		statBox("Commits (yr)", s.CommitsLastYear),
	)
	if len(s.Languages) == 0 {
		return row
	}
	return row + "\n\n" + renderLanguages(s.Languages)
}

func renderOperational(s *github.Stats) string {
	return lipgloss.JoinHorizontal(lipgloss.Top,
		statBox("Public Repos", s.PublicRepos),
		statBox("Forks Received", s.ForksReceived),
		statBox("Open Issues", s.OpenIssues),
		statBox("Open PRs", s.OpenPRs),
	)
}

func renderNetwork(s *github.Stats) string {
	var lines []string

	if len(s.Organizations) > 0 {
		var logins []string
		for _, o := range s.Organizations {
			logins = append(logins, o.Login)
		}
		lines = append(lines, mutedStyle.Render("Organizations")+"  "+strings.Join(logins, " · "))
	}

	if len(s.SocialAccounts) > 0 || s.TwitterUsername != "" {
		var links []string
		seen := map[string]bool{}
		// TwitterUsername is often duplicated by SocialAccounts; dedupe
		// by URL to avoid showing "@me · @me".
		if s.TwitterUsername != "" {
			url := "https://twitter.com/" + s.TwitterUsername
			links = append(links, "@"+s.TwitterUsername)
			seen[url] = true
		}
		for _, sa := range s.SocialAccounts {
			if seen[sa.URL] {
				continue
			}
			label := sa.DisplayName
			if label == "" {
				label = sa.URL
			}
			links = append(links, label)
			seen[sa.URL] = true
		}
		if len(links) > 0 {
			lines = append(lines, mutedStyle.Render("Social       ")+"  "+strings.Join(links, " · "))
		}
	}

	if len(lines) == 0 {
		return mutedStyle.Render("(no public organizations or social links)")
	}
	return strings.Join(lines, "\n")
}

// ---------- Languages bar ----------

// renderLanguages draws a horizontal bar per top-language with the
// colour GitHub itself assigns to the language. Percentages are
// computed against the total bytes in the top-N set (what we rendered),
// not across every language ever touched — keeps the bars adding up
// to ~100% visually.
func renderLanguages(langs []github.Language) string {
	if len(langs) == 0 {
		return ""
	}
	var total int
	for _, l := range langs {
		total += l.Bytes
	}
	if total == 0 {
		return ""
	}

	const barWidth = 24
	var longestName int
	for _, l := range langs {
		if len(l.Name) > longestName {
			longestName = len(l.Name)
		}
	}

	var b strings.Builder
	b.WriteString(mutedStyle.Render("Languages") + "\n")
	for _, l := range langs {
		pct := float64(l.Bytes) / float64(total) * 100
		filled := int(float64(barWidth)*pct/100 + 0.5)
		if filled < 1 && l.Bytes > 0 {
			filled = 1
		}

		barColour := lipgloss.Color(l.Color)
		if l.Color == "" {
			barColour = colMuted
		}
		filledBar := lipgloss.NewStyle().Foreground(barColour).Render(strings.Repeat("█", filled))
		emptyBar := mutedStyle.Render(strings.Repeat("░", barWidth-filled))

		line := fmt.Sprintf(
			"  %-*s  %s%s  %5.1f%%",
			longestName, l.Name, filledBar, emptyBar, pct,
		)
		b.WriteString(line + "\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

// ---------- Stat card ----------

// statBox renders a single card. Numeric values get a K/M suffix once
// they cross 10 000 — easier to scan and keeps the card width bounded.
func statBox(label string, value int) string {
	return boxStyle.Render(
		mutedStyle.Render(label) + "\n" +
			valueStyle.Render(formatCompact(value)),
	)
}

// formatCompact returns either a thousands-separated number (for
// values up to 9 999) or a k/M-shortened one. Chosen over strict
// "always shorten" because for a stat like Followers = 119 the raw
// number is what the user wants to see.
func formatCompact(n int) string {
	switch {
	case n >= 1_000_000:
		return fmt.Sprintf("%.1fM", float64(n)/1_000_000)
	case n >= 10_000:
		return fmt.Sprintf("%.1fk", float64(n)/1_000)
	case n >= 1_000:
		return formatThousands(n)
	default:
		return fmt.Sprintf("%d", n)
	}
}

func formatThousands(n int) string {
	s := fmt.Sprintf("%d", n)
	if len(s) <= 3 {
		return s
	}
	var out []byte
	for i, c := range []byte(s) {
		if i > 0 && (len(s)-i)%3 == 0 {
			out = append(out, ',')
		}
		out = append(out, c)
	}
	return string(out)
}

// ---------- Footer ----------

func renderFooter(m Model) string {
	age := time.Since(m.lastFetch).Truncate(time.Second)
	if m.err != nil {
		return errorStyle.Render("stale — last refresh errored") + "  " +
			mutedStyle.Render("r retry · q quit")
	}
	return mutedStyle.Render(fmt.Sprintf(
		"Updated %s ago  ·  auto-refresh %ds  ·  r refresh  ·  q quit",
		age, int(m.interval.Seconds()),
	))
}
