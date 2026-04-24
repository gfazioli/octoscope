package ui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/gen2brain/beeep"
	"github.com/gfazioli/octoscope/internal/github"
)

// Tab identifies one of the top-level views. Values are stable so the
// key bindings ("1".."5") map cleanly to positions.
type Tab int

const (
	TabOverview Tab = iota
	TabRepos
	TabPRs
	TabIssues
	TabActivity
)

// tabCount is the number of tabs. Keep in sync with the Tab constants.
const tabCount = 5

// tabLabels is the visible name for each tab, indexed by Tab value.
var tabLabels = [tabCount]string{
	"Overview",
	"Repos",
	"PRs",
	"Issues",
	"Activity",
}

// Model is the top-level BubbleTea state. For the v0.1.0 MVP it's a
// single-screen dashboard; later phases add tabs / panels by nesting
// sub-models here rather than replacing this one.
type Model struct {
	client *github.Client
	stats  *github.Stats
	err    error

	loading   bool
	lastFetch time.Time

	width, height int

	interval time.Duration

	// version string shown in the banner and footer. Set by the
	// caller — keeps the UI package ignorant of the main package's
	// build-time constant.
	version string

	// spinner rotates one frame every ~100ms while a fetch is in
	// flight, so the footer gives visible feedback that we're not
	// stuck. Owned by the model so subsequent Updates can tick it.
	spinner spinner.Model

	// pulseMap tracks when each card's value last changed, keyed by
	// the card's stable id. The view uses it to apply the accent
	// border for pulseDuration seconds after a change.
	pulseMap map[string]time.Time

	// activeTab is the currently visible tab (0 = Overview). Switched
	// via number keys "1".."5" or Tab/Shift+Tab.
	activeTab Tab

	// repos holds the state for the Repos tab (cursor + sort order).
	// A dedicated sub-model keeps tab-specific state from bloating
	// this root struct — the pattern scales as PRs / Issues get their
	// own sub-models in follow-up releases.
	repos ReposModel
}

// fetchMsg carries the outcome of a FetchStats call back to the
// model's Update loop.
type fetchMsg struct {
	stats *github.Stats
	err   error
	at    time.Time
}

// tickMsg fires at `interval` and schedules the next auto-refresh.
type tickMsg time.Time

// clockTickMsg fires once a second just so the footer's "Updated Xs
// ago" label stays current. BubbleTea only re-renders when messages
// arrive, so without this the freshness clock would stay frozen at
// whatever value it showed at fetch time.
type clockTickMsg time.Time

// pulseExpireMsg fires once the pulse window elapses after a fetch
// that saw changes. Its only purpose is to force a redraw so the
// accent borders on "recently changed" cards revert to muted without
// waiting for the next auto-refresh tick (60s).
type pulseExpireMsg struct{}

// diffIDs maps a Stats field to the card id used by the view. Only
// the integer fields that appear as cards are tracked; profile-text
// fields (bio, location, …) are outside this feature for now.
var diffIDs = []struct {
	id  string
	get func(*github.Stats) int
}{
	{"followers", func(s *github.Stats) int { return s.Followers }},
	{"following", func(s *github.Stats) int { return s.Following }},
	{"stars", func(s *github.Stats) int { return s.TotalStars }},
	{"prs_authored", func(s *github.Stats) int { return s.PRsTotal }},
	{"prs_merged", func(s *github.Stats) int { return s.PRsMerged }},
	{"issues_authored", func(s *github.Stats) int { return s.IssuesAuthored }},
	{"commits_year", func(s *github.Stats) int { return s.CommitsLastYear }},
	{"public_repos", func(s *github.Stats) int { return s.PublicRepos }},
	{"forks_received", func(s *github.Stats) int { return s.ForksReceived }},
	{"open_issues", func(s *github.Stats) int { return s.OpenIssues }},
	{"open_prs", func(s *github.Stats) int { return s.OpenPRs }},
}

// changedCards returns the subset of card ids whose numeric value
// differs between old and new. Nil-safe on both sides — a first
// fetch (old == nil) returns nothing because nothing has "changed",
// it's establishing a baseline.
func changedCards(old, new *github.Stats) []string {
	if old == nil || new == nil {
		return nil
	}
	var out []string
	for _, d := range diffIDs {
		if d.get(old) != d.get(new) {
			out = append(out, d.id)
		}
	}
	return out
}

// notifyDeltas sends a system notification + beep summarising how
// Stars and/or Followers changed between old and new. No-op when
// neither of those two fields changed.
func notifyDeltas(old, new *github.Stats) tea.Cmd {
	if old == nil || new == nil {
		return nil
	}
	var parts []string
	if old.TotalStars != new.TotalStars {
		parts = append(parts, formatDelta("star", new.TotalStars-old.TotalStars))
	}
	if old.Followers != new.Followers {
		parts = append(parts, formatDelta("follower", new.Followers-old.Followers))
	}
	if len(parts) == 0 {
		return nil
	}
	msg := strings.Join(parts, " · ")
	who := new.Login
	if new.Name != "" {
		who = new.Name
	}
	return func() tea.Msg {
		_ = beeep.Notify("octoscope — "+who, msg, "")
		_ = beeep.Beep(beeep.DefaultFreq, beeep.DefaultDuration)
		return nil
	}
}

// formatDelta returns a human-readable "+2 stars" / "-1 follower"
// fragment. Singular/plural picked from the magnitude.
func formatDelta(noun string, delta int) string {
	if delta == 0 {
		return ""
	}
	abs := delta
	sign := "+"
	if delta < 0 {
		abs = -delta
		sign = "-"
	}
	if abs != 1 {
		noun += "s"
	}
	return fmt.Sprintf("%s%d %s", sign, abs, noun)
}

