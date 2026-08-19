package ui

// Sponsors — who funds this account and who it funds (#72), on the
// Overview tab.
//
// Two surfaces, deliberately, because they answer different questions with
// data of different shapes. The **counts** join the Social card row, where
// a person already looks for followers and stars, and get the pulse-on-
// change treatment for free — a new sponsor arriving is exactly the kind of
// passive event the pulse exists for. The **names** get their own section,
// modelled on Network: a label column with the list packed and wrapped
// under it, because a name list is not a number and does not belong in a
// card.
//
// Both are absent by default. An account nobody sponsors and that sponsors
// nobody shows neither the cards nor the section — the same rule the
// pinned / watched / review-request sections follow, and the reason the
// tab does not grow a permanently empty row for most users.

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/gfazioli/octoscope/internal/github"
)

// sponsorCards returns the conditional additions to the Social card row.
// Nothing is added for an account with no sponsorship in either direction,
// so the row keeps its shape for the overwhelming majority of users.
func sponsorCards(s *github.Stats) []cardSpec {
	if s == nil {
		return nil
	}
	var out []cardSpec
	if s.SponsorsTotal > 0 {
		out = append(out, cardSpec{
			id: "sponsors", icon: "♥", label: "Sponsors", short: "Sponsors",
			value: s.SponsorsTotal,
		})
	}
	if s.SponsoringTotal > 0 {
		out = append(out, cardSpec{
			id: "sponsoring", icon: "♡", label: "Sponsoring", short: "Sponsoring",
			value: s.SponsoringTotal,
		})
	}
	return out
}

// sponsorLabels turns a capped list into the strings the row prints, and
// appends the honest remainder when the fetch saw fewer than GitHub has.
//
// The remainder is computed from the *total*, not from the page size, so a
// list that happens to be short does not claim to be truncated — the same
// distinction the events feed draws about its own window.
func sponsorLabels(list []github.Sponsor, total int) []string {
	out := make([]string, 0, len(list)+1)
	for _, sp := range list {
		label := sp.Label()
		if sp.IsOrg {
			// Organizations and people read differently in a mixed list,
			// and GitHub's own listing separates them. One glyph is enough
			// to stop "Acme Inc" looking like somebody's display name.
			label = "▣ " + label
		}
		out = append(out, label)
	}
	if rest := total - len(list); rest > 0 {
		out = append(out, fmt.Sprintf("and %d more", rest))
	}
	return out
}

// formatCents renders a cents amount as US dollars, which is the unit
// GitHub reports in and the only one it reports in.
func formatCents(cents int) string {
	return fmt.Sprintf("$%d.%02d", cents/100, cents%100)
}

// The row labels deliberately do not repeat the section title. Rendered
// under a heading of "Sponsors", a row also labelled "Sponsors" reads as a
// stutter — which is only visible by running it, not by reading it. The
// issue's own wording ("sponsors received and given") is the fix.
const (
	sponsorsLabel   = "Received"
	sponsoringLabel = "Given   "
	incomeLabel     = "Income  "
	sponsorLabelGap = "  "
)

// renderSponsors draws the section body: who sponsors this account, who it
// sponsors, and — for the viewer only — GitHub's monthly income estimate.
//
// Returns "" when there is nothing worth a section, which is what keeps it
// off the Overview for accounts with no sponsorship at all.
func renderSponsors(s *github.Stats, available int) string {
	if s == nil {
		return ""
	}

	valueWidth := available - lipgloss.Width(sponsorsLabel) - lipgloss.Width(sponsorLabelGap)
	if valueWidth < 20 {
		valueWidth = 20
	}

	line := func(label string, items []string) string {
		packed := packLines(items, " · ", valueWidth)
		return mutedStyle.Render(label) + sponsorLabelGap +
			indentContinuation(packed, label, sponsorLabelGap)
	}

	var lines []string

	switch {
	case len(s.Sponsors) > 0:
		lines = append(lines, line(sponsorsLabel, sponsorLabels(s.Sponsors, s.SponsorsTotal)))
	case s.SponsorsTotal > 0:
		// A count with no names is what public-only leaves behind on a
		// different code path, and it is also what a future cap change
		// could produce. Say the number rather than drawing an empty row.
		lines = append(lines, mutedStyle.Render(sponsorsLabel)+sponsorLabelGap+
			valueStyle.Render(fmt.Sprintf("%d", s.SponsorsTotal)))
	case s.HasSponsorsListing:
		// The distinction worth drawing: this account *can* be sponsored
		// and nobody has yet. Saying nothing here would read as "octoscope
		// does not know", which is a different and wronger claim.
		lines = append(lines, mutedStyle.Render(sponsorsLabel)+sponsorLabelGap+
			mutedStyle.Render("nobody yet — the listing is open"))
	}

	if len(s.Sponsoring) > 0 {
		lines = append(lines, line(sponsoringLabel, sponsorLabels(s.Sponsoring, s.SponsoringTotal)))
	} else if s.SponsoringTotal > 0 {
		lines = append(lines, mutedStyle.Render(sponsoringLabel)+sponsorLabelGap+
			valueStyle.Render(fmt.Sprintf("%d", s.SponsoringTotal)))
	}

	// Income is the viewer's own, or it is not shown. GitHub answers this
	// field with 0 for anyone else, so the github package only carries it
	// in viewer mode — a zero here means "not ours to say", never "zero".
	if s.MonthlySponsorsIncomeCents > 0 {
		lines = append(lines, mutedStyle.Render(incomeLabel)+sponsorLabelGap+
			valueStyle.Render(formatCents(s.MonthlySponsorsIncomeCents))+
			mutedStyle.Render(" / month estimated by GitHub"))
	}

	// No explicit empty check: strings.Join over no lines is already "",
	// which is the signal the caller tests for. Two earlier guards said the
	// same thing — an up-front "is there any sponsorship" predicate and a
	// len(lines)==0 return — and mutating either away changed no behaviour
	// and failed no test, which is the definition of a second source of
	// truth. One mechanism, stated once.
	return strings.Join(lines, "\n")
}
