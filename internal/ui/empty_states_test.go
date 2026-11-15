package ui

import (
	"fmt"
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
		// The affordance must name the active query — that's the point.
		if !strings.Contains(out, "zzz-no-match") {
			t.Errorf("filtered-empty message should name the query, got:\n%s", out)
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
		// The affordance must name the active query — that's the point.
		if !strings.Contains(out, "zzz-no-match") {
			t.Errorf("filtered-empty message should name the query, got:\n%s", out)
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

// reviewRequestsNoteText is the start of the line the PRs tab adds when
// the review-request search failed (#240), short enough to survive the
// truncation at a narrow width.
const reviewRequestsNoteText = "Couldn't ask GitHub"

// TestPRsTabSaysWhenReviewRequestsCouldNotBeAsked: a failed review-request
// search shows its line in every shape the tab can take, and never when
// the search answered, empty answer included (#240).
func TestPRsTabSaysWhenReviewRequestsCouldNotBeAsked(t *testing.T) {
	_ = applyTheme("octoscope", "")
	authored := []github.PullRequest{{Number: 1, Title: "add feature", Repo: "me/app"}}

	for _, tc := range []struct {
		name  string
		pm    PRsModel
		stats github.Stats
		want  bool
	}{
		{"no pull requests at all", PRsModel{}, github.Stats{ReviewRequestsUnavailable: true}, true},
		{"authored pull requests", PRsModel{}, github.Stats{OpenPullRequests: authored, ReviewRequestsUnavailable: true}, true},
		{"a filter that matches nothing", PRsModel{query: "zzz-no-match"}, github.Stats{OpenPullRequests: authored, ReviewRequestsUnavailable: true}, true},
		{"a search that answered", PRsModel{}, github.Stats{OpenPullRequests: authored}, false},
		{"a search that answered with nothing", PRsModel{}, github.Stats{}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out := ansi.Strip(tc.pm.renderPRsTab(&tc.stats, 120, 40))
			if got := strings.Contains(out, reviewRequestsNoteText); got != tc.want {
				t.Errorf("note shown = %v, want %v:\n%s", got, tc.want, out)
			}
		})
	}
}

// The note is one line at any width and takes its row from the table, so
// a tab that overflows is exactly as tall with it as without it.
func TestPRsTabReviewRequestsNoteKeepsTheHeight(t *testing.T) {
	_ = applyTheme("octoscope", "")
	var many []github.PullRequest
	for i := 1; i <= 40; i++ {
		many = append(many, github.PullRequest{Number: i, Title: fmt.Sprintf("pull request %d", i), Repo: "me/app"})
	}
	for _, width := range []int{40, 120} {
		t.Run(fmt.Sprint(width), func(t *testing.T) {
			answered := github.Stats{OpenPullRequests: many}
			failed := answered
			failed.ReviewRequestsUnavailable = true
			without := ansi.Strip(PRsModel{}.renderPRsTab(&answered, width, 20))
			with := ansi.Strip(PRsModel{}.renderPRsTab(&failed, width, 20))
			if a, b := strings.Count(with, "\n"), strings.Count(without, "\n"); a != b {
				t.Errorf("%d lines with the note, %d without", a+1, b+1)
			}
			found := false
			for _, line := range strings.Split(with, "\n") {
				if !strings.Contains(line, reviewRequestsNoteText) {
					continue
				}
				found = true
				if w := cellWidth(line); w > width {
					t.Errorf("note is %d cells wide at width %d: %q", w, width, line)
				}
			}
			if !found {
				t.Fatalf("no note in:\n%s", with)
			}
		})
	}
}
