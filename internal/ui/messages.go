package ui

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/gfazioli/octoscope/internal/clipboard"
	"github.com/gfazioli/octoscope/internal/github"
)

// This file collects the tea.Msg types the root model's Update loop
// dispatches on, plus the small Cmd constructors that build them. Split
// out of model.go (#60) purely to shrink the highest-churn file — no
// renames, no signature changes, no behaviour: the dispatcher and the
// model itself stay put.

// fetchMsg carries the outcome of a FetchStats call back to the
// model's Update loop. `manual` marks fetches that must NOT reschedule
// the auto-refresh tick (startup paint, manual `r`, settings save) — the
// timer chain reschedules itself, so only timer-origin fetches do.
type fetchMsg struct {
	stats  *github.Stats
	err    error
	at     time.Time
	manual bool
	// gen is the auto-refresh generation that originated this fetch. A
	// timer-origin fetch reschedules its NEXT tick under this captured
	// gen (not the model's current gen), so a fetch that was in flight
	// when an interval change bumped refreshGen reschedules a now-stale
	// tick that the guard drops — keeping exactly one chain. Unused for
	// manual fetches (they never reschedule).
	gen int
}

// tickMsg fires at `interval` and drives the next auto-refresh. It
// carries the generation it was scheduled under: an interval change
// bumps Model.refreshGen, so a tick from a superseded chain is ignored
// (and self-terminates) instead of running a second perpetual chain.
type tickMsg struct{ gen int }

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

// updateCheckMsg carries the latest octoscope release tag back from a
// (cache-aware) update check. It never carries an error: a failed
// check stays silent — surfacing "couldn't check for updates" would be
// noise. An empty latest just means "nothing to compare against".
type updateCheckMsg struct{ latest string }

// updateTickMsg fires on the slow (hourly) update-check chain, separate
// from the dashboard refresh tick. Fixed interval (it never changes),
// so unlike tickMsg it needs no generation guard — there's only ever
// one chain, started in Init when checkForUpdates is on.
type updateTickMsg struct{}

// ---------- Action menu cmds + msgs ----------

// showToastMsg requests an inline footer toast for the next ~2s.
// The action menu's Cmds emit this when the chosen action has no
// "real" side-effect yet (still wired to a stub) so the user gets
// visible confirmation that the keypress was registered.
type showToastMsg struct {
	text string
}

// viewRepoDetailMsg is fired by the "View details" menu entry on a
// Repos row. The root model intercepts it to switch into the
// drill-in view (Step 2). For now (Step 1) it falls through to a
// "coming soon" toast — the type is already in place so the wiring
// doesn't need to change when the detail view lands.
type viewRepoDetailMsg struct {
	repo github.Repo
}

// viewPRDetailMsg is fired by the "View details" menu entry on a
// PRs row (v0.11.0). Mirrors viewRepoDetailMsg.
type viewPRDetailMsg struct {
	pr github.PullRequest
}

// viewIssueDetailMsg is the Issues-side counterpart of
// viewPRDetailMsg / viewRepoDetailMsg.
type viewIssueDetailMsg struct {
	issue github.Issue
}

// viewGistDetailMsg is the Gists-side counterpart. It carries the whole
// row rather than just the hash so the drill-in can title itself while the
// fetch is still in flight.
type viewGistDetailMsg struct {
	gist github.Gist
}

// urlCopiedMsg fires after a copy-to-clipboard action — `err` is
// nil on success, non-nil when the clipboard helper failed
// (missing xclip/xsel on minimal Linux, headless X session, etc.).
// The root model picks the toast wording based on the outcome.
//
// `text` is the payload that was placed on the clipboard. The
// field name dropped "url" once copyPathCmd (v0.12.0) started
// reusing the message for file paths — keeping a misleading name
// invites bugs where someone reads msg.url and assumes a URL.
//
// `noun` lets the caller swap "URL" for whatever fits the
// payload: "Path" for file paths, etc. Empty string defaults to
// "URL" so existing call sites that don't care don't have to
// thread the field through.
type urlCopiedMsg struct {
	text string
	err  error
	noun string
}

// viewRepoDetailCmd builds a Cmd that asks the root model to open
// the drill-in detail for `r`. Captured at action-menu Open() time
// so the closure carries the relevant repo through the BubbleTea
// runtime tick — the menu itself stays oblivious to repo data.
func viewRepoDetailCmd(r github.Repo) tea.Cmd {
	return func() tea.Msg {
		return viewRepoDetailMsg{repo: r}
	}
}

// viewPRDetailCmd is the PRs-side counterpart of viewRepoDetailCmd:
// captures the row and asks the root to open the PR drill-in.
func viewPRDetailCmd(p github.PullRequest) tea.Cmd {
	return func() tea.Msg {
		return viewPRDetailMsg{pr: p}
	}
}

// viewIssueDetailCmd captures an Issues row for the action menu.
func viewIssueDetailCmd(it github.Issue) tea.Cmd {
	return func() tea.Msg {
		return viewIssueDetailMsg{issue: it}
	}
}

// viewGistDetailCmd captures a Gists row for the drill-in.
func viewGistDetailCmd(g github.Gist) tea.Cmd {
	return func() tea.Msg {
		return viewGistDetailMsg{gist: g}
	}
}

// copyURLCmd builds a Cmd that copies `url` to the system
// clipboard via the internal/clipboard helper (pbcopy on macOS,
// clip on Windows, wl-copy/xclip/xsel on Linux). Returns a
// urlCopiedMsg with the err field populated on failure so the
// root can decide whether to show "URL copied" or a one-line
// reason ("clipboard helper not found") in the footer toast.
// Thin wrapper around copyTextCmd; see copyPathCmd for the
// path-flavoured counterpart used by the v0.12.0 diff viewer.
func copyURLCmd(url string) tea.Cmd {
	return copyTextCmd(url, "URL")
}

// copyPathCmd is the file-path counterpart of copyURLCmd: same
// pipeline, different toast noun ("Path copied" instead of
// "URL copied"). Used by the PR diff viewer's files list (v0.12.0)
// where the clipboard payload is a repo-relative path rather than
// a URL.
func copyPathCmd(path string) tea.Cmd {
	return copyTextCmd(path, "Path")
}

// copyTextCmd is the shared underlying primitive. Captures the
// noun so the eventual toast reflects what was actually copied
// without each call site having to construct the urlCopiedMsg
// itself.
func copyTextCmd(text, noun string) tea.Cmd {
	return func() tea.Msg {
		err := clipboard.Copy(text)
		return urlCopiedMsg{text: text, err: err, noun: noun}
	}
}
