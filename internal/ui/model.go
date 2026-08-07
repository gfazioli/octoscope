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
	"github.com/gfazioli/octoscope/internal/update"
)

// Tab identifies one of the top-level views. Values map to the key
// bindings ("1".."7") by position.
//
// Gists landed at position 6 in v0.29.0 rather than at the end, which
// moved What's new from "6" to "7". That was the deliberate half of the
// choice: Gists is a *content* tab and belongs beside the others, while
// What's new is a meta tab whose meaningful stable position is last —
// keeping it there costs one digit of muscle memory and keeps the row
// readable. Nothing persists the active tab, so no stored value breaks.
type Tab int

const (
	TabOverview Tab = iota
	TabRepos
	TabPRs
	TabIssues
	TabActivity
	TabGists
	TabWhatsNew
)

// tabCount is the number of tabs. Keep in sync with the Tab constants.
const tabCount = 7

// tabLabels is the visible name for each tab, indexed by Tab value.
var tabLabels = [tabCount]string{
	"Overview",
	"Repos",
	"PRs",
	"Issues",
	"Activity",
	"Gists",
	"What's new",
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

	// refreshGen identifies the live auto-refresh tick chain. Init
	// seeds chain 0; an interval change increments it so the previous
	// chain's next tick is recognised as stale and dropped, leaving
	// exactly one chain running at any time. See tickMsg.
	refreshGen int

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

	// showSponsor mirrors the config's show_sponsor knob so the settings
	// panel can edit and persist it (#61). It gates the launch-time
	// splash only — flipping it mid-run just changes the next start.
	showSponsor bool

	// noColor records that this run was forced to the monochrome
	// palette by the NO_COLOR env convention or the --no-color flag
	// (resolved in main.go). It's an environment directive for the
	// current session, NOT a stored preference: persistConfig leaves
	// the file's theme / accent_color keys untouched while it's set,
	// so the user's real theme survives unsetting NO_COLOR.
	noColor bool

	// pinned is the live list of "owner/name" pinned repositories
	// for the Repos tab (v0.13.0). Mutable via P on a row or via
	// the action menu; on every change we write back to config so
	// the next launch picks the same set up. The slice is treated
	// as ordered — first entry renders first in the pinned
	// section — to preserve the user's intent.
	pinned []string

	// pinnedIssues is the live list of "owner/name#N" pinned issues
	// for the Issues tab (v0.21.0). Same lifecycle as pinned: mutable
	// via P on a row or via the action menu, written back to config on
	// every change, and treated as ordered so config order is the
	// section order.
	pinnedIssues []string

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
	// via number keys "1".."7" or Tab/Shift+Tab.
	activeTab Tab

	// repos, prs, issues hold per-tab state (cursor / sort / search).
	// Sub-models keep tab-specific state from bloating this root
	// struct — each tab owns its own idioms (PRs has mergeable state,
	// Issues has no state column, Repos has language colours) and
	// Update dispatches to the sub-model of the active tab.
	repos  ReposModel
	prs    PRsModel
	issues IssuesModel
	gists  GistsModel

	// starModeDefault seeds RepoDetailModel.starMode on every
	// drill-in Open (config key default_star_history, #35). The
	// in-detail `v` cycle changes the open detail only; the next
	// Open starts from this default again.
	starModeDefault StarHistoryMode

	// overviewVP / activityVP scroll the static-content tabs
	// vertically when the terminal is shorter than the rendered body.
	// Repos / PRs / Issues already paginate their own row lists, so
	// they don't need a viewport. The viewports are kept on the model
	// so YOffset persists across re-renders; width/height/SetContent
	// are refreshed before each scroll keystroke (see scroll.go).
	overviewVP viewport.Model
	activityVP viewport.Model

	// actionMenu is the modal action picker shown when the user
	// presses Ctrl+Enter on a list-tab row. While open it absorbs
	// every keystroke (except ctrl+c) — same dispatch idiom as the
	// settings panel. Closed by esc, or by selecting an action; the
	// chosen Action's Cmd is returned to the BubbleTea runtime so
	// the side-effect (open browser, copy URL, request detail view)
	// happens on the next tick.
	actionMenu ActionMenuModel

	// repoDetail is the drill-in view shown when the user picks
	// "View details" from a Repos action menu (or presses `d`
	// directly on a row). When IsOpen() the Repos tab body is
	// replaced by the detail's render — banner, profile, tab bar
	// and footer stay pinned. Esc closes and returns to the list
	// with cursor preserved.
	repoDetail RepoDetailModel

	// prDetail mirrors repoDetail for the PRs tab — same drill-in
	// pattern (sticky title + viewport-wrapped body, stale-fetch
	// protection by URL, esc back / r refetch / o open). Only one
	// of repoDetail / prDetail / issueDetail is ever open at a time
	// (opening a new one closes the previous via the action-menu
	// dispatch flow).
	prDetail PRDetailModel

	// issueDetail mirrors prDetail for the Issues tab. Simpler
	// shape (no checks, no diff size, no head/base) — see
	// internal/ui/issue_detail.go and CLAUDE.md's drill-in
	// pattern note.
	issueDetail IssueDetailModel

	// scan is the supply-chain integrity-scan drill-in opened from
	// the Repos action menu ("Security scan", `s`). Same drill-in
	// idiom and mutual-exclusion contract as repoDetail / prDetail /
	// issueDetail — only one of the four is ever open at a time. See
	// internal/ui/scan.go and docs/design/supply-chain-scan.md.
	scan ScanModel

	// help is the keyboard-shortcut overlay opened with `?`. Like the
	// other modals it absorbs keys while open (any key dismisses) and
	// renders at the top of the modal priority chain.
	help HelpModel

	// rateLimits is the per-resource API budget panel opened with `%`
	// (v0.18.0). The footer chip stays the ambient indicator; this is
	// the drill-down for "why is octoscope slow / 403-ing". Unlike
	// help it routes keys explicitly (r refetches) instead of
	// dismissing on any key.
	rateLimits RateLimitModel

	// sponsor is the splash inviting the user to sponsor octoscope
	// (v0.16.0). Opened at every startup when show_sponsor is on and
	// we're not in --public-only mode. While open it absorbs keys
	// (o open · c copy · any key dismiss), same modal idiom as the
	// settings panel / action menu. Dismissal is session-only — there's
	// no persisted "seen" flag, so it reappears next launch by design.
	sponsor SponsorModel

	// toastMsg is a transient one-line status shown in place of the
	// footer freshness for `toastDuration` after an event. Used today
	// for "URL copied" and the "View details — coming soon" stub;
	// any future inline notification can pipe through here too.
	toastMsg   string
	toastUntil time.Time

	// checkForUpdates gates the in-app update check (v0.19.0). When
	// false (config check_for_updates=false) Init fires neither the
	// startup check nor the hourly tick, and no notice ever renders.
	checkForUpdates bool

	// updateLatest is the latest octoscope release tag seen by the
	// update check (e.g. "v0.19.0"); updateAvailable is true when it's
	// strictly newer than the running version. updateChannel is how
	// this binary was installed, so the notice can suggest the right
	// upgrade command (octoscope never self-updates). See internal/update.
	updateLatest    string
	updateAvailable bool
	updateChannel   update.Channel
}

