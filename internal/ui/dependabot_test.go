package ui

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/gfazioli/octoscope/internal/github"
)

func TestRepoDetailAlerts(t *testing.T) {
	_ = applyTheme("octoscope", "")
	alerts := &github.DependabotAlerts{Critical: 1, High: 6, Low: 1}
	alerts.Alerts = append(alerts.Alerts, github.DependabotAlert{
		Number: 1, Severity: "critical", Package: "lodash", Summary: "Command injection in lodash",
		URL: "https://github.com/o/r/security/dependabot/1", FixedIn: "4.17.21",
	})
	for i := 0; i < 6; i++ {
		alerts.Alerts = append(alerts.Alerts, github.DependabotAlert{
			Number: 10 + i, Severity: "high", Package: fmt.Sprintf("pkg%d", i), Summary: "Denial of service",
			URL: "https://github.com/o/r/security/dependabot/10",
		})
	}
	alerts.Alerts = append(alerts.Alerts, github.DependabotAlert{Number: 99, Severity: "low", Package: "minimist", Summary: "Prototype pollution"})

	t.Run("a tally and the most severe, then a count of the rest", func(t *testing.T) {
		out := ansi.Strip(repoDetailAlerts(&github.RepoDetail{AlertsAccess: github.AccessOK, Alerts: alerts}, 110))
		lines := strings.Split(out, "\n")
		if lines[0] != "Dependabot alerts   8 open · 1 critical · 6 high · 1 low" {
			t.Errorf("heading = %q; want the total and only the severities that have any", lines[0])
		}
		if len(lines) != 1+alertsMaxVisible+1 {
			t.Fatalf("got %d lines, want the heading, %d rows and the overflow:\n%s", len(lines), alertsMaxVisible, out)
		}
		first := lines[1]
		for _, want := range []string{"critical", "lodash", "Command injection in lodash", "fix 4.17.21"} {
			if !strings.Contains(first, want) {
				t.Errorf("first row %q lacks %q", first, want)
			}
		}
		if !strings.Contains(lines[2], "no fix yet") {
			t.Errorf("a row with no patched version must say so: %q", lines[2])
		}
		if strings.TrimSpace(lines[len(lines)-1]) != "+3 more" {
			t.Errorf("overflow = %q, want +3 more (8 open, 5 shown)", lines[len(lines)-1])
		}
		for i, l := range lines {
			if w := ansi.StringWidth(l); w > 110 {
				t.Errorf("line %d is %d cells wide, past the 110 it was given: %q", i, w, l)
			}
		}
		// The fix column starts at the same cell on every row, whatever
		// the version string ("fix 4.17.21" against "no fix yet").
		col := func(l, s string) int { return ansi.StringWidth(l[:strings.Index(l, s)]) }
		if a, b := col(lines[1], "fix 4.17.21"), col(lines[2], "no fix yet"); a != b {
			t.Errorf("fix column at %d and %d, want one column", a, b)
		}
	})

	t.Run("the link covers the summary, not its padding", func(t *testing.T) {
		row := alertRow(github.DependabotAlert{Severity: "high", Package: "x", Summary: "short", URL: "https://github.com/o/r/security/dependabot/1"}, 110)
		// OSC 8 closes with ESC ] 8 ; ; ST; what follows the close is
		// outside the link, and the padding must be there.
		closeAt := strings.LastIndex(row, "\x1b]8;;")
		if closeAt < 0 || !strings.HasPrefix(row[closeAt-len("short"):], "short") {
			t.Errorf("the link does not end right after the summary: %q", row)
		}
	})

	t.Run("a walk that did not finish marks every count a floor", func(t *testing.T) {
		capped := &github.DependabotAlerts{High: 500, Truncated: true}
		if out := ansi.Strip(repoDetailAlerts(&github.RepoDetail{AlertsAccess: github.AccessOK, Alerts: capped}, 110)); !strings.Contains(out, "500+ open · 500+ high") {
			t.Errorf("got %q", out)
		}
		empty := &github.DependabotAlerts{Truncated: true}
		if out := ansi.Strip(repoDetailAlerts(&github.RepoDetail{AlertsAccess: github.AccessOK, Alerts: empty}, 110)); strings.Contains(out, "none open") {
			t.Errorf("an unfinished walk that counted nothing said %q", out)
		}
	})

	t.Run("a severity GitHub adds later is named other", func(t *testing.T) {
		odd := &github.DependabotAlerts{Other: 1, Alerts: []github.DependabotAlert{{Severity: "other", Package: "p", Summary: "s"}}}
		if out := ansi.Strip(repoDetailAlerts(&github.RepoDetail{AlertsAccess: github.AccessOK, Alerts: odd}, 110)); !strings.Contains(out, "1 open · 1 other") {
			t.Errorf("got %q", out)
		}
	})

	t.Run("narrow terminals keep every line inside its width, and the fix", func(t *testing.T) {
		every := &github.DependabotAlerts{Critical: 1, High: 1, Medium: 1, Low: 1, Other: 1, Alerts: alerts.Alerts}
		for _, width := range []int{79, 60, 40} {
			for _, d := range []*github.RepoDetail{
				{AlertsAccess: github.AccessOK, Alerts: alerts},
				{AlertsAccess: github.AccessOK, Alerts: every},
				{AlertsAccess: github.AccessDisabled},
				{AlertsAccess: github.AccessOK, Alerts: &github.DependabotAlerts{}},
			} {
				out := ansi.Strip(repoDetailAlerts(d, width))
				for i, l := range strings.Split(out, "\n") {
					if w := ansi.StringWidth(l); w > width {
						t.Errorf("width %d, line %d is %d cells: %q", width, i, w, l)
					}
				}
			}
			// The summary keeps room to say what is wrong.
			if out := ansi.Strip(repoDetailAlerts(&github.RepoDetail{AlertsAccess: github.AccessOK, Alerts: alerts}, width)); !strings.Contains(out, "Command inj") {
				t.Errorf("width %d squeezed the summary out:\n%s", width, out)
			}
			// The version may be cut at a narrow width; that a fix
			// exists, or not, may not.
			if out := ansi.Strip(repoDetailAlerts(&github.RepoDetail{AlertsAccess: github.AccessOK, Alerts: alerts}, width)); !strings.Contains(out, "fix 4.") || !strings.Contains(out, "no fix yet") {
				t.Errorf("width %d dropped whether a fix exists:\n%s", width, out)
			}
		}
	})

	t.Run("none open is an answer", func(t *testing.T) {
		out := ansi.Strip(repoDetailAlerts(&github.RepoDetail{AlertsAccess: github.AccessOK, Alerts: &github.DependabotAlerts{}}, 110))
		if out != "Dependabot alerts   none open" {
			t.Errorf("got %q", out)
		}
	})

	t.Run("switched off is said", func(t *testing.T) {
		out := ansi.Strip(repoDetailAlerts(&github.RepoDetail{AlertsAccess: github.AccessDisabled}, 110))
		if out != "Dependabot alerts   off for this repository" {
			t.Errorf("got %q", out)
		}
	})

	t.Run("a token short the permission says which one", func(t *testing.T) {
		out := strings.Join(strings.Fields(ansi.Strip(repoDetailAlerts(&github.RepoDetail{AlertsAccess: github.AccessTokenLacks}, 110))), " ")
		// It names the permission, and does not claim to know it is the
		// token: an organisation can also keep alerts to its admins.
		for _, want := range []string{"refused this token", "Dependabot alerts (read)", "security_events", "keep alerts to its admins"} {
			if !strings.Contains(out, want) {
				t.Errorf("missing %q in %q", want, out)
			}
		}
	})

	t.Run("a failure says why", func(t *testing.T) {
		out := ansi.Strip(repoDetailAlerts(&github.RepoDetail{AlertsAccess: github.AccessFailed, AlertsErr: errors.New("GitHub answered 503")}, 110))
		if !strings.Contains(out, "503") || !strings.Contains(out, "r reloads") {
			t.Errorf("got %q", out)
		}
	})

	t.Run("silent where GitHub is", func(t *testing.T) {
		for _, a := range []github.Access{github.AccessNotPermitted, github.AccessNotAsked} {
			if out := repoDetailAlerts(&github.RepoDetail{AlertsAccess: a}, 110); out != "" {
				t.Errorf("access %d rendered %q", a, out)
			}
		}
	})

	t.Run("in the drill-in body, beside Checks", func(t *testing.T) {
		rd := newLoadedRepoDetail(&github.RepoDetail{
			Owner: "gfazioli", Name: "octoscope", URL: "https://github.com/gfazioli/octoscope",
			AlertsAccess: github.AccessOK, Alerts: alerts,
		})
		if body := ansi.Strip(rd.computeBody(110)); !strings.Contains(body, "Dependabot alerts   8 open") {
			t.Errorf("the drill-in does not paint the section:\n%s", body)
		}
	})
}
