package ui

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/gfazioli/octoscope/internal/github"
)

// GitHub's own status, surfaced so that an outage on GitHub's side stops
// looking like a bug in octoscope. Two placements, and the second is the
// one that does the real work: a line on the dashboard while something
// octoscope depends on is unhealthy, and a sentence on the error screen
// at the exact moment the user is deciding who to blame.
//
// Silent while green, by construction: ServiceStatus.Affected only ever
// holds components that are not operational, so a healthy fetch renders
// nothing and a *failed* fetch renders nothing either. octoscope never
// says GitHub is fine — it only ever says GitHub is not.

// serviceStatusTTL is how long a fetched status is reused. Statuspage is
// somebody else's infrastructure and this feature does not poll it: the
// fetch happens at startup, on a manual refresh, and after a failed
// GitHub request, with this TTL collapsing bursts of those.
//
// Two minutes sits well inside the useful range: measured across 50 real
// GitHub incidents on 2026-08-29 the median lasted 81 minutes and the
// fastest tenth still ran 25, so a two-minute-old answer has never been
// the difference between right and wrong. The page's own Cache-Control
// is max-age=10, so this is the conservative side of what it invites.
const serviceStatusTTL = 2 * time.Minute

// serviceStatusMaxAge is how old an answer may be and still be shown.
//
// It is deliberately longer than the fetch TTL and is a safety net, not
// the mechanism: while a banner is up the dashboard's own refresh keeps
// re-asking, so in practice the displayed state is at most one refresh
// interval old. This bounds the pathological case — a session left idle
// with a very long --refresh — where octoscope would otherwise keep
// asserting an incident that ended hours ago. Reporting a stale outage
// is the same class of error as reporting a clean state that was never
// verified, just pointed the other way.
const serviceStatusMaxAge = 15 * time.Minute

// serviceStatusTimeout bounds the whole command. Best-effort: the
// dashboard never waits on it, and a slow third party must not be why a
// refresh feels slow.
const serviceStatusTimeout = 8 * time.Second

// serviceStatusMsg carries the outcome. A nil st is a failed or useless
// fetch and is stored as nil — which renders nothing, rather than the
// last known state, because a stale "GitHub is down" is its own kind of
// lie once the incident is over.
type serviceStatusMsg struct{ st *github.ServiceStatus }

// serviceStatusCmd is indirected through a variable so the *wiring* is
// testable and not merely the function. The command is constructed at
// the moment a handler decides to ask, so swapping this in a test
// records that decision without any network and without walking an
// opaque tea.Batch. A perfect command nothing ever issues is the
// failure mode that hides best.
var serviceStatusCmd = fetchServiceStatusCmd

func fetchServiceStatusCmd() tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), serviceStatusTimeout)
		defer cancel()
		st, err := github.FetchServiceStatus(ctx)
		if err != nil {
			return serviceStatusMsg{st: nil}
		}
		return serviceStatusMsg{st: st}
	}
}

// wantsServiceStatus reports whether this session checks GitHub's status
// at all.
//
// Two gates, and the second is not the one the issue guessed at. The
// config knob is the opt-out for anyone who wants octoscope talking to
// exactly one host: this is the only feature in the tool that contacts a
// host other than api.github.com, which is what makes it the only one
// that needs the knob.
//
// --public-only is gated for the same reason the update check is, and it
// is not about privacy — a public incident is not sensitive. It is that
// the VHS tapes are recorded in public-only mode, so an incident during
// a recording session would silently paint a warning line into a
// screenshot that is meant to be reproducible.
func (m Model) wantsServiceStatus() bool {
	return m.checkServiceStatus && !m.client.PublicOnly()
}

// maybeFetchServiceStatus returns a fetch command unless the session has
// opted out or a recent answer is still good. nil means "nothing to do".
func (m Model) maybeFetchServiceStatus(now time.Time) tea.Cmd {
	if !m.wantsServiceStatus() {
		return nil
	}
	if !m.serviceStatusAt.IsZero() && now.Sub(m.serviceStatusAt) < serviceStatusTTL {
		return nil
	}
	return serviceStatusCmd()
}

// renderServiceStatusLines is the dashboard placement: nothing at all
// while every component octoscope depends on is operational.
func renderServiceStatusLines(st *github.ServiceStatus, width int) string {
	if !st.Impaired() {
		return ""
	}
	line := warnStyle.Render("⚠ " + st.Headline())

	if note := serviceIncidentNote(st); note != "" {
		w := width
		if w < 20 {
			w = 20
		}
		line += "\n" + mutedStyle.Width(w).Render("  "+note)
	}
	return line
}