// toastDuration is how long a transient footer toast stays visible
// after a user-triggered event before reverting to the freshness
// label. Long enough to read, short enough not to outstay its
// welcome.
const toastDuration = 2 * time.Second

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
	// subtitle is the semantic category — "Stars" / "Followers"
	// / "Stars & Followers" when both moved at once. Gives the
	// macOS Notification Center a second line that keeps the
	// per-entry timestamp visible even when terminal-notifier
	// notifications group (see notify.Send doc for the
	// rationale). When only one bucket moved it doubles as the
	// natural click-target hint.
	subtitle := ""
	if new.Login != "" {
		switch {
		case followersChanged && starsChanged:
			subtitle = "Stars & Followers"
			clickURL = "https://github.com/" + new.Login
		case followersChanged:
			subtitle = "Followers"
			clickURL = "https://github.com/" + new.Login
		case starsChanged:
			subtitle = "Stars"
			clickURL = "https://github.com/" + new.Login + "?tab=stars"
		}
	}

	return func() tea.Msg {
		_ = notify.Send("octoscope — "+who, subtitle, msg, clickURL)
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

	// NoColor records that main.go forced the monochrome palette for
	// this run via the NO_COLOR env convention or the --no-color flag.
	// main.go has already set Theme to "monochrome" and cleared
	// AccentColor; this flag only tells the Model to keep the file's
	// theme / accent_color keys untouched on persist (the directive is
	// environmental, not a stored preference).
	NoColor bool

	// DefaultSort / DefaultWorkFilter / DefaultStarHistory seed the
	// initial view state (config keys default_sort /
	// default_work_filter / default_star_history, #35): the list
	// tabs' sort cycles, the Repos work filter, and the star-history
	// sparkline mode. Already validated by main.go against the
	// ui.IsValid*Key helpers; empty strings keep the built-in
	// defaults.
	DefaultSort        string
	DefaultWorkFilter  string
	DefaultStarHistory string

	// PinnedRepos is the persisted list of "owner/name" identifiers
	// that the Repos tab renders in a sticky section at the top.
	// Already sanitised (see config.SanitizeRepoList) by the
	// caller — NewModel trusts the slice as-is.
	PinnedRepos []string

	// PinnedIssues is the persisted list of "owner/name#N" identifiers
	// that the Issues tab renders in a sticky section at the top.
	// Already sanitised (see config.SanitizeIssueList) by the
	// caller — NewModel trusts the slice as-is.
	PinnedIssues []string

	// ShowSponsor gates the sponsor splash (v0.16.0). When true (and
	// not in public-only mode) the splash opens on every launch. False
	// opts out — set via config show_sponsor or the --no-sponsor flag,
	// already resolved by main.go before reaching here.
	ShowSponsor bool

	// CheckForUpdates gates the v0.19.0 in-app update check (config
	// check_for_updates). When true, NewModel records the install
	// channel and Init starts the check + hourly poll.
	CheckForUpdates bool
}

// NewModel returns a Model ready for tea.NewProgram. The first fetch
// is kicked off as an Init command so the UI renders a loading state
// immediately rather than waiting for the network.
func NewModel(client *github.Client, version string, opts Options) Model {
	interval := config.NormalizeInterval(opts.Interval)
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

	// Sponsor splash: open on every launch unless the user opted out
	// (show_sponsor=false / --no-sponsor) or we're in --public-only
	// mode (so screenshots / screencasts stay clean).
	var sponsor SponsorModel
	if opts.ShowSponsor && !client.PublicOnly() {
		sponsor = sponsor.Open(sponsorURL)
	}

	// Detect the install channel once at startup (cheap, but only
	// meaningful when the update check is on) so the update notice can
	// suggest the right upgrade command without re-probing each render.
	var updateChannel update.Channel
	if opts.CheckForUpdates {
		updateChannel = update.DetectChannel()
	}

	return Model{
		client:       client,
		loading:      true,
		interval:     interval,
		compact:      opts.Compact,
		configPath:   opts.ConfigPath,
		theme:        themeName,
		accentColor:  opts.AccentColor,
		showSponsor:  opts.ShowSponsor,
		noColor:      opts.NoColor,
		pinned:       append([]string(nil), opts.PinnedRepos...),
		pinnedIssues: append([]string(nil), opts.PinnedIssues...),
		// Per-tab view state seeded from the default_* config keys
		// (#35). A missing/empty key hits the maps' zero values —
		// i.e. each tab's built-in default; main.go already rejected
		// invalid non-empty values.
		repos:           ReposModel{sort: reposSortKeys[opts.DefaultSort], work: workFilterKeys[opts.DefaultWorkFilter]},
		prs:             PRsModel{sort: listSortKeys[opts.DefaultSort].prs},
		issues:          IssuesModel{sort: listSortKeys[opts.DefaultSort].issues},
		starModeDefault: starHistoryKeys[opts.DefaultStarHistory],
		version:         version,
		spinner:         sp,
		pulseMap:        make(map[string]time.Time),
		overviewVP:      viewport.New(0, 0),
		activityVP:      viewport.New(0, 0),
		sponsor:         sponsor,
		checkForUpdates: opts.CheckForUpdates,
		updateChannel:   updateChannel,
	}
}

