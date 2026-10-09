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
	})

	t.Run("a capped walk says its counts are a floor", func(t *testing.T) {
		capped := &github.DependabotAlerts{High: 500, Truncated: true}
		if out := ansi.Strip(repoDetailAlerts(&github.RepoDetail{AlertsAccess: github.AccessOK, Alerts: capped}, 110)); !strings.Contains(out, "500+ open") {
			t.Errorf("got %q", out)
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
		out := ansi.Strip(repoDetailAlerts(&github.RepoDetail{AlertsAccess: github.AccessTokenLacks}, 110))
		for _, want := range []string{"Dependabot alerts (read)", "security_events"} {
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