// NewModel returns a Model ready for tea.NewProgram. The first fetch
// is kicked off as an Init command so the UI renders a loading state
// immediately rather than waiting for the network.
func NewModel(client *github.Client, version string) Model {
	sp := spinner.New()
	sp.Spinner = spinner.MiniDot
	sp.Style = lipgloss.NewStyle().Foreground(colAccent)
	return Model{
		client:   client,
		loading:  true,
		interval: 60 * time.Second,
		version:  version,
		spinner:  sp,
		pulseMap: make(map[string]time.Time),
	}
}

// Init starts the first fetch, schedules the periodic tick, starts
// the 1-second clock that keeps the footer freshness label live,
// and kicks off the spinner animation — we're in the loading state
// on first paint so the spinner is already visible.
func (m Model) Init() tea.Cmd {
	return tea.Batch(
		fetchCmd(m.client),
		tickCmd(m.interval),
		clockTickCmd(),
		m.spinner.Tick,
	)
}

// Update routes keyboard, resize, and network messages.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case tea.KeyMsg:
		// When a sub-model is capturing text input (e.g. the Repos-tab
		// search box), give it the keystroke first so "q", "1"–"5",
		// "tab" etc. become literal characters instead of triggering
		// the global hotkeys. ctrl+c still quits regardless.
		if m.activeTab == TabRepos && m.repos.IsInputMode() && msg.String() != "ctrl+c" {
			var cmd tea.Cmd
			m.repos, cmd = m.repos.Update(msg, m.stats)
			return m, cmd
		}

		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit
		case "r":
			if !m.loading {
				m.loading = true
				// Restart the spinner tick alongside the fetch so the
				// animation begins immediately on user-triggered
				// refreshes too.
				return m, tea.Batch(fetchCmd(m.client), m.spinner.Tick)
			}
		case "tab", "shift+tab":
			if msg.String() == "tab" {
				m.activeTab = (m.activeTab + 1) % tabCount
			} else {
				m.activeTab = (m.activeTab - 1 + tabCount) % tabCount
			}
			return m, nil
		case "1", "2", "3", "4", "5":
			// Digit → zero-based tab index. Safe because len("1"..."5") == 1
			// and the range is bounded by tabCount via the case list.
			m.activeTab = Tab(msg.String()[0] - '1')
			return m, nil
		default:
			// Any other key is forwarded to the active tab's sub-model
			// (only Repos has one today). Global keys above have
			// already matched by this point.
			if m.activeTab == TabRepos {
				var cmd tea.Cmd
				m.repos, cmd = m.repos.Update(msg, m.stats)
				return m, cmd
			}
		}

	case fetchMsg:
		m.loading = false
		previous := m.stats
		m.stats = msg.stats
		m.err = msg.err
		m.lastFetch = msg.at

		// On a successful fetch that's not the first one, diff the
		// new stats against the previous snapshot. Fields that moved
		// get a pulse timestamp so the view highlights them, and
		// Stars/Followers additionally fire a system notification.
		if msg.err == nil && msg.stats != nil {
			changes := changedCards(previous, msg.stats)
			if len(changes) > 0 {
				now := time.Now()
				for _, id := range changes {
					m.pulseMap[id] = now
				}
				cmds := []tea.Cmd{
					tea.Tick(pulseDuration, func(t time.Time) tea.Msg {
						return pulseExpireMsg{}
					}),
				}
				if notify := notifyDeltas(previous, msg.stats); notify != nil {
					cmds = append(cmds, notify)
				}
				return m, tea.Batch(cmds...)
			}
		}

	case tickMsg:
		// Every `interval`, re-fetch and reschedule. Kicked off as a
		// batch so the next tick is enqueued even if fetchCmd takes a
		// while. Flip loading=true so the footer spinner shows.
		m.loading = true
		return m, tea.Batch(fetchCmd(m.client), tickCmd(m.interval), m.spinner.Tick)

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		// Only keep the animation running while a fetch is in
		// flight — otherwise the idle loop wastes CPU on redraws.
		if !m.loading {
			return m, nil
		}
		return m, cmd

	case pulseExpireMsg:
		// Returning (m, nil) forces a re-render; the view itself
		// checks time.Since(pulseMap[id]) so expired entries are
		// naturally ignored. We clean the map opportunistically
		// to keep it bounded.
		now := time.Now()
		for k, v := range m.pulseMap {
			if now.Sub(v) >= pulseDuration {
				delete(m.pulseMap, k)
			}
		}
		return m, nil

	case clockTickMsg:
		// Heartbeat that keeps the footer's "Updated Xs ago" label
		// current. Schedule the next tick and let the framework
		// re-render the view against the fresh time.Now().
		return m, clockTickCmd()
	}

	return m, nil
}

// fetchCmd runs FetchStats with a 10s timeout and packs the result in
// a fetchMsg. Returning a command rather than calling directly keeps
// the network off BubbleTea's synchronous update loop.
func fetchCmd(client *github.Client) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		stats, err := client.FetchStats(ctx)
		return fetchMsg{stats: stats, err: err, at: time.Now()}
	}
}

// tickCmd is just tea.Tick with a tickMsg envelope, for readability.
func tickCmd(d time.Duration) tea.Cmd {
	return tea.Tick(d, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

// clockTickCmd fires every second and does nothing other than cause
// a re-render — the "Updated Xs ago" label is computed against
// `time.Now()` at render time, so a periodic heartbeat is all we need
// to keep it live. Cheap: one redraw per second, zero allocations in
// the steady state.
func clockTickCmd() tea.Cmd {
	return tea.Tick(time.Second, func(t time.Time) tea.Msg {
		return clockTickMsg(t)
	})
}
