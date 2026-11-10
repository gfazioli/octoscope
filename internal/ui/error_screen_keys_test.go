package ui

import (
	"errors"
	"os"
	"path/filepath"
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
// drawn on the error screen.
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

// holdsTheKeyboard reports whether any dashboard surface is open.
func holdsTheKeyboard(m Model) bool {
	return m.settings.IsOpen() || m.actionMenu.IsOpen() || m.repoDetail.IsOpen() ||
		m.prDetail.IsOpen() || m.gistDetail.IsOpen() || m.issueDetail.IsOpen() ||
		m.scan.IsOpen() || m.listInputMode()
}

func rejectedToken() error {
	return &github.FetchError{Reason: github.ReasonAuth, Err: errors.New("401 Bad credentials")}
}

// onTheErrorScreen is a dashboard with a surface open that a rejected
// token then took down: the error screen is what is drawn.
func onTheErrorScreen(t *testing.T, open func(*testing.T, Model) Model) Model {
	t.Helper()
	m := open(t, loadedModel(t))
	if !holdsTheKeyboard(m) {
		t.Fatal("precondition: the surface did not open")
	}
	u, _ := m.Update(fetchMsg{err: rejectedToken(), at: time.Now()})
	m = u.(Model)
	view := ansi.Strip(m.View())
	if m.stats != nil || !strings.Contains(view, "retry") || !strings.Contains(view, "quit") {
		t.Fatalf("precondition: want the error screen advertising r retry · q quit, got:\n%s", view)
	}
	return m
}

// namedKey is key, for the three more names this file presses that key
// would spell out as typed text.
func namedKey(s string) tea.KeyMsg {
	switch s {
	case "tab":
		return tea.KeyMsg{Type: tea.KeyTab}
	case "shift+tab":
		return tea.KeyMsg{Type: tea.KeyShiftTab}
	case " ":
		return tea.KeyMsg{Type: tea.KeySpace}
	}
	return key(s)
}

func quits(cmd tea.Cmd) bool {
	if cmd == nil {
		return false
	}
	_, ok := cmd().(tea.QuitMsg)
	return ok
}

// TestErrorScreenKeysReachTheErrorScreen pins #239: the error screen
// advertises `r retry · q quit`, and both do exactly that whatever was
// open on the dashboard when it went away — one dashboard fetch for r,
// a quit for q — instead of being taken by a surface nobody can see.
// The same while the retry runs, when the launch screen stands in and
// still says `q quit`. What was open waits underneath: when the
// dashboard comes back it is there, with its keys.
func TestErrorScreenKeysReachTheErrorScreen(t *testing.T) {
	recordTicks(t)
	for _, s := range dashboardSurfaces {
		t.Run(s.name+"/r retries", func(t *testing.T) {
			fetches := recordFetches(t)
			m := onTheErrorScreen(t, s.open)
			u, _ := m.Update(key("r"))
			retrying := u.(Model)
			if *fetches != 1 || !retrying.loading {
				t.Fatalf("r dispatched %d dashboard fetches, loading=%v; want one running", *fetches, retrying.loading)
			}
			// A second r while it runs starts nothing more.
			u, _ = retrying.Update(key("r"))
			if *fetches != 1 {
				t.Errorf("r during the retry dispatched a second fetch (%d)", *fetches)
			}
			// The launch screen stands in for the retry, and says q quit.
			if view := ansi.Strip(retrying.View()); !strings.Contains(view, "Loading…") || !strings.Contains(view, "quit") {
				t.Fatalf("precondition: want the launch screen during the retry, got:\n%s", view)
			}
			if _, cmd := retrying.Update(key("q")); !quits(cmd) {
				t.Error("q during the retry did not quit")
			}
			// The retry succeeds: the surface is still there, and r is
			// its own again.
			u, _ = retrying.Update(fetchMsg{stats: &github.Stats{}, at: time.Now()})
			back := u.(Model)
			if !holdsTheKeyboard(back) {
				t.Fatalf("the %s was gone when the dashboard came back", s.name)
			}
			before := *fetches
			back.Update(key("r"))
			if *fetches != before {
				t.Errorf("back on the dashboard, r under the open %s dispatched a dashboard fetch", s.name)
			}
		})
		t.Run(s.name+"/q quits", func(t *testing.T) {
			recordFetches(t)
			m := onTheErrorScreen(t, s.open)
			if _, cmd := m.Update(key("q")); !quits(cmd) {
				t.Error("q did not quit")
			}
		})
	}
}

// firstLoadErrorScreen is the error screen of a launch whose first fetch
// failed, on a list tab so that `/` would have a filter box to open.
func firstLoadErrorScreen(t *testing.T, configPath string) Model {
	t.Helper()
	t.Setenv("GITHUB_TOKEN", "test-token-not-used")
	_ = applyTheme("octoscope", "")
	client, err := github.New("octocat", github.Options{})
	if err != nil {
		t.Fatalf("github.New: %v", err)
	}
	m := NewModel(client, "0.16.0", Options{ConfigPath: configPath})
	u, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	u, _ = u.(Model).Update(key("2")) // typed ahead of the first dashboard: the Repos tab
	u, _ = u.(Model).Update(fetchMsg{err: networkError(), at: time.Now()})
	m = u.(Model)
	if m.stats != nil || m.loading || m.activeTab != TabRepos {
		t.Fatal("precondition: a first fetch that failed leaves the error screen, on the Repos tab")
	}
	return m
}

// TestErrorScreenTakesOnlyTheKeysItShows: every other key does nothing
// there. None opens a surface the screen cannot draw, switches a tab
// nobody can see, or writes the config file; and after any of them r
// still retries.
func TestErrorScreenTakesOnlyTheKeysItShows(t *testing.T) {
	recordTicks(t)
	for _, k := range []string{",", "/", "p", "1", "3", "tab", "shift+tab", "esc", "enter", " ", "s", "w", "o", "d", "P", "j", "down", "x"} {
		t.Run(k, func(t *testing.T) {
			fetches := recordFetches(t)
			path := filepath.Join(t.TempDir(), "config.toml")
			m := firstLoadErrorScreen(t, path)
			u, cmd := m.Update(namedKey(k))
			got := u.(Model)
			if cmd != nil {
				t.Errorf("%q returned a command", k)
			}
			if holdsTheKeyboard(got) {
				t.Errorf("%q opened something the error screen does not draw", k)
			}
			if got.activeTab != m.activeTab || got.client.PublicOnly() != m.client.PublicOnly() || got.repos.sort != m.repos.sort {
				t.Errorf("%q changed state behind the error screen: tab %v→%v", k, m.activeTab, got.activeTab)
			}
			if _, err := os.Stat(path); err == nil {
				t.Errorf("%q wrote the config file", k)
			}
			if *fetches != 0 {
				t.Errorf("%q dispatched a fetch", k)
			}
			u, _ = got.Update(key("r"))
			if *fetches != 1 || !u.(Model).loading {
				t.Errorf("%q then r dispatched %d dashboard fetches; want one running", k, *fetches)
			}
		})
	}
}

// TestErrorScreenStillOpensWhatItCanDraw: help and the rate-limit panel
// are drawn over the error screen, so ? and % keep opening them, and
// once open they keep their own keys.
func TestErrorScreenStillOpensWhatItCanDraw(t *testing.T) {
	recordTicks(t)
	fetches := recordFetches(t)
	m := firstLoadErrorScreen(t, "")

	u, _ := m.Update(key("?"))
	if !u.(Model).help.IsOpen() {
		t.Error("? did not open help on the error screen")
	}
	u, _ = u.(Model).Update(key("r")) // any key dismisses help, r included
	if u.(Model).help.IsOpen() || *fetches != 0 {
		t.Errorf("r over help: open=%v fetches=%d, want it dismissed and no fetch", u.(Model).help.IsOpen(), *fetches)
	}

	u, cmd := m.Update(key("%"))
	if !u.(Model).rateLimits.IsOpen() || cmd == nil {
		t.Error("% did not open the rate-limit panel on the error screen")
	}
	if _, cmd := m.Update(key("ctrl+c")); !quits(cmd) {
		t.Error("ctrl+c did not quit on the error screen")
	}
}

// coldLaunch is octoscope before its first dashboard: loading, nothing
// failed, the launch screen on screen with its one key, q quit.
func coldLaunch(t *testing.T, configPath string) Model {
	t.Helper()
	t.Setenv("GITHUB_TOKEN", "test-token-not-used")
	_ = applyTheme("octoscope", "")
	client, err := github.New("octocat", github.Options{})
	if err != nil {
		t.Fatalf("github.New: %v", err)
	}
	m := NewModel(client, "0.16.0", Options{ConfigPath: configPath})
	u, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m = u.(Model)
	if !m.loading || m.stats != nil || m.failedWithoutDashboard() {
		t.Fatal("precondition: a cold launch is loading, with nothing failed")
	}
	if !strings.Contains(ansi.Strip(m.View()), "q quit") {
		t.Fatal("precondition: the launch screen shows q quit")
	}
	return m
}

// TestLaunchScreenTakesOnlyTheKeysItShows pins #261: before the first
// dashboard the launch screen shows one key, q quit, and nothing that
// draws on the dashboard can be seen. `,` used to open the settings
// panel behind it and `/` a filter box, which then took the q; `p`
// switched public-only and wrote the config. Now none of them does
// anything until there is a dashboard, and q quits after any of them.
func TestLaunchScreenTakesOnlyTheKeysItShows(t *testing.T) {
	recordTicks(t)
	for _, k := range []string{",", "/", "p", "r", "esc", "enter", " ", "s", "w", "o", "d", "P", "j", "down", "x"} {
		t.Run(k, func(t *testing.T) {
			fetches := recordFetches(t)
			path := filepath.Join(t.TempDir(), "config.toml")
			m := coldLaunch(t, path)
			// Read before the key: the client is shared by both models.
			publicOnly := m.client.PublicOnly()
			u, cmd := m.Update(namedKey(k))
			got := u.(Model)
			if cmd != nil {
				t.Errorf("%q returned a command", k)
			}
			if holdsTheKeyboard(got) {
				t.Errorf("%q opened something the launch screen does not draw", k)
			}
			if got.activeTab != m.activeTab || got.client.PublicOnly() != publicOnly || got.repos.sort != m.repos.sort {
				t.Errorf("%q changed state behind the launch screen", k)
			}
			if _, err := os.Stat(path); err == nil {
				t.Errorf("%q wrote the config file", k)
			}
			if *fetches != 0 {
				t.Errorf("%q dispatched a fetch", k)
			}
			if _, cmd := got.Update(key("q")); !quits(cmd) {
				t.Errorf("%q then q did not quit", k)
			}
		})
	}
}

// TestLaunchScreenKeepsATabChosenAhead: a dashboard is on its way, so a
// tab picked before it arrives is where it opens; and the screen's other
// keys are the ones it can act on: the two overlays it draws, and quit.
func TestLaunchScreenKeepsATabChosenAhead(t *testing.T) {
	recordTicks(t)
	recordFetches(t)
	m := coldLaunch(t, "")
	for _, c := range []struct {
		key  string
		want Tab
	}{{"2", TabRepos}, {"tab", TabPRs}, {"shift+tab", TabRepos}, {"8", TabWhatsNew}, {"1", TabOverview}} {
		u, _ := m.Update(namedKey(c.key))
		m = u.(Model)
		if m.activeTab != c.want {
			t.Errorf("%q typed ahead: on tab %v, want %v", c.key, m.activeTab, c.want)
		}
		if holdsTheKeyboard(m) {
			t.Errorf("%q opened something the launch screen does not draw", c.key)
		}
	}
	u, _ := m.Update(key("2"))
	// On a list tab `/` is the filter box, which the screen cannot draw.
	u, cmd := u.(Model).Update(key("/"))
	if cmd != nil || holdsTheKeyboard(u.(Model)) {
		t.Error("/ on a list tab chosen ahead opened its filter box behind the launch screen")
	}
	if _, cmd := u.(Model).Update(key("q")); !quits(cmd) {
		t.Error("q after a tab chosen ahead did not quit")
	}
	u, _ = u.(Model).Update(fetchMsg{stats: &github.Stats{}, at: time.Now()})
	if got := u.(Model); got.stats == nil || got.activeTab != TabRepos {
		t.Errorf("the dashboard arrived on tab %v, want the Repos tab chosen ahead", got.activeTab)
	}
	// A tab, not what is inside it: the Activity tab's own arrows wait.
	u, _ = m.Update(key("5"))
	u, cmd = u.(Model).Update(key("right"))
	if got := u.(Model); cmd != nil || got.activeTab != TabActivity || got.activitySub != m.activitySub {
		t.Errorf("→ on the Activity tab chosen ahead: tab %v, sub-tab %v, want Activity on its first half and no command", got.activeTab, got.activitySub)
	}

	u, _ = m.Update(key("?"))
	if !u.(Model).help.IsOpen() {
		t.Error("? did not open help on the launch screen")
	}
	u, cmd = m.Update(key("%"))
	if !u.(Model).rateLimits.IsOpen() || cmd == nil {
		t.Error("% did not open the rate-limit panel on the launch screen")
	}
	if _, cmd := m.Update(key("ctrl+c")); !quits(cmd) {
		t.Error("ctrl+c did not quit on the launch screen")
	}
}

// TestLaunchScreenGivesTheKeysBackWithTheDashboard is the control: the
// same keys do what they always did once there is a dashboard to draw on.
func TestLaunchScreenGivesTheKeysBackWithTheDashboard(t *testing.T) {
	recordTicks(t)
	recordFetches(t)
	path := filepath.Join(t.TempDir(), "config.toml")
	m := coldLaunch(t, path)
	u, _ := m.Update(key(","))
	u, _ = u.(Model).Update(fetchMsg{stats: &github.Stats{}, at: time.Now()})
	loaded := u.(Model)
	if holdsTheKeyboard(loaded) {
		t.Fatal("a key pressed on the launch screen left something open for the dashboard")
	}
	if u, _ := loaded.Update(key(",")); !u.(Model).settings.IsOpen() {
		t.Error(", does not open the settings panel on the dashboard")
	}
	u, _ = loaded.Update(key("2"))
	if u, _ = u.(Model).Update(key("/")); !u.(Model).listInputMode() {
		t.Error("/ does not open the Repos filter box on the dashboard")
	}
	if u, _ := loaded.Update(key("p")); !u.(Model).client.PublicOnly() {
		t.Error("p does not switch public-only on the dashboard")
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("p on the dashboard did not write the config: %v", err)
	}
}

// TestDashboardSurfacesKeepTheirKeysOnTheDashboard is the rule's other
// edge: with the dashboard on screen every surface still owns `r` — no
// dashboard fetch — exactly as before, also while a failed refresh has
// left the old dashboard up with its footer saying so.
func TestDashboardSurfacesKeepTheirKeysOnTheDashboard(t *testing.T) {
	recordTicks(t)
	for _, s := range dashboardSurfaces {
		t.Run(s.name, func(t *testing.T) {
			fetches := recordFetches(t)
			m := s.open(t, loadedModel(t))
			u, _ := m.Update(key("r"))
			if *fetches != 0 || !holdsTheKeyboard(u.(Model)) {
				t.Errorf("r under an open %s: %d dashboard fetches, still open=%v", s.name, *fetches, holdsTheKeyboard(u.(Model)))
			}
			// A refresh that failed for the network keeps the dashboard.
			u, _ = m.Update(fetchMsg{err: networkError(), at: time.Now()})
			stale := u.(Model)
			if stale.stats == nil {
				t.Fatal("precondition: a network failure keeps the dashboard on screen")
			}
			u, _ = stale.Update(key("r"))
			if *fetches != 0 || !holdsTheKeyboard(u.(Model)) {
				t.Errorf("r under an open %s on a stale dashboard: %d dashboard fetches, still open=%v", s.name, *fetches, holdsTheKeyboard(u.(Model)))
			}
		})
	}
}
