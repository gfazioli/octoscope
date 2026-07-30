package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/gfazioli/octoscope/internal/github"
)

// TestPRsTabFilteredEmpty pins #64 for the PRs tab: PRs exist but the
// active filter hides them all, so the tab shows the esc-to-clear
// affordance naming the query — not a blank table — and distinct from
// the genuinely-empty "no PRs authored" case.
func TestPRsTabFilteredEmpty(t *testing.T) {
	_ = applyTheme("octoscope", "")

	stats := &github.Stats{
		OpenPullRequests: []github.PullRequest{
			{Number: 1, Title: "add feature", Repo: "me/app"},
		},
	}

	t.Run("filter matches nothing", func(t *testing.T) {
		pm := PRsModel{query: "zzz-no-match"}
		out := ansi.Strip(pm.renderPRsTab(stats, 120, 40))
		if !strings.Contains(out, "no pull requests match") || !strings.Contains(out, "esc to clear") {
			t.Errorf("want filtered-empty affordance, got:\n%s", out)
		}
	})

	t.Run("genuinely empty reads differently", func(t *testing.T) {
		out := ansi.Strip(PRsModel{}.renderPRsTab(&github.Stats{}, 120, 40))
		if !strings.Contains(out, "no open pull requests you authored") {
			t.Errorf("want genuinely-empty message, got:\n%s", out)
		}
		if strings.Contains(out, "esc to clear") {
			t.Errorf("genuinely-empty must not show the filter affordance:\n%s", out)
		}
	})
}

// TestIssuesTabFilteredEmpty pins #64 for the Issues tab (same contract).
func TestIssuesTabFilteredEmpty(t *testing.T) {
	_ = applyTheme("octoscope", "")

	stats := &github.Stats{OpenIssuesList: []github.Issue{mkIssue("me/app", 1)}}

	t.Run("filter matches nothing", func(t *testing.T) {
		im := IssuesModel{query: "zzz-no-match"}
		out := ansi.Strip(im.renderIssuesTab(stats, 120, 40, nil))
		if !strings.Contains(out, "no issues match") || !strings.Contains(out, "esc to clear") {
			t.Errorf("want filtered-empty affordance, got:\n%s", out)
		}
	})

	t.Run("genuinely empty reads differently", func(t *testing.T) {
		out := ansi.Strip(IssuesModel{}.renderIssuesTab(&github.Stats{}, 120, 40, nil))
		if !strings.Contains(out, "no open issues you authored") {
			t.Errorf("want genuinely-empty message, got:\n%s", out)
		}
		if strings.Contains(out, "esc to clear") {
			t.Errorf("genuinely-empty must not show the filter affordance:\n%s", out)
		}
	})
}