// Init starts the first fetch, schedules the periodic tick, starts
// the 1-second clock that keeps the footer freshness label live,
// and kicks off the spinner animation — we're in the loading state
// on first paint so the spinner is already visible.
func (m Model) Init() tea.Cmd {
	cmds := []tea.Cmd{
		// Startup fetch is a one-shot paint (manual=true): it must not
		// seed a second chain. The lone tickCmd below is the single
		// auto-refresh chain (generation 0).
		fetchCmd(m.client, true, m.refreshGen),
		tickCmd(m.interval, m.refreshGen),
		clockTickCmd(),
		m.spinner.Tick,
	}
	// Update check (v0.19.0): an immediate cache-aware check plus the
	// hourly poll. Gated on the config knob AND --public-only — a
	// public-only / screenshot session does no release polling and
	// writes no cache, so tape runs stay hermetic (the notice is also
	// hidden in the render path; this stops the work at the source).
	// Independent of the dashboard refresh chain.
	if m.checkForUpdates && !m.client.PublicOnly() {
		cmds = append(cmds, updateCheckCmd(m.client), updateTickCmd())
	}
	return tea.Batch(cmds...)
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
		// Sponsor splash has the highest priority: while open it absorbs
		// every key except ctrl+c. `o` opens the GitHub Sponsors page,
		// `b` opens the one-off "buy me a coffee" link, `c` copies the
		// Sponsors URL, and ANY other key dismisses. Dismissal is
		// session-only (no persisted flag) — by design the splash
		// reappears on the next launch unless the user opted out.
		if msg.String() != "ctrl+c" && m.sponsor.IsOpen() {
			var cmd tea.Cmd
			switch msg.String() {
			case "o":
				cmd = openURLCmd(m.sponsor.url)
			case "b":
				cmd = openURLCmd(coffeeURL)
			case "c":
				cmd = copyURLCmd(m.sponsor.url)
			}
			m.sponsor = m.sponsor.Close()
			return m, cmd
		}

		// Help overlay is the next-highest surface: while open, any key
		// except ctrl+c dismisses it. Read-only, so there's nothing to
		// apply — just close.
		if msg.String() != "ctrl+c" && m.help.IsOpen() {
			m.help = m.help.Close()
			return m, nil
		}

		// Rate-limit panel routes keys explicitly while open (esc
		// close, r refetch, q quit) — it can't be any-key-dismiss like
		// help because `r` must reach the panel, not fall through to
		// the dashboard refresh.
		if msg.String() != "ctrl+c" && m.rateLimits.IsOpen() {
			var cmd tea.Cmd
			m.rateLimits, cmd = m.rateLimits.Update(msg, m.client)
			return m, cmd
		}

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

		// Action menu has the same priority as the settings modal:
		// while open, it absorbs every key except ctrl+c. Routed
		// before the per-tab dispatch so the menu can be opened from
		// any list tab and stay focused until dismissed.
		if msg.String() != "ctrl+c" && m.actionMenu.IsOpen() {
			newMenu, cmd := m.actionMenu.Update(msg)
			m.actionMenu = newMenu
			return m, cmd
		}

		// Repo-detail drill-in absorbs keystrokes when open: esc
		// dismisses, r refetches, o opens the underlying URL in the
		// browser, everything else falls through to its internal
		// viewport for body scrolling (↑/↓/pgup/pgdn/space/u/d).
		// Width / height are the same budget View uses so the
		// viewport's maxYOffset stays correct across resizes.
		if msg.String() != "ctrl+c" && m.repoDetail.IsOpen() {
			width := computeAvailable(m.width)
			height := computeTabHeight(m)
			newDetail, cmd := m.repoDetail.Update(msg, m.client, width, height)
			m.repoDetail = newDetail
			return m, cmd
		}

		// PR-detail drill-in — same dispatch shape as repoDetail.
		if msg.String() != "ctrl+c" && m.prDetail.IsOpen() {
			width := computeAvailable(m.width)
			height := computeTabHeight(m)
			newDetail, cmd := m.prDetail.Update(msg, m.client, width, height)
			m.prDetail = newDetail
			return m, cmd
		}

		// Issue-detail drill-in — same dispatch shape.
		if msg.String() != "ctrl+c" && m.issueDetail.IsOpen() {
			width := computeAvailable(m.width)
			height := computeTabHeight(m)
			newDetail, cmd := m.issueDetail.Update(msg, m.client, width, height)
			m.issueDetail = newDetail
			return m, cmd
		}

		// Integrity-scan drill-in — same dispatch shape as the detail
		// views. Routed before the per-tab dispatch so its keys
		// (esc / r / o / y / scroll) win while open.
		if msg.String() != "ctrl+c" && m.scan.IsOpen() {
			width := computeAvailable(m.width)
			height := computeTabHeight(m)
			newScan, cmd := m.scan.Update(msg, m.client, width, height)
			m.scan = newScan
			return m, cmd
		}

		// When a sub-model is capturing text input (e.g. a search
		// box), give it the keystroke first so "q", "1"–"5", "tab"
		// etc. become literal characters instead of triggering the
		// global hotkeys. ctrl+c still quits regardless.
		//
		// IMPORTANT: pass effectiveStats() (not m.stats) so the
		// sub-model sees exactly the rows the render path showed.
		// In public-only mode m.stats includes private items that
		// are filtered out at render time; if we passed m.stats
		// here, the sub-model's row[cursor] would be different
		// from the row the user is looking at, and Enter would
		// open a drill-in for the wrong PR/issue/repo. See the
		// matching note in the default-branch dispatch below.
		eff := m.effectiveStats()
		if msg.String() != "ctrl+c" {
			switch {
			case m.activeTab == TabRepos && m.repos.IsInputMode():
				var cmd tea.Cmd
				m.repos, cmd = m.repos.Update(msg, eff, m.pinned)
				return m, cmd
			case m.activeTab == TabPRs && m.prs.IsInputMode():
				var cmd tea.Cmd
				m.prs, cmd = m.prs.Update(msg, eff)
				return m, cmd
			case m.activeTab == TabGists && m.gists.IsInputMode():
				var cmd tea.Cmd
				m.gists, cmd = m.gists.Update(msg, eff)
				return m, cmd
			case m.activeTab == TabIssues && m.issues.IsInputMode():
				var cmd tea.Cmd
				m.issues, cmd = m.issues.Update(msg, eff, m.pinnedIssues)
				return m, cmd
			}
		}

		// Open action menu on a list-tab row. Handled here (before
		// the global key switch) and ONLY for Repos / PRs / Issues,
		// so on Overview / Activity the same key falls through to
		// the default branch and reaches the viewport — which uses
		// space as a page-down. Without this guard the action-menu
		// case would unconditionally `return m, nil` on every tab
		// and silently regress the documented page-down binding.
		//
		// Ctrl+Enter is also accepted for emulators that deliver
		// the modifier (kitty, alacritty + xterm modifyOtherKeys,
		// vte ≥ 0.74); most VT100-derived terminals (iTerm, stock
		// macOS Terminal) silently drop ctrl on enter and the
		// keystroke arrives as plain "enter" — which since v0.11.0
		// the row's "View details" handler picks up (drill-in by
		// convention, mirroring lazygit / k9s / ranger). Space stays
		// as the out-of-the-box action-menu gesture, ctrl+enter as a
		// power-user alternative for terminals that pass the modifier.
		if (msg.String() == " " || msg.String() == "ctrl+@" || msg.String() == "ctrl+enter") &&
			(m.activeTab == TabRepos || m.activeTab == TabPRs || m.activeTab == TabIssues) {
			s := m.stats
			if s != nil && m.client.PublicOnly() {
				s = s.Public()
			}
			var (
				title   string
				actions []Action
			)
			switch m.activeTab {
			case TabRepos:
				if r, ok := m.repos.selectedRepo(s, m.pinned); ok {
					title = "Actions for " + r.Name
					pinLabel := "Pin"
					pinNext := true
					if isPinned(r.URL, m.pinned) {
						pinLabel = "Unpin"
						pinNext = false
					}
					actions = []Action{
						{Label: "Open in GitHub", Shortcut: 'o', Cmd: openURLCmd(r.URL)},
						{Label: "View details", Shortcut: 'd', Cmd: viewRepoDetailCmd(r)},
						{Label: "Security scan", Shortcut: 's', Cmd: viewRepoScanCmd(r)},
						{Label: pinLabel, Shortcut: 'P', Cmd: togglePinCmd(r.URL, pinNext)},
						{Label: "Copy URL", Shortcut: 'c', Cmd: copyURLCmd(r.URL)},
					}
				}
			case TabPRs:
				if p, ok := m.prs.selectedPR(s); ok {
					title = fmt.Sprintf("Actions for PR #%d", p.Number)
					actions = []Action{
						{Label: "Open in GitHub", Shortcut: 'o', Cmd: openURLCmd(p.URL)},
						{Label: "View details", Shortcut: 'd', Cmd: viewPRDetailCmd(p)},
						{Label: "Copy URL", Shortcut: 'c', Cmd: copyURLCmd(p.URL)},
					}
				}
			case TabIssues:
				if it, ok := m.issues.selectedIssue(s, m.pinnedIssues); ok {
					title = fmt.Sprintf("Actions for issue #%d", it.Number)
					pinLabel := "Pin"
					pinNext := true
					if isPinnedIssue(it, m.pinnedIssues) {
						pinLabel = "Unpin"
						pinNext = false
					}
					actions = []Action{
						{Label: "Open in GitHub", Shortcut: 'o', Cmd: openURLCmd(it.URL)},
						{Label: "View details", Shortcut: 'd', Cmd: viewIssueDetailCmd(it)},
						{Label: pinLabel, Shortcut: 'P', Cmd: togglePinIssueCmd(issueID(it), pinNext)},
						{Label: "Copy URL", Shortcut: 'c', Cmd: copyURLCmd(it.URL)},
					}
				}
			case TabGists:
				// No "View details": the files expand inline on the row,
				// since they arrived with it and there is nothing to fetch.
				if g, ok := m.gists.selectedGist(s); ok {
					title = "Actions for " + gistLabel(g)
					actions = []Action{
						{Label: "Open in GitHub", Shortcut: 'o', Cmd: openURLCmd(g.URL)},
						{Label: "Copy URL", Shortcut: 'c', Cmd: copyURLCmd(g.URL)},
					}
				}
			}
			if len(actions) > 0 {
				m.actionMenu = m.actionMenu.Open(title, actions)
			}
			return m, nil
		}

		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit
		case ",":
			// Open the in-app settings panel, seeded with the current
			// live values so the form reflects what the user is
			// actually running. Subsequent keystrokes are absorbed by
			// the modal until it returns actionCancel / actionSaveAndExit.
			m.settings = m.settings.Open(m.interval, m.compact, m.client.PublicOnly(), m.theme, m.accentColor, m.showSponsor)
			return m, nil
		case "?":
			// Open the keyboard-shortcut overlay. Reached only outside
			// input mode (the search-box guard above returns first), so
			// `?` never opens help while the user is typing a filter.
			m.help = m.help.Open()
			return m, nil
		case "%":
			// Open the rate-limit detail panel (v0.18.0) and fire the
			// /rate_limit fetch. Same input-mode protection as `?` —
			// while a filter is being typed, % is a literal character.
			m.rateLimits = m.rateLimits.Open()
			return m, fetchRateLimitsCmd(m.client)
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
			// Best-effort writeback through the shared read-modify-write
			// helper so toggling public-only can't clobber pinned_repos
			// / watch_repos. m.client already holds the new value.
			_ = m.persistConfig()
			return m, nil
		case "r":
			if !m.loading {
				m.loading = true
				// Restart the spinner tick alongside the fetch so the
				// animation begins immediately on user-triggered
				// refreshes too. manual=true: a manual refresh must not
				// spawn a second auto-refresh chain.
				return m, tea.Batch(fetchCmd(m.client, true, m.refreshGen), m.spinner.Tick)
			}
		case "tab", "shift+tab":
			if msg.String() == "tab" {
				m.activeTab = (m.activeTab + 1) % tabCount
			} else {
				m.activeTab = (m.activeTab - 1 + tabCount) % tabCount
			}
			return m, nil
		case "1", "2", "3", "4", "5", "6", "7":
			// Digit → zero-based tab index. Safe because len("1"..."7") == 1
			// and the range is bounded by tabCount via the case list.
			m.activeTab = Tab(msg.String()[0] - '1')
			return m, nil
		default:
			// Any other key is forwarded to the active tab's sub-model.
			// Global keys above have already matched by this point.
			//
			// effectiveStats() (with the public-only filter
			// applied) is the same payload the render path passes
			// to renderXTab — the sub-model's row[cursor]
			// arithmetic must agree with what the user is looking
			// at, otherwise Enter on the highlighted row drills
			// into a different item (e.g. a private PR that's
			// filtered out of the view but still in m.stats).
			eff := m.effectiveStats()
			switch m.activeTab {
			case TabRepos:
				var cmd tea.Cmd
				m.repos, cmd = m.repos.Update(msg, eff, m.pinned)
				return m, cmd
			case TabPRs:
				var cmd tea.Cmd
				m.prs, cmd = m.prs.Update(msg, eff)
				return m, cmd
			case TabIssues:
				var cmd tea.Cmd
				m.issues, cmd = m.issues.Update(msg, eff, m.pinnedIssues)
				return m, cmd
			case TabGists:
				var cmd tea.Cmd
				m.gists, cmd = m.gists.Update(msg, eff)
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
			case TabWhatsNew:
				// Static tab. The only actions are on the embedded
				// support links: o opens GitHub Sponsors, b opens the
				// one-off "buy me a coffee" link, c copies the Sponsors
				// URL.
				switch msg.String() {
				case "o":
					return m, openURLCmd(sponsorURL)
				case "b":
					return m, openURLCmd(coffeeURL)
				case "c":
					return m, copyURLCmd(sponsorURL)
				}
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
		// we can stretch the cadence when GitHub tells us we're out of
		// budget. Only timer-origin fetches reschedule: a manual fetch
		// (startup paint, `r`, settings save) leaves nextTick nil so it
		// can't spawn a parallel chain. tea.Batch / a bare return both
		// tolerate a nil cmd.
		var nextTick tea.Cmd
		if !msg.manual {
			// Reschedule under the gen that ORIGINATED this fetch, not
			// the model's current gen: if an interval change bumped
			// refreshGen while this fetch was in flight, the new tick is
			// stamped stale and the guard drops it — so an in-flight
			// fetch can't resurrect a superseded chain (the "exactly one
			// chain" invariant holds even across that interleave).
			nextTick = tickCmd(m.nextRefreshDelay(), msg.gen)
		}

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
		// Drop ticks from a superseded chain (an interval change bumped
		// refreshGen): not fetching and not rescheduling lets the stale
		// chain self-terminate, so exactly one chain stays alive.
		if msg.gen != m.refreshGen {
			return m, nil
		}
		// Every `interval`, re-fetch. The next tick is scheduled by the
		// fetchMsg handler (timer-origin, manual=false) so we can back
		// off when rate-limited without hammering every 60s. Flip
		// loading=true so the footer spinner shows.
		m.loading = true
		// Carry this tick's gen into the fetch so its rescheduled tick
		// inherits the same gen (see fetchMsg.gen).
		return m, tea.Batch(fetchCmd(m.client, false, msg.gen), m.spinner.Tick)

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

	case updateCheckMsg:
		// Store the latest release and flag whether it's newer than the
		// running build. Gated on checkForUpdates as belt-and-braces
		// (Init wouldn't have fired the check otherwise). A failed/empty
		// check leaves updateAvailable false — silent by design.
		m.updateLatest = msg.latest
		m.updateAvailable = m.checkForUpdates && update.IsNewer(m.version, msg.latest)
		return m, nil

	case updateTickMsg:
		// Hourly re-check. Re-arm the (single, fixed-interval) chain and
		// fire another cache-aware check. Self-terminates if the feature
		// is off or public-only is on (e.g. toggled on at runtime) so a
		// public-only session stops polling and writing the cache.
		if !m.checkForUpdates || m.client.PublicOnly() {
			return m, nil
		}
		return m, tea.Batch(updateCheckCmd(m.client), updateTickCmd())

	case showToastMsg:
		m.toastMsg = msg.text
		m.toastUntil = time.Now().Add(toastDuration)
		// Schedule a redraw at expiry so the toast disappears
		// without waiting for the next refresh / keystroke.
		return m, tea.Tick(toastDuration, func(time.Time) tea.Msg {
			return clockTickMsg(time.Now())
		})

	case viewRepoDetailMsg:
		// Open the drill-in view in the loading state and fire a
		// targeted GraphQL fetch for that one repo. The detail's
		// own Update handles the response when it lands.
		owner, name := github.SplitOwnerName(msg.repo.URL)
		if owner == "" || name == "" {
			// Defensive: shouldn't happen — Repo.URL is always a
			// canonical github.com URL on rows that came from
			// FetchStats. Surface a toast so a future regression is
			// visible rather than silent.
			m.toastMsg = "Could not parse repo URL"
			m.toastUntil = time.Now().Add(toastDuration)
			return m, tea.Tick(toastDuration, func(time.Time) tea.Msg {
				return clockTickMsg(time.Now())
			})
		}
		// Close any other drill-in first. BubbleTea cmds are async
		// so a user who triggers space→d on a Repos row, switches
		// tab, and triggers space→d on a PRs row before the first
		// fetch lands could otherwise have two detail models open
		// simultaneously — the View priority switch would render
		// one while late fetched data populates the other,
		// confusing back-navigation. One-detail-at-a-time is the
		// invariant the View priority assumes.
		m.prDetail = m.prDetail.Close()
		m.issueDetail = m.issueDetail.Close()
		m.scan = m.scan.Close()
		// Every fresh drill-in starts from the configured star-history
		// default (#35); the `v` cycle inside the detail stays a
		// per-visit choice.
		m.repoDetail = m.repoDetail.Open(msg.repo, m.starModeDefault)
		return m, fetchRepoDetailCmd(m.client, owner, name, msg.repo.URL)

	case repoDetailFetchedMsg:
		// Apply the fetched payload only when the user is still
		// looking at the same repo — they may have closed the
		// detail or opened a different one before the network came
		// back. URL identity is the cheap correlation key.
		if !m.repoDetail.IsOpen() || m.repoDetail.repo.URL != msg.url {
			return m, nil
		}
		m.repoDetail = m.repoDetail.applyFetched(msg.detail, msg.err)
		return m, nil

	case rateLimitsFetchedMsg:
		// Stale-fetch protection by open-state alone: the panel has
		// no per-item identity (there's exactly one /rate_limit), so
		// "still open" is the only correlation that matters. A reply
		// landing after esc is silently dropped.
		if !m.rateLimits.IsOpen() {
			return m, nil
		}
		m.rateLimits = m.rateLimits.applyFetched(msg.limits, msg.err)
		return m, nil

	case viewPRDetailMsg:
		// PRs drill-in mirror of viewRepoDetailMsg. Open the
		// detail in loading state and fire the targeted fetch.
		owner, name, num := github.SplitOwnerNameNumber(msg.pr.URL)
		if owner == "" || name == "" || num == 0 {
			m.toastMsg = "Could not parse PR URL"
			m.toastUntil = time.Now().Add(toastDuration)
			return m, tea.Tick(toastDuration, func(time.Time) tea.Msg {
				return clockTickMsg(time.Now())
			})
		}
		// Mutual exclusion — see the matching note on
		// viewRepoDetailMsg.
		m.repoDetail = m.repoDetail.Close()
		m.issueDetail = m.issueDetail.Close()
		m.scan = m.scan.Close()
		m.prDetail = m.prDetail.Open(msg.pr)
		return m, fetchPRDetailCmd(m.client, owner, name, num, msg.pr.URL)

	case prDetailFetchedMsg:
		// Stale-fetch protection by URL — same idiom as
		// repoDetailFetchedMsg.
		if !m.prDetail.IsOpen() || m.prDetail.pr.URL != msg.url {
			return m, nil
		}
		m.prDetail = m.prDetail.applyFetched(msg.detail, msg.err)
		return m, nil

	case viewIssueDetailMsg:
		owner, name, num := github.SplitOwnerNameNumber(msg.issue.URL)
		if owner == "" || name == "" || num == 0 {
			m.toastMsg = "Could not parse issue URL"
			m.toastUntil = time.Now().Add(toastDuration)
			return m, tea.Tick(toastDuration, func(time.Time) tea.Msg {
				return clockTickMsg(time.Now())
			})
		}
		// Mutual exclusion — see viewRepoDetailMsg note.
		m.repoDetail = m.repoDetail.Close()
		m.prDetail = m.prDetail.Close()
		m.scan = m.scan.Close()
		m.issueDetail = m.issueDetail.Open(msg.issue)
		return m, fetchIssueDetailCmd(m.client, owner, name, num, msg.issue.URL)

	case issueDetailFetchedMsg:
		if !m.issueDetail.IsOpen() || m.issueDetail.issue.URL != msg.url {
			return m, nil
		}
		m.issueDetail = m.issueDetail.applyFetched(msg.detail, msg.err)
		return m, nil

	case viewRepoScanMsg:
		// Integrity-scan drill-in (mirror of viewRepoDetailMsg). Parse
		// owner/name from the row URL, enforce one-drill-in-at-a-time,
		// open in loading state and fire the targeted scan.
		owner, name := github.SplitOwnerName(msg.repo.URL)
		if owner == "" || name == "" {
			m.toastMsg = "Could not parse repo URL"
			m.toastUntil = time.Now().Add(toastDuration)
			return m, tea.Tick(toastDuration, func(time.Time) tea.Msg {
				return clockTickMsg(time.Now())
			})
		}
		m.repoDetail = m.repoDetail.Close()
		m.prDetail = m.prDetail.Close()
		m.issueDetail = m.issueDetail.Close()
		// Hand the scan the repo list for its push-burst context, via
		// effectiveStats() rather than m.stats: public-only mode is
		// documented as screenshot-safe, and a burst derived from
		// private repos would disclose private push activity in the
		// report — as a count, but a disclosure either way. Running
		// without --public-only is how you opt into full coverage.
		var burstRepos []github.Repo
		if eff := m.effectiveStats(); eff != nil {
			burstRepos = eff.Repositories
		}
		// The baseline store sits beside whichever config file is in
		// use, so --config keeps its state together with it.
		blPath := config.BaselinePathFor(m.configPath)
		m.scan = m.scan.Open(msg.repo, burstRepos, blPath)
		return m, fetchRepoScanCmd(m.client, owner, name, msg.repo.URL, burstRepos, blPath)

	case repoScanFetchedMsg:
		// Stale-fetch protection by URL — same idiom as
		// repoDetailFetchedMsg.
		if !m.scan.IsOpen() || m.scan.repo.URL != msg.url {
			return m, nil
		}
		m.scan = m.scan.applyFetched(msg.scan, msg.err)
		return m, nil

	case pinToggledMsg:
		// Mutate the canonical pinned list, then persist back to
		// the config file if one was given at launch. Toast feedback
		// so the user knows the press registered even when the
		// row's visual position doesn't move (e.g. unpinning a
		// row that was already last in the pinned section).
		owner, name := github.SplitOwnerName(msg.url)
		if owner == "" || name == "" {
			// Defensive: the only way to land here with an
			// unparseable URL is a bug in the producer. Swallow
			// without changing state.
			return m, nil
		}
		key := owner + "/" + name
		nextPinned := togglePinList(m.pinned, key, msg.pin)
		// No-op when the requested state already matches — avoid
		// pointless disk writes and a misleading toast.
		if pinnedEqual(m.pinned, nextPinned) {
			return m, nil
		}
		m.pinned = nextPinned

		// Best-effort writeback through the shared read-modify-write
		// helper (persistConfig). A config-less launch (no path) keeps
		// the pin in-memory only — same trade-off the settings panel
		// makes for other keys. On any failure the on-disk file is left
		// untouched and the pin stays in memory only; surface that as a
		// toast. m.pinned was already updated above, so persistConfig
		// snapshots the new pin set.
		saveErr := ""
		if err := m.persistConfig(); err != nil {
			// Include the underlying error so the user can tell an
			// unreadable file (fix the TOML — hand-edits intact) from a
			// write failure (perms / disk). persistConfig returns the
			// distinguishing error from either config.Load or config.Save.
			saveErr = "config not saved (" + err.Error() + "), pin kept in memory only"
		}

		switch {
		case saveErr != "":
			m.toastMsg = saveErr
		case msg.pin:
			m.toastMsg = key + " pinned"
		default:
			m.toastMsg = key + " unpinned"
		}
		m.toastUntil = time.Now().Add(toastDuration)
		return m, tea.Tick(toastDuration, func(time.Time) tea.Msg {
			return clockTickMsg(time.Now())
		})

	case pinIssueToggledMsg:
		// Issues counterpart of pinToggledMsg. The id is already the
		// canonical "owner/name#N" identity (built by issueID), so
		// there's no URL parse here — the only difference from the repo
		// handler. Everything else mirrors it: mutate the canonical
		// list, no-op when unchanged, best-effort writeback, toast.
		next := togglePinList(m.pinnedIssues, msg.id, msg.pin)
		if pinnedEqual(m.pinnedIssues, next) {
			return m, nil
		}
		m.pinnedIssues = next

		saveErr := ""
		if err := m.persistConfig(); err != nil {
			saveErr = "config not saved (" + err.Error() + "), pin kept in memory only"
		}

		switch {
		case saveErr != "":
			m.toastMsg = saveErr
		case msg.pin:
			m.toastMsg = msg.id + " pinned"
		default:
			m.toastMsg = msg.id + " unpinned"
		}
		m.toastUntil = time.Now().Add(toastDuration)
		return m, tea.Tick(toastDuration, func(time.Time) tea.Msg {
			return clockTickMsg(time.Now())
		})

	case urlCopiedMsg:
		// Set the toast based on the outcome. The clipboard helper
		// failure path is rare in practice (macOS / Windows always
		// have one; Linux without xclip/xsel/wl-copy is an edge
		// case) but we surface a real reason rather than a generic
		// "failed" so the user knows what to install.
		noun := msg.noun
		if noun == "" {
			noun = "URL"
		}
		if msg.err != nil {
			m.toastMsg = "Copy " + noun + " failed: " + msg.err.Error()
		} else {
			m.toastMsg = noun + " copied"
		}
		m.toastUntil = time.Now().Add(toastDuration)
		return m, tea.Tick(toastDuration, func(time.Time) tea.Msg {
			return clockTickMsg(time.Now())
		})
	}

	return m, nil
}

