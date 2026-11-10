package ui

import (
	"errors"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/gfazioli/octoscope/internal/github"
)

// dashboardSurfaces is everything that draws on the dashboard and takes
// the keyboard while it is open: the settings panel, the action menu,
// every drill-in, the scan, and a list tab's filter box. None of them is
// drawn on the launch or the error screen.
var dashboardSurfaces = []struct {
	name string
	open func(t *testing.T, m Model) Model
}{
	{"settings panel", func(t *testing.T, m Model) Model {
		u, _ := m.Update(key(","))
		return u.(Model)
	}},
	{"action menu", func(t *testing.T, m Model) Model {
		m.actionMenu = ActionMenuModel{}.Open("Actions", []Action{{Label: "Open in GitHub", Shortcut: 'o'}})
		return m
	}},
	{"repo drill-in", func(t *testing.T, m Model) Model {
		m.repoDetail = m.repoDetail.Open(github.Repo{URL: "https://github.com/octocat/hello"}, StarModeDensity)
		return m
	}},
	{"PR drill-in", func(t *testing.T, m Model) Model {
		m.prDetail = m.prDetail.Open(github.PullRequest{URL: "https://github.com/octocat/hello/pull/1"})
		return m
	}},
	{"gist drill-in", func(t *testing.T, m Model) Model {
		m.gistDetail = m.gistDetail.Open(github.Gist{Name: "abc", URL: "https://gist.github.com/octocat/abc"})
		return m
	}},
	{"issue drill-in", func(t *testing.T, m Model) Model {
		m.issueDetail = m.issueDetail.Open(github.Issue{URL: "https://github.com/octocat/hello/issues/1"})
		return m
	}},
	{"scan", func(t *testing.T, m Model) Model {
		m.scan = m.scan.Open(github.Repo{URL: "https://github.com/octocat/hello"}, nil, "")
		return m
	}},
	{"Repos filter box", func(t *testing.T, m Model) Model {
		u, _ := m.Update(key("2"))
		u, _ = u.(Model).Update(key("/"))
		m = u.(Model)
		if !m.listInputMode() {
			t.Fatal("precondition: '/' on the Repos tab enters filter input")
		}
		return m
	}},
	// The other tabs' filter boxes, put straight into input mode.
	{"PRs filter box", func(t *testing.T, m Model) Model {
		m.activeTab, m.prs.searchActive = TabPRs, true
		return m
	}},
	{"Issues filter box", func(t *testing.T, m Model) Model {
		m.activeTab, m.issues.searchActive = TabIssues, true
		return m
	}},
	{"Gists filter box", func(t *testing.T, m Model) Model {
		m.activeTab, m.gists.searchActive = TabGists, true
		return m
	}},
	{"Inbox filter box", func(t *testing.T, m Model) Model {
		m.activeTab, m.inbox.searchActive = TabInbox, true
		return m
	}},
	{"Activity feed filter box", func(t *testing.T, m Model) Model {
		m.activeTab, m.activitySub, m.feed.searchActive = TabActivity, ActivitySubFeed, true
		return m
	}},
}

// holdsTheKeyboard reports whether any dashboard surface is still open.
func holdsTheKeyboard(m Model) bool {
	return m.settings.IsOpen() || m.actionMenu.IsOpen() || m.repoDetail.IsOpen() ||
		m.prDetail.IsOpen() || m.gistDetail.IsOpen() || m.issueDetail.IsOpen() ||
		m.scan.IsOpen() || m.listInputMode()
}

// onTheErrorScreen is a dashboard with surface open that a rejected
// token then took down: the error screen is what is drawn.
func onTheErrorScreen(t *testing.T, open func(*testing.T, Model) Model) Model {
	t.Helper()
	m := open(t, loadedModel(t))
	if !holdsTheKeyboard(m) {
		t.Fatal("precondition: the surface did not open")
	}
	rejected := &github.FetchError{Reason: github.ReasonAuth, Err: errors.New("401 Bad credentials")}
	u, _ := m.Update(fetchMsg{err: rejected, at: time.Now()})
	m = u.(Model)
	view := ansi.Strip(m.View())
	if m.stats != nil || !strings.Contains(view, "retry") || !strings.Contains(view, "quit") {
		t.Fatalf("precondition: want the error screen advertising r retry · q quit, got:\n%s", view)
	}
	return m
}

// TestErrorScreenKeysReachTheErrorScreen pins #239: the error screen
// advertises `r retry · q quit`, and both do exactly that whatever was
// open on the dashboard when it went away — one dashboard fetch for r,
// a quit for q — instead of being taken by a surface nobody can see.
func TestErrorScreenKeysReachTheErrorScreen(t *testing.T) {
	recordTicks(t)
	for _, s := range dashboardSurfaces {
		t.Run(s.name+"/r retries", func(t *testing.T) {
			fetches := recordFetches(t)
			m := onTheErrorScreen(t, s.open)
			u, _ := m.Update(key("r"))
			got := u.(Model)
			if *fetches != 1 || !got.loading {
				t.Errorf("r dispatched %d dashboard fetches, loading=%v; want one running", *fetches, got.loading)
			}
			if holdsTheKeyboard(got) {
				t.Error("the invisible surface is still holding the keyboard")
			}
		})
		t.Run(s.name+"/q quits", func(t *testing.T) {
			recordFetches(t)
			m := onTheErrorScreen(t, s.open)
			_, cmd := m.Update(key("q"))
			if cmd == nil {
				t.Fatal("q returned no command")
			}
			if _, ok := cmd().(tea.QuitMsg); !ok {
				t.Errorf("q did not quit: got %T", cmd())
			}
		})
	}
}

// TestSurfacesOpenedWithNoDashboardDoNotKeepTheKeys: the same from the
// other side. On the first-load error screen `,` and `/` still reach the
// handlers that open the settings panel and a filter box, neither of
// which is drawn there; the next key must not be theirs.
func TestSurfacesOpenedWithNoDashboardDoNotKeepTheKeys(t *testing.T) {
	recordTicks(t)
	for _, opener := range []string{",", "/"} {
		t.Run(opener+" then r", func(t *testing.T) {
			fetches := recordFetches(t)
			m := newPlainModel(t)
			u, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
			u, _ = u.(Model).Update(key("2")) // a list tab, so `/` has a box to open
			u, _ = u.(Model).Update(fetchMsg{err: networkError(), at: time.Now()})
			m = u.(Model)
			if m.stats != nil || m.loading {
				t.Fatal("precondition: a first fetch that failed leaves the error screen")
			}
			u, _ = m.Update(key(opener))
			u, _ = u.(Model).Update(key("r"))
			got := u.(Model)
			if *fetches != 1 || !got.loading {
				t.Errorf("%s then r dispatched %d dashboard fetches, loading=%v; want one running", opener, *fetches, got.loading)
			}
		})
	}
}

// TestDashboardSurfacesKeepTheirKeysOnTheDashboard is the guard's other
// half: with the dashboard on screen every surface still owns `r` — no
// dashboard fetch — exactly as before.
func TestDashboardSurfacesKeepTheirKeysOnTheDashboard(t *testing.T) {
	recordTicks(t)
	for _, s := range dashboardSurfaces {
		t.Run(s.name, func(t *testing.T) {
			fetches := recordFetches(t)
			m := s.open(t, loadedModel(t))
			u, _ := m.Update(key("r"))
			if *fetches != 0 {
				t.Errorf("r under an open %s dispatched %d dashboard fetches, want none", s.name, *fetches)
			}
			if !holdsTheKeyboard(u.(Model)) {
				t.Errorf("r closed the %s", s.name)
			}
		})
	}
}
