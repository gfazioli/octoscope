package ui

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/gfazioli/octoscope/internal/github"
)

// RateLimitModel is the rate-limit detail panel (v0.18.0), opened
// with `%` from any tab. The footer chip stays the ambient
// indicator; this panel is the "why is octoscope slow / 403-ing"
// debugging surface — a per-resource breakdown of every REST +
// GraphQL budget from `GET /rate_limit` (an endpoint that is
// itself free and consumes no quota).
//
// Same three-state shape as the drill-ins: loading (Open + fetch
// cmd), error (`r retry · esc back`), loaded. Unlike the help
// overlay it does NOT dismiss on any key — `r` refetches, so keys
// are routed explicitly.
type RateLimitModel struct {
	open    bool
	loading bool
	err     error
	limits  *github.RateLimits
}

// IsOpen reports whether the panel is visible.
func (rl RateLimitModel) IsOpen() bool { return rl.open }

// Open returns a fresh panel in the loading state. Caller pairs it
// with fetchRateLimitsCmd so loading actually resolves.
func (rl RateLimitModel) Open() RateLimitModel {
	return RateLimitModel{open: true, loading: true}
}

// Close returns a dismissed panel (zero value).
func (rl RateLimitModel) Close() RateLimitModel { return RateLimitModel{} }

// applyFetched commits a fetch result into the panel.
func (rl RateLimitModel) applyFetched(limits *github.RateLimits, err error) RateLimitModel {
	rl.loading = false
	rl.limits = limits
	rl.err = err
	return rl
}

// Update handles a key while the panel is open. esc closes, r
// refetches, q quits the app (global escape hatch, consistent with
// the drill-ins). Everything else is ignored — the panel is a
// short fixed-height table, no scrolling machinery needed.
func (rl RateLimitModel) Update(msg tea.KeyMsg, client *github.Client) (RateLimitModel, tea.Cmd) {
	if !rl.open {
		return rl, nil
	}
	switch msg.String() {
	case "q":
		return rl.Close(), tea.Quit
	case "esc":
		return rl.Close(), nil
	case "r":
		rl.loading = true
		rl.err = nil
		return rl, fetchRateLimitsCmd(client)
	}
	return rl, nil
}

// View renders the centered, accent-bordered panel — same chrome as
// the help overlay so the two "App-level modals" read as siblings.
func (rl RateLimitModel) View(width int) string {
	if !rl.open {
		return ""
	}

	var lines []string
	title := boldStyle.Foreground(colAccent).Render("API rate limits")
	switch {
	case rl.loading:
		lines = []string{title, "", mutedStyle.Render("Loading rate limits…")}
	case rl.err != nil:
		lines = []string{
			title,
			"",
			errorStyle.Render("Could not fetch rate limits"),
			mutedStyle.Render(cleanErr(rl.err)),
			"",
			keyHints("r", "retry", "esc", "back"),
		}
	case rl.limits == nil || len(rl.limits.Resources) == 0:
		lines = []string{title, "", mutedStyle.Render("(no rate-limit data)"),
			"", keyHints("r", "refresh", "esc", "back")}
	default:
		lines = append([]string{title, ""}, rateLimitTable(rl.limits)...)
		lines = append(lines, "",
			mutedStyle.Render("free endpoint — checking does not consume quota"),
			"",
			keyHints("r", "refresh", "esc", "back", "q", "quit"),
		)
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

// rateLimitTable renders the per-resource rows: name, used/limit
// with a remaining figure, and the reset ETA. Row colour follows
// the footer chip's thresholds (muted ≥20% remaining, warn <20%,
// error <5%) so the two surfaces never disagree about urgency.
func rateLimitTable(limits *github.RateLimits) []string {
	const (
		nameW   = 22
		usedW   = 13
		remainW = 11
		resetW  = 9
	)

	header := mutedStyle.Render(
		padRight("Resource", nameW) + "  " +
			padLeft("Used/Limit", usedW) + "  " +
			padLeft("Remaining", remainW) + "  " +
			padLeft("Reset", resetW))
	rule := tabRuleStyle.Render(strings.Repeat("─", lipgloss.Width(header)))
	rows := []string{header, rule}

	for _, r := range limits.Resources {
		style := mutedStyle
		if r.Limit > 0 {
			pct := float64(r.Remaining) / float64(r.Limit)
			switch {
			case pct < 0.05:
				style = lipgloss.NewStyle().Foreground(colError)
			case pct < 0.20:
				style = lipgloss.NewStyle().Foreground(colWarn)
			}
		}

		name := padRight(truncate(r.Name, nameW), nameW)
		used := padLeft(fmt.Sprintf("%d/%d", r.Used, r.Limit), usedW)
		remain := padLeft(fmt.Sprintf("%d", r.Remaining), remainW)
		reset := padLeft(formatResetETA(r.Reset), resetW)

		// The leading budgets are the ones octoscope spends — keep
		// their names readable (value style), let the long tail stay
		// muted unless its threshold colour kicks in.
		nameStyled := style.Render(name)
		if style.GetForeground() == mutedStyle.GetForeground() && isLeaderResource(r.Name) {
			nameStyled = valueStyle.Render(name)
		}
		rows = append(rows,
			nameStyled+"  "+style.Render(used+"  "+remain+"  "+reset))
	}
	return rows
}

// isLeaderResource reports whether the resource is one octoscope
// actively spends (mirrors github.rateLeaders without exporting it
// — the display emphasis is a UI concern).
func isLeaderResource(name string) bool {
	switch name {
	case "graphql", "core", "search":
		return true
	}
	return false
}

// rateLimitsFetchedMsg carries the /rate_limit result back to the
// root Update.
type rateLimitsFetchedMsg struct {
	limits *github.RateLimits
	err    error
}

// fetchRateLimitsCmd pulls the per-resource budget snapshot. 10s
// timeout — it's a single tiny REST call; longer means something is
// genuinely wrong and worth surfacing as the panel's error state.
func fetchRateLimitsCmd(client *github.Client) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		limits, err := client.FetchRateLimits(ctx)
		return rateLimitsFetchedMsg{limits: limits, err: err}
	}
}