// persistConfig writes the Model's current UI settings back to the
// config file using a read-modify-write cycle, and is the single
// writer every save path must route through. It re-Loads the on-disk
// config first so the one known key the Model doesn't track —
// watch_repos — survives the write; only the fields the in-app UI can
// change are overwritten.
//
// Scope of "preserved": config.Save rewrites the file from a fixed
// template, so this path preserves the KNOWN config keys (watch_repos
// in particular), NOT arbitrary unknown keys a user might add by hand.
// octoscope's config schema is closed — BurntSushi/toml ignores
// unknown keys on Load and they aren't re-emitted on Save — so there's
// nothing else to preserve.
//
// Returns nil when there is no config path (a config-less launch keeps
// settings in memory only) or on success. A non-nil error means the
// on-disk file was left UNTOUCHED: if the re-Load fails (malformed
// TOML, perms changed mid-session) we MUST NOT Save, or Save would
// overwrite the file with Defaults() plus our in-memory state,
// silently nuking the user's known keys (watch_repos / pinned_repos).
// Callers decide whether to surface the failure; the in-memory state
// is already updated either way.
//
// A *missing* file is NOT a re-Load failure — config.Load returns
// defaults with a nil error — so persistConfig will recreate the file
// from the current Model state. The only loss in that edge (config
// deleted mid-session) is a hand-edited watch_repos, which the removed
// file no longer carries; acceptable, since the user deleted it.
//
// Callers MUST update the live Model fields (m.interval, m.compact,
// m.client publicOnly, m.theme, m.accentColor, m.pinned) BEFORE calling
// this — it snapshots whatever the Model currently holds.
func (m *Model) persistConfig() error {
	if m.configPath == "" {
		return nil
	}
	cfgOnDisk, err := config.Load(m.configPath)
	if err != nil {
		return err
	}
	cfgOnDisk.RefreshInterval = m.interval
	cfgOnDisk.PublicOnly = m.client.PublicOnly()
	cfgOnDisk.Compact = m.compact
	// NO_COLOR / --no-color force the monochrome palette for this run,
	// so m.theme is "monochrome" — but that's an environment directive,
	// not the user's stored choice. Leave the file's theme / accent_color
	// keys exactly as Load read them so they survive unsetting NO_COLOR.
	// (Same hands-off treatment as ShowSponsor / WatchRepos below.)
	if !m.noColor {
		cfgOnDisk.Theme = m.theme
		cfgOnDisk.AccentColor = m.accentColor
	}
	cfgOnDisk.PinnedRepos = m.pinned
	cfgOnDisk.PinnedIssues = m.pinnedIssues
	// ShowSponsor is editable from the settings panel (#61), so the Model
	// tracks it and writes it back — no longer hands-off. (Unlike
	// theme/accent it isn't gated on noColor: NO_COLOR doesn't touch it.)
	cfgOnDisk.ShowSponsor = m.showSponsor
	// NOTE: WatchRepos is deliberately NOT touched — it's hand-edit
	// only (no runtime toggle), so cfgOnDisk keeps whatever Load read
	// from disk. Re-loading the known fields instead of building a
	// struct literal is the whole point: it's what stops watch_repos
	// from being silently dropped — the v0.x data-loss bug this helper
	// exists to prevent.
	return config.Save(m.configPath, cfgOnDisk)
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
	parsed, _ := m.settings.Refresh() // already parse-validated
	// Floor it so a panel-entered "0s" / "-1m" / "1ms" can neither
	// busy-loop the tick nor persist a bad value to disk.
	newInterval := config.NormalizeInterval(parsed)
	newCompact := m.settings.Compact()
	newPublicOnly := m.settings.PublicOnly()
	newTheme := m.settings.Theme()
	newAccent := m.settings.AccentColor()
	newShowSponsor := m.settings.ShowSponsor()

	intervalChanged := newInterval != m.interval
	themeChanged := newTheme != m.theme
	accentChanged := newAccent != m.accentColor

	m.interval = newInterval
	m.compact = newCompact
	m.showSponsor = newShowSponsor
	m.client.SetPublicOnly(newPublicOnly)
	if themeChanged || accentChanged {
		m.theme = newTheme
		m.accentColor = newAccent
		// Reapply the palette so a new theme or a new accent override
		// takes effect immediately (the accent slot alone when only the
		// override changed). Then re-foreground the spinner so its
		// colour tracks the possibly-changed accent.
		_ = applyTheme(newTheme, newAccent)
		m.spinner.Style = lipgloss.NewStyle().Foreground(colAccent)
	}

	// Persist via the shared read-modify-write helper so saving from
	// the settings panel preserves the known keys the Model doesn't
	// track — watch_repos in particular (see persistConfig's contract;
	// arbitrary unknown keys are not preserved, the schema is closed).
	// If the path is empty (no HOME / XDG_CONFIG_HOME
	// resolved) or the write fails, just stay quiet — the in-memory
	// state is already updated, and surfacing a "save failed" toast
	// right now would be more noise than value. A future release can
	// add a footer indicator for this if it ever bites. (The live
	// Model fields were all set above, so persistConfig snapshots the
	// new values.)
	_ = m.persistConfig()

	m.settings = m.settings.Close()

	// Compact, public-only, and theme switches all change the
	// rendered length / width math of the Overview tab. Re-sync so
	// the scroll viewports + footer hint reflect the new layout
	// without waiting for the next keystroke.
	syncOverviewViewport(m)
	syncActivityViewport(m)

	if intervalChanged {
		// Supersede the running chain: bump the generation so the old
		// chain's next tick is recognised as stale and dropped (it
		// self-terminates), and start one fresh chain at the new
		// cadence — immediate re-arm, no doubling.
		m.refreshGen++
		return tickCmd(newInterval, m.refreshGen)
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

// fetchCmd runs FetchStats (via fetchStatsWithRetry — 30s timeout per
// attempt, transient-5xx retry) and packs the result in a fetchMsg.
// Returning a command rather than calling directly keeps the network off
// BubbleTea's synchronous update loop.
func fetchCmd(client *github.Client, manual bool, gen int) tea.Cmd {
	return func() tea.Msg {
		stats, err := fetchStatsWithRetry(client)
		return fetchMsg{stats: stats, err: err, at: time.Now(), manual: manual, gen: gen}
	}
}

// fetchStatsRetries / fetchStatsBackoff bound the transient-5xx retry.
// The GraphQL gateway intermittently 502s the heavy dashboard query on
// busy accounts (the complexity-ceiling scar); those clear in moments,
// so a couple of quick retries keep the user on the loading spinner
// instead of bouncing them to the error screen on the first blip.
const (
	fetchStatsRetries = 3
	fetchStatsBackoff = 800 * time.Millisecond
)

// fetchStatsWithRetry calls FetchStats, retrying ONLY transient
// server/gateway errors (ReasonServer — 5xx, typically a 502) with
// backoff. Auth / rate-limit / network errors surface immediately (no
// point retrying those). A 5xx comes back fast, so the retries add only
// the backoff, not a full timeout each.
func fetchStatsWithRetry(client *github.Client) (*github.Stats, error) {
	return retryTransient(func(ctx context.Context) (*github.Stats, error) {
		return client.FetchStats(ctx)
	}, fetchStatsRetries, fetchStatsBackoff)
}

// retryTransient runs fetch up to `attempts` times, retrying ONLY a
// transient ReasonServer error (5xx — typically a 502) with `backoff`
// (doubled each round). Success and every other error class (auth,
// rate-limit, network/timeout, unknown) return immediately — retrying
// those is pointless. Each attempt gets its own 30s timeout: the
// dashboard fetch is content-heavy on busy accounts (~9s on a 74-repo
// profile), but a 5xx comes back fast, so retries cost only the backoff.
// (A 5xx that instead hangs to the per-attempt deadline isn't really
// transient; that's the worst case ~attempts×timeout, which also defers
// the next auto-refresh by that long — acceptable: don't pile refreshes
// onto a struggling gateway.) Extracted from fetchStatsWithRetry so the
// retry policy is unit-testable with a fake fetch.
func retryTransient(fetch func(context.Context) (*github.Stats, error), attempts int, backoff time.Duration) (*github.Stats, error) {
	var stats *github.Stats
	var err error
	for attempt := 1; attempt <= attempts; attempt++ {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		stats, err = fetch(ctx)
		cancel()
		if err == nil {
			return stats, nil
		}
		var fe *github.FetchError
		if !errors.As(err, &fe) || fe.Reason != github.ReasonServer {
			return stats, err
		}
		if attempt < attempts {
			time.Sleep(backoff)
			backoff *= 2
		}
	}
	return stats, err
}

// tickCmd is tea.Tick with a tickMsg envelope stamped with the chain
// generation `gen` so a superseded chain's tick can be recognised and
// dropped (see tickMsg / Model.refreshGen).
func tickCmd(d time.Duration, gen int) tea.Cmd {
	return tea.Tick(d, func(t time.Time) tea.Msg {
		return tickMsg{gen: gen}
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

// updateCheckInterval is how often the in-app update check polls the
// Releases API in-session, and the freshness window for the on-disk
// cache. Hourly is plenty for a release that ships every few days, and
// keeps the (free, unauthenticated-capable) endpoint usage negligible.
const updateCheckInterval = time.Hour

// updateCheckCmd performs one cache-aware update check off the Update
// loop. A cache fresher than updateCheckInterval is reused without any
// network call (so repeated short sessions don't re-poll); otherwise it
// fetches the latest release, persists the result, and reports it. The
// check is deliberately silent on failure — it falls back to whatever
// was cached and never surfaces an error, because a flaky update check
// must not nag the user.
func updateCheckCmd(client *github.Client) tea.Cmd {
	return func() tea.Msg {
		cached := update.LoadCache()
		if cached.Fresh(updateCheckInterval) {
			return updateCheckMsg{latest: cached.LatestTag}
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		latest, err := client.FetchLatestRelease(ctx)
		if err != nil || latest == "" {
			return updateCheckMsg{latest: cached.LatestTag}
		}
		_ = update.SaveCache(update.Cache{LastCheck: time.Now(), LatestTag: latest})
		return updateCheckMsg{latest: latest}
	}
}

// updateTickCmd schedules the next hourly update check. Fixed interval,
// single chain — no generation guard needed (see updateTickMsg).
func updateTickCmd() tea.Cmd {
	return tea.Tick(updateCheckInterval, func(t time.Time) tea.Msg {
		return updateTickMsg{}
	})
}
