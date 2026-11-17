package ui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/gfazioli/octoscope/internal/github"
)

// #268. A refresh that returns fewer rows leaves a list tab's cursor past
// the new end. The renderer and the selection accessor clamp it to the
// last row, so that row is drawn as selected; Update has to clamp too, or
// enter / d / o / c / P see an index that no longer exists and do nothing
// until the cursor moves, and the first up steps from the stale index.
//
// Driven through the root model the way the issue measured it: a fetch,
// the tab's digit, G to the last row, a second fetch with fewer rows,
// then the key. Two rows survive the refresh on purpose — with one left,
// every cursor value renders on it and the stale step of `up` would be
// invisible (see TestFeedCursorSurvivesTheRowSetShrinking).
func TestListTabsClampTheCursorWhenARefreshShortensTheList(t *testing.T) {
	now := time.Now()
	ago := func(i int) time.Time { return now.Add(-time.Duration(i) * time.Minute) }
	viewer := func(s github.Stats) *github.Stats {
		s.Login, s.Authenticated, s.IsViewer = "octocat", true, true
		return &s
	}

	pr := func(title string, i int, reviewer bool) github.PullRequest {
		p := github.PullRequest{Number: 100 + i, Title: title, Repo: "octocat/app", URL: "https://github.com/octocat/app/pull/" + itoa(100+i), UpdatedAt: ago(i)}
		if reviewer {
			p.AuthorLogin = "someone"
		}
		return p
	}
	repo := func(name string, i int) github.Repo {
		return github.Repo{Name: name, URL: "https://github.com/octocat/" + name, PushedAt: ago(i)}
	}
	issue := func(title string, i int) github.Issue {
		return github.Issue{Number: 200 + i, Title: title, Repo: "octocat/app", URL: "https://github.com/octocat/app/issues/" + itoa(200+i), UpdatedAt: ago(i)}
	}
	gistAt := func(desc string, i int) github.Gist {
		return gist(desc, nil, 0, true, time.Duration(i)*time.Minute)
	}

	// Every label starts with the subtest's name and a dash: that is how
	// drawnSelection tells this test's rows from the rest of the screen.
	for _, tc := range []struct {
		name          string
		digit         string
		before, after *github.Stats
		// The refreshed list's first and last rows, as drawn.
		first, last string
		// opened names the row the command of enter acts on.
		opened func(tea.Msg) string
	}{
		{
			// The issue's own case: review requests answered elsewhere,
			// the authored pull request the cursor sat below stays.
			name:  "pr",
			digit: "3",
			before: viewer(github.Stats{
				ReviewRequests: []github.PullRequest{
					pr("pr-review-alpha", 1, true), pr("pr-review-bravo", 2, true),
					pr("pr-review-charlie", 3, true), pr("pr-review-delta", 4, true),
				},
				OpenPullRequests: []github.PullRequest{pr("pr-mine-echo", 5, false)},
			}),
			after: viewer(github.Stats{
				ReviewRequests:   []github.PullRequest{pr("pr-review-alpha", 1, true)},
				OpenPullRequests: []github.PullRequest{pr("pr-mine-echo", 5, false)},
			}),
			first:  "pr-review-alpha",
			last:   "pr-mine-echo",
			opened: func(m tea.Msg) string { return m.(viewPRDetailMsg).pr.Title },
		},
		{
			name:  "repo",
			digit: "2",
			before: viewer(github.Stats{Repositories: []github.Repo{
				repo("repo-alpha", 1), repo("repo-bravo", 2), repo("repo-charlie", 3),
				repo("repo-delta", 4), repo("repo-echo", 5),
			}}),
			after:  viewer(github.Stats{Repositories: []github.Repo{repo("repo-alpha", 1), repo("repo-bravo", 2)}}),
			first:  "repo-alpha",
			last:   "repo-bravo",
			opened: func(m tea.Msg) string { return m.(viewRepoDetailMsg).repo.Name },
		},
		{
			name:  "issue",
			digit: "4",
			before: viewer(github.Stats{OpenIssuesList: []github.Issue{
				issue("issue-alpha", 1), issue("issue-bravo", 2), issue("issue-charlie", 3),
				issue("issue-delta", 4), issue("issue-echo", 5),
			}}),
			after:  viewer(github.Stats{OpenIssuesList: []github.Issue{issue("issue-alpha", 1), issue("issue-bravo", 2)}}),
			first:  "issue-alpha",
			last:   "issue-bravo",
			opened: func(m tea.Msg) string { return m.(viewIssueDetailMsg).issue.Title },
		},
		{
			name:  "gist",
			digit: "6",
			before: viewer(github.Stats{Gists: []github.Gist{
				gistAt("gist-alpha", 1), gistAt("gist-bravo", 2), gistAt("gist-charlie", 3),
				gistAt("gist-delta", 4), gistAt("gist-echo", 5),
			}}),
			after:  viewer(github.Stats{Gists: []github.Gist{gistAt("gist-alpha", 1), gistAt("gist-bravo", 2)}}),
			first:  "gist-alpha",
			last:   "gist-bravo",
			opened: func(m tea.Msg) string { return m.(viewGistDetailMsg).gist.Description },
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			drawn := func(m Model) string { return drawnSelection(m, tc.name+"-") }
			// enter must act on the row that is drawn, not on nothing.
			enterOpens := func(m Model, want string) {
				t.Helper()
				_, cmd := m.Update(key("enter"))
				if cmd == nil {
					t.Fatalf("enter returned no command with %q drawn as selected", want)
				}
				if got := tc.opened(cmd()); got != want {
					t.Errorf("enter opened %q, want the drawn row %q", got, want)
				}
			}

			m := newFeedRoutingModel(t)
			m = sendMsg(m, fetchMsg{stats: tc.before, at: now})
			m = sendMsg(m, key(tc.digit))
			m = sendMsg(m, key("G"))
			if got := drawn(m); !strings.HasSuffix(got, "-echo") || strings.Contains(got, ",") {
				t.Fatalf("G drew %q as selected, want the last row of five", got)
			}

			m = sendMsg(m, fetchMsg{stats: tc.after, at: now.Add(time.Minute)})
			if got := drawn(m); got != tc.last {
				t.Fatalf("after the refresh %q is drawn as selected, want %q", got, tc.last)
			}
			enterOpens(m, tc.last)

			// up steps from the drawn row, not from the stale index.
			if got := drawn(sendMsg(m, key("up"))); got != tc.first {
				t.Errorf("up drew %q as selected, want %q", got, tc.first)
			}

			// The other half of the clamp: a refresh that empties the
			// list, a key pressed on it, then rows again. The cursor must
			// not be left at -1 for enter to index with.
			m = sendMsg(m, fetchMsg{stats: viewer(github.Stats{}), at: now.Add(2 * time.Minute)})
			m = sendMsg(m, key("down"))
			m = sendMsg(m, fetchMsg{stats: tc.after, at: now.Add(3 * time.Minute)})
			if got := drawn(m); got != tc.first {
				t.Fatalf("after emptying and refilling %q is drawn as selected, want %q", got, tc.first)
			}
			enterOpens(m, tc.first)
		})
	}
}

func sendMsg(m Model, msg tea.Msg) Model {
	next, _ := m.Update(msg)
	return next.(Model)
}

// drawnSelection returns the labels starting with prefix on the lines of
// the rendered screen that carry the cursor marker: one label when
// exactly one row is drawn as selected.
func drawnSelection(m Model, prefix string) string {
	var found []string
	for _, line := range strings.Split(ansi.Strip(m.View()), "\n") {
		if !strings.Contains(line, "▸ ") {
			continue
		}
		for _, f := range strings.Fields(line) {
			if strings.HasPrefix(f, prefix) {
				found = append(found, f)
			}
		}
	}
	return strings.Join(found, ",")
}