// serviceIncidentNote is the incident context, as plain text.
//
// Incidents are deliberately not filtered by which components they name
// — 15 of 50 real incidents carried no component join at all — so this
// reports whatever GitHub currently has open, and only ever appears
// alongside a component that is genuinely impaired.
func serviceIncidentNote(st *github.ServiceStatus) string {
	if st == nil || len(st.Incidents) == 0 {
		return ""
	}
	i := st.Incidents[0]
	parts := []string{i.Name}
	if i.Status != "" {
		parts = append(parts, i.Status)
	}
	if i.URL != "" {
		parts = append(parts, i.URL)
	}
	note := strings.Join(parts, " · ")
	if n := len(st.Incidents) - 1; n > 0 {
		suffix := " · and %d more incidents"
		if n == 1 {
			suffix = " · and %d more incident"
		}
		note += fmt.Sprintf(suffix, n)
	}
	return note
}

// serviceStatusExplains reports whether a GitHub incident could
// plausibly be *the cause* of this particular failure.
//
// Found by running it rather than by a test: against a deliberately
// broken token the error screen said "Token expired or revoked" and
// then, underneath, "very likely not octoscope". Both sentences were
// true, and together they were worse than either alone — a correct
// diagnosis muddied by an irrelevant one. A 401 is not what a partial
// outage looks like, an exhausted hourly budget is the user's own, and a
// warning that fires where it does not apply is exactly the kind people
// learn to dismiss.
//
// The dashboard banner stays unconditional because it is information.
// This gate exists because the error screen makes a causal claim, and a
// causal claim needs a plausible cause.
func serviceStatusExplains(r github.FetchErrorReason) bool {
	switch r {
	case github.ReasonServer, github.ReasonNetwork, github.ReasonUnknown:
		return true
	}
	return false
}

// serviceStatusDetail is the error-screen placement, and the higher
// value half of the feature: it turns "octoscope could not fetch this"
// into "GitHub says this is GitHub", at the exact moment the user is
// deciding who to blame. Plain text; the caller styles it.
//
// It leads with the conclusion rather than burying it after the generic
// advice, because the one thing worth reading here is that the tool in
// front of them is probably fine.
//
// Returns "" when there is nothing to add, so the caller can append
// unconditionally.
func serviceStatusDetail(st *github.ServiceStatus) string {
	if !st.Impaired() {
		return ""
	}
	names := make([]string, 0, len(st.Affected))
	affects := make([]string, 0, len(st.Affected))
	for _, c := range st.Affected {
		names = append(names, c.Name)
		affects = append(affects, c.Affects)
	}
	out := "⚠ Very likely not octoscope — GitHub reports " + joinList(names) + " " +
		st.Worst().Label() + " right now, which affects " + joinAffects(affects) + "."
	if note := serviceIncidentNote(st); note != "" {
		out += " " + note
	}
	return out
}

// joinAffects renders the "what you would notice" strings as a
// sentence, with a fallback for the case where none of them carry text.
func joinAffects(items []string) string {
	if out := joinList(items); out != "" {
		return out
	}
	return "what octoscope shows"
}

// joinList renders a list the way a sentence would, dropping blanks and
// duplicates — two impaired components that break the same thing must
// not say it twice.
func joinList(items []string) string {
	seen := make(map[string]bool, len(items))
	uniq := make([]string, 0, len(items))
	for _, s := range items {
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		uniq = append(uniq, s)
	}
	switch len(uniq) {
	case 0:
		return ""
	case 1:
		return uniq[0]
	case 2:
		return uniq[0] + " and " + uniq[1]
	}
	return strings.Join(uniq[:len(uniq)-1], ", ") + " and " + uniq[len(uniq)-1]
}

// visibleServiceStatus is the status the view may render: nil unless the
// session wants the check at all, a component octoscope depends on is
// impaired, *and* the measurement is recent enough to still stand behind.
//
// wantsServiceStatus is re-read here rather than only at fetch time,
// which is not belt-and-braces but the whole point of the public-only
// gate: `p` is pressed precisely when someone is about to show the
// screen to somebody else, and a warning already on screen would
// otherwise survive the switch for up to serviceStatusMaxAge. The update
// notice one line above this in the view has always been a render-time
// check; this matches it. Caught by review, reproduced by test first.
func (m Model) visibleServiceStatus() *github.ServiceStatus {
	if !m.wantsServiceStatus() || !m.serviceStatus.Impaired() {
		return nil
	}
	if m.serviceStatusAt.IsZero() || time.Since(m.serviceStatusAt) > serviceStatusMaxAge {
		return nil
	}
	return m.serviceStatus
}
