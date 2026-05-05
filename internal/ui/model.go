package ui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/gfazioli/octoscope/internal/config"
	"github.com/gfazioli/octoscope/internal/github"
	"github.com/gfazioli/octoscope/internal/notify"
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

	// errReason classifies the last fetch failure (auth / network /
	// rate-limit / server / unknown) so the footer can render an
	// actionable line instead of a generic "errored". Set alongside
	// err; zero-valued (ReasonUnknown) when err is nil.
	errReason github.FetchErrorReason

	// lastRateLimit is the most recent GraphQL budget snapshot seen.
	// Carried separately from stats so a failed refresh doesn't blank
	// the footer's rate readout — and so rate-limit errors have a
	// resetAt to back off against even when the failing fetch
	// returned no body.
	lastRateLimit *github.RateLimit

	loading   bool
	lastFetch time.Time

	width, height int

	interval time.Duration

	// compact toggles a denser card layout in the Overview tab:
	// smaller card width, abbreviated labels. Mutable via the
	// in-app settings panel (',' to open) — change applies on save
	// without restarting octoscope.
	compact bool

	// configPath is the file the in-app settings panel writes back
	// to on save. Empty when octoscope was launched with no usable
	// HOME / XDG_CONFIG_HOME — in that case the settings panel is
	// still usable but the changes don't persist past the session.
	configPath string

	// theme is the active theme name (e.g. "octoscope",
	// "stranger-things"). Mutable via the in-app settings panel; on
	// change, applyTheme rebuilds every dependent style and the
	// spinner's Foreground is reset to track the new accent.
	theme string

	// accentColor optionally overrides only the accent slot of the
	// active theme. Empty = no override. Persisted to config alongside
	// theme so launch-time and runtime sources stay in sync.
	accentColor string

	// settings holds the in-app settings form's transient state
	// (focused row, edit buffer, staged toggles). The panel is open
	// iff settings.IsOpen().
	settings SettingsModel

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

	// repos, prs, issues hold per-tab state (cursor / sort / search).
	// Sub-models keep tab-specific state from bloating this root
	// struct — each tab owns its own idioms (PRs has mergeable state,
	// Issues has no state column, Repos has language colours) and
	// Update dispatches to the sub-model of the active tab.
	repos  ReposModel
	prs    PRsModel
	issues IssuesModel

	// overviewVP / activityVP scroll the static-content tabs
	// vertically when the terminal is shorter than the rendered body.
	// Repos / PRs / Issues already paginate their own row lists, so
	// they don't need a viewport. The viewports are kept on the model
	// so YOffset persists across re-renders; width/height/SetContent
	// are refreshed before each scroll keystroke (see scroll.go).
	overviewVP viewport.Model
	activityVP viewport.Model
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
//
// Click-through routes by which signal fired: Followers deltas open
// the user's profile page, Stars deltas open their starred-repos tab
// (we don't know which specific repo got starred — that would need a
// per-repo diff over time). When both fire in the same refresh, the
// profile page is the safer landing.
func notifyDeltas(old, new *github.Stats) tea.Cmd {
	if old == nil || new == nil {
		return nil
	}
	var parts []string
	starsChanged := old.TotalStars != new.TotalStars
	followersChanged := old.Followers != new.Followers
	if starsChanged {
		parts = append(parts, formatDelta("star", new.TotalStars-old.TotalStars))
	}
	if followersChanged {
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

	clickURL := ""
	if new.Login != "" {
		switch {
		case followersChanged:
			clickURL = "https://github.com/" + new.Login
		case starsChanged:
			clickURL = "https://github.com/" + new.Login + "?tab=stars"
		}
	}

	return func() tea.Msg {
		_ = notify.Send("octoscope — "+who, msg, clickURL)
		_ = notify.Beep()
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

// Options carries the user-configurable knobs that shape a Model.
// Wired from main.go after merging the config file with CLI flags;
// the ui package itself stays oblivious to where the values came from.
type Options struct {
	// Interval is the auto-refresh period. Anything <= 0 is treated
	// as "use the package default" (60s) so callers can pass a zero
	// Options{} during tests.
	Interval time.Duration

	// Compact enables the dense card layout in the Overview tab.
	Compact bool

	// ConfigPath is the file the in-app settings panel writes back
	// to. Empty path = panel is still functional but changes won't
	// persist beyond the current session.
	ConfigPath string

	// Theme picks one of the built-in palettes (octoscope by default).
	// main.go has already validated the name against ui.IsValidTheme
	// before reaching here, so an unknown value is a programmer error.
	Theme string

	// AccentColor optionally overrides the active theme's Accent slot
	// only. Empty = no override.
	AccentColor string
}

// NewModel returns a Model ready for tea.NewProgram. The first fetch
// is kicked off as an Init command so the UI renders a loading state
// immediately rather than waiting for the network.
func NewModel(client *github.Client, version string, opts Options) Model {
	interval := opts.Interval
	if interval <= 0 {
		interval = 60 * time.Second
	}
	// Apply the chosen theme before constructing the spinner, so its
	// Foreground reads the theme's Accent and tracks subsequent
	// theme switches (the spinner's own Foreground is rebuilt in
	// applySettingsAndClose when the theme row changes).
	themeName := opts.Theme
	if themeName == "" {
		themeName = "octoscope"
	}
	_ = applyTheme(themeName, opts.AccentColor)

	sp := spinner.New()
	sp.Spinner = spinner.MiniDot
	sp.Style = lipgloss.NewStyle().Foreground(colAccent)
	return Model{
		client:      client,
		loading:     true,
		interval:    interval,
		compact:     opts.Compact,
		configPath:  opts.ConfigPath,
		theme:       themeName,
		accentColor: opts.AccentColor,
		version:     version,
		spinner:     sp,
		pulseMap:    make(map[string]time.Time),
		overviewVP:  viewport.New(0, 0),
		activityVP:  viewport.New(0, 0),
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
		// Re-sync the scroll viewports so the footer's "↑/↓ scroll"
		// hint and the viewport's overflow detection track the new
		// dimensions on the very next paint, not after the next
		// keystroke. No-op when stats haven't arrived yet.
		syncOverviewViewport(&m)
		syncActivityViewport(&m)
		return m, nil

	case tea.KeyMsg:
		// Settings modal absorbs every key while open, except ctrl+c
		// which always quits. We route here BEFORE the search-box
		// branch so the modal can sit on top of any tab.
		if msg.String() != "ctrl+c" && m.settings.IsOpen() {
			var action settingsAction
			m.settings, action = m.settings.Update(msg)
			switch action {
			case actionCancel:
				m.settings = m.settings.Close()
			case actionSaveAndExit:
				cmd := m.applySettingsAndClose()
				return m, cmd
			}
			return m, nil
		}

		// When a sub-model is capturing text input (e.g. a search
		// box), give it the keystroke first so "q", "1"–"5", "tab"
		// etc. become literal characters instead of triggering the
		// global hotkeys. ctrl+c still quits regardless.
		if msg.String() != "ctrl+c" {
			switch {
			case m.activeTab == TabRepos && m.repos.IsInputMode():
				var cmd tea.Cmd
				m.repos, cmd = m.repos.Update(msg, m.stats)
				return m, cmd
			case m.activeTab == TabPRs && m.prs.IsInputMode():
				var cmd tea.Cmd
				m.prs, cmd = m.prs.Update(msg, m.stats)
				return m, cmd
			case m.activeTab == TabIssues && m.issues.IsInputMode():
				var cmd tea.Cmd
				m.issues, cmd = m.issues.Update(msg, m.stats)
				return m, cmd
			}
		}

		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit
		case ",":
			// Open the in-app settings panel, seeded with the current
			// live values so the form reflects what the user is
			// actually running. Subsequent keystrokes are absorbed by
			// the modal until it returns actionCancel / actionSaveAndExit.
			m.settings = m.settings.Open(m.interval, m.compact, m.client.PublicOnly(), m.theme)
			return m, nil
		case "p":
			// Quick toggle for public-only mode without going through
			// the settings panel — the most "screenshot-relevant"
			// switch deserves a one-key shortcut. Filter is applied
			// at render time (see Stats.Public) so flipping it shows
			// up immediately on the next paint, no refetch needed.
			newVal := !m.client.PublicOnly()
			m.client.SetPublicOnly(newVal)
			// Toggling public-only changes which repos / PRs / issues
			// the Overview body lists, so the rendered length shifts —
			// refresh the scroll viewports so the footer hint and
			// overflow detection stay in step.
			syncOverviewViewport(&m)
			syncActivityViewport(&m)
			if m.configPath != "" {
				_ = config.Save(m.configPath, config.Config{
					RefreshInterval: m.interval,
					PublicOnly:      newVal,
					Compact:         m.compact,
					Theme:           m.theme,
					AccentColor:     m.accentColor,
				})
			}
			return m, nil
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
			// Any other key is forwarded to the active tab's sub-model.
			// Global keys above have already matched by this point.
			switch m.activeTab {
			case TabRepos:
				var cmd tea.Cmd
				m.repos, cmd = m.repos.Update(msg, m.stats)
				return m, cmd
			case TabPRs:
				var cmd tea.Cmd
				m.prs, cmd = m.prs.Update(msg, m.stats)
				return m, cmd
			case TabIssues:
				var cmd tea.Cmd
				m.issues, cmd = m.issues.Update(msg, m.stats)
				return m, cmd
			case TabOverview:
				// Static tab content — let the viewport handle scroll
				// keys (up/down/pgup/pgdn/space/u/d/f/b). Sync content
				// + size to the current model state before delegating
				// so the viewport's internal maxYOffset is correct.
				syncOverviewViewport(&m)
				var cmd tea.Cmd
				m.overviewVP, cmd = m.overviewVP.Update(msg)
				return m, cmd
			case TabActivity:
				syncActivityViewport(&m)
				var cmd tea.Cmd
				m.activityVP, cmd = m.activityVP.Update(msg)
				return m, cmd
			}
		}

	case fetchMsg:
		m.loading = false
		previous := m.stats
		m.stats = msg.stats
		m.err = msg.err
		m.errReason = github.ReasonUnknown
		m.lastFetch = msg.at

		// Refresh the scroll viewports against the new stats so the
		// overflow / hint state matches reality immediately. Stats
		// changes can shrink or grow the rendered content (Top
		// repositories appearing once a third starred repo arrives,
		// the Stars+Forks card disappearing when forks lose stars,
		// etc.), and the viewport's YOffset clamps itself if the new
		// content is shorter than the previous offset.
		syncOverviewViewport(&m)
		syncActivityViewport(&m)

		// Pull a typed reason off the error envelope so the footer
		// can specialise its message without sniffing the string.
		if msg.err != nil {
			var fe *github.FetchError
			if errors.As(msg.err, &fe) {
				m.errReason = fe.Reason
			}
		}

		// Refresh the rate-limit snapshot from whichever side carries
		// it: successful fetches include it in stats; failed ones
		// leave lastRateLimit alone so we can still back off against
		// its resetAt if the failure was a rate-limit.
		if msg.stats != nil && msg.stats.RateLimit != nil {
			m.lastRateLimit = msg.stats.RateLimit
		}

		// Reschedule the next tick here rather than inside tickMsg so
		// we can stretch the cadence when GitHub tells us we're out
		// of budget. Manual "r" refreshes bypass the timer entirely.
		nextTick := tickCmd(m.nextRefreshDelay())

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
					nextTick,
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
		return m, nextTick

	case tickMsg:
		// Every `interval`, re-fetch. The next tick is scheduled by
		// the fetchMsg handler so we can back off when rate-limited
		// without hammering every 60s. Flip loading=true so the
		// footer spinner shows.
		m.loading = true
		return m, tea.Batch(fetchCmd(m.client), m.spinner.Tick)

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

// applySettingsAndClose commits the staged values from the settings
// modal: it mutates the live Model fields, persists to disk (if a
// config path was given on launch), closes the panel, and returns a
// fresh tickCmd when the refresh interval changed.
//
// public-only and compact don't need a fetch round-trip to take
// effect — both are applied at render time (Stats.Public() and
// renderCardRow's compact branch respectively), so flipping them
// just requires the next Update→View cycle, which happens as soon
// as we return.
func (m *Model) applySettingsAndClose() tea.Cmd {
	newInterval, _ := m.settings.Refresh() // already validated
	newCompact := m.settings.Compact()
	newPublicOnly := m.settings.PublicOnly()
	newTheme := m.settings.Theme()

	intervalChanged := newInterval != m.interval
	themeChanged := newTheme != m.theme

	m.interval = newInterval
	m.compact = newCompact
	m.client.SetPublicOnly(newPublicOnly)
	if themeChanged {
		m.theme = newTheme
		// Reapply with the (possibly-still-set) accent override so
		// switching theme doesn't silently drop a user-customised
		// accent. Then re-foreground the spinner so its colour
		// tracks the new accent.
		_ = applyTheme(newTheme, m.accentColor)
		m.spinner.Style = lipgloss.NewStyle().Foreground(colAccent)
	}

	// Persist. If the path is empty (no HOME / XDG_CONFIG_HOME
	// resolved) or the write fails, just stay quiet — the in-memory
	// state is already updated, and surfacing a "save failed"
	// toast right now would be more noise than value. A future
	// release can add a footer indicator for this if it ever bites.
	if m.configPath != "" {
		_ = config.Save(m.configPath, config.Config{
			RefreshInterval: newInterval,
			PublicOnly:      newPublicOnly,
			Compact:         newCompact,
			Theme:           m.theme,
			AccentColor:     m.accentColor,
		})
	}

	m.settings = m.settings.Close()

	// Compact, public-only, and theme switches all change the
	// rendered length / width math of the Overview tab. Re-sync so
	// the scroll viewports + footer hint reflect the new layout
	// without waiting for the next keystroke.
	syncOverviewViewport(m)
	syncActivityViewport(m)

	if intervalChanged {
		// New cadence kicks in immediately; the existing in-flight
		// tick will still fire once with the old delay, but the
		// scheduler picks the shorter of the two because we batch
		// the new tickCmd here.
		return tickCmd(newInterval)
	}
	return nil
}

// nextRefreshDelay decides when to re-fetch after a fetchMsg. The
// default is the configured interval (60s); on a primary rate-limit
// error we stretch it to just past the ResetAt so the next attempt
// actually has budget to succeed. Secondary limits are short-lived —
// a single interval of backoff is enough. Other failures keep the
// default cadence so transient blips recover quickly.
func (m Model) nextRefreshDelay() time.Duration {
	switch m.errReason {
	case github.ReasonRateLimitPrimary:
		if m.lastRateLimit != nil && !m.lastRateLimit.ResetAt.IsZero() {
			d := time.Until(m.lastRateLimit.ResetAt) + 5*time.Second
			if d > m.interval {
				return d
			}
		}
	case github.ReasonRateLimitSecondary:
		// Secondary limits auto-clear in ~30–60s; one interval
		// pause is both polite and enough.
		if m.interval < 60*time.Second {
			return 60 * time.Second
		}
	}
	return m.interval
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

