package ui

// Feed — the Activity tab's second sub-view (#71). The heatmap says how
// much; this says what.
//
// **Loaded on demand, not with the dashboard.** Every other list in the app
// rides FetchStats; this one does not, for two reasons that both come off
// the wire rather than out of taste. GitHub answers the events endpoint
// with `X-Poll-Interval: 60`, and octoscope's refresh floor is 5s — riding
// the refresh would poll it twelve times faster than asked, for a sub-tab
// the user may never open. And the endpoint has no `viewer` form, so it
// needs a login, which is only known *after* the profile query the fetch
// would have to run beside.
//
// The consequence is a state machine rather than a slice, which is why this
// file carries one and gists does not.

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/gfazioli/octoscope/internal/github"
)

// feedState tracks the on-demand load. feedIdle is the pre-open state: the
// distinction from feedReady-with-nothing matters, because "not asked yet"
// and "asked, and you have done nothing recently" are different sentences
// and the second one is a claim.
type feedState int

const (
	feedIdle feedState = iota
	feedLoading
	feedReady
	feedFailed
)

// FeedModel is the sub-view state. Cursor + search mirror the list tabs;
// state + err are what the on-demand load adds.
type FeedModel struct {
	state  feedState
	events []github.Event
	err    error
	reason github.FetchErrorReason

	// fetchedAt stamps the load so the header can say how stale the feed
	// is. The dashboard's own "updated Ns ago" says nothing about this
	// sub-view, which refreshes on its own schedule (i.e. when asked).
	fetchedAt time.Time

	cursor       int
	query        string
	searchActive bool
}

// IsInputMode reports whether the sub-model is absorbing keystrokes as
// text, so the root model can withhold its global hotkeys.
func (fm FeedModel) IsInputMode() bool { return fm.searchActive }

// NeedsLoad reports whether opening the sub-view should fire a fetch.
// False once anything has been attempted — a failed load is retried with
// `r`, not by looking at it again, so a broken token cannot turn tab
// switching into a request loop.
func (fm FeedModel) NeedsLoad() bool { return fm.state == feedIdle }

// startLoading is the transition into the in-flight state. Existing rows
// are deliberately kept: a reload should not blank a feed the user is
// reading, and if the reload fails the previous rows are still the best
// answer available.
func (fm FeedModel) startLoading() FeedModel {
	fm.state = feedLoading
	return fm
}

// loaded installs a successful result.
func (fm FeedModel) loaded(events []github.Event, at time.Time) FeedModel {
	fm.state = feedReady
	fm.events = events
	fm.err = nil
	fm.fetchedAt = at
	if fm.cursor >= len(events) {
		fm.cursor = 0
	}
	return fm
}

// failed installs an error. Rows from a previous successful load survive —
// see startLoading — so the view shows the stale feed with the failure
// stated above it rather than replacing information with an error.
func (fm FeedModel) failed(err error, reason github.FetchErrorReason) FeedModel {
	fm.state = feedFailed
	fm.err = err
	fm.reason = reason
	return fm
}

// visibleEvents is the single source of truth for the row pipeline —
// cursor, renderer and key actions all consume it, which is the invariant
// that keeps the highlighted row and the selected row the same row.
//
// **publicOnly is applied here, not in Stats.Public().** Events are not
// part of Stats (see the file comment), so the usual strip-every-private-
// list pass cannot reach them. The fetch already asks the `/events/public`
// endpoint when the client starts in public-only mode, but that covers
// startup and not the `p` toggle: flipping it mid-session has to filter
// what is already in memory, or a screenshot taken right after the toggle
// still shows private-repo activity.
func visibleEvents(events []github.Event, query string, publicOnly bool) []github.Event {
	out := make([]github.Event, 0, len(events))
	needle := strings.ToLower(strings.TrimSpace(query))
	for _, e := range events {
		if publicOnly && !e.IsPublic {
			continue
		}
		if needle != "" && !eventMatches(e, needle) {
			continue
		}
		out = append(out, e)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// eventMatches matches against what the row actually shows — repo, verb
// and subject — rather than the raw fields. Filtering on the raw type
// would make "merged" find nothing while the screen says merged.
func eventMatches(e github.Event, needle string) bool {
	verb, subject := eventPhrase(e)
	for _, hay := range []string{e.Repo, verb, subject} {
		if strings.Contains(strings.ToLower(hay), needle) {
			return true
		}
	}
	return false
}

// feedRow is one printed line: an event, plus how many adjacent events it
// stands for and the verbs folded into it.
type feedRow struct {
	event github.Event
	count int
	verb  string
}

// chatterTypes are the event types that a busy pull request produces in
// bulk. Everything else changes state and stays on its own line.
var chatterTypes = map[string]bool{
	"PullRequestReviewEvent":        true,
	"PullRequestReviewCommentEvent": true,
	"IssueCommentEvent":             true,
}

// isChatter reports whether an event is review/comment noise rather than a
// state change. An approval or a change-request is *not* chatter even
// though it arrives as a review — folding those into a "×12" is precisely
// how the one review that mattered disappears.
func isChatter(e github.Event) bool {
	if !chatterTypes[e.Type] {
		return false
	}
	if e.Type == "PullRequestReviewEvent" {
		return e.Action == "" || e.Action == "commented"
	}
	return true
}

// collapseChatter folds runs of review/comment traffic on the same subject
// into a single row.
//
// Without it the feed is a wall: on the account this was built against,
// twenty of the newest twenty-five events were reviews and comments on one
// pull request, and the tab answered "what happened recently" with the same
// line fifteen times. Reading it took longer than opening GitHub, which is
// the failure the Gists tab already taught this project once.
//
// **Only adjacent events merge, and only if they share repo and number.**
// Nothing is reordered and nothing jumps a row of a different kind, so the
// timeline the user reads is still the timeline GitHub sent — a run is
// folded, never a history rewritten. State changes (opened, merged, closed,
// pushed, released, approved) never fold, so the row that says what
// actually happened is always its own line.
func collapseChatter(events []github.Event) []feedRow {
	out := make([]feedRow, 0, len(events))
	for _, e := range events {
		verb, _ := eventPhrase(e)
		if n := len(out); n > 0 && e.Number != 0 &&
			isChatter(e) && isChatter(out[n-1].event) &&
			out[n-1].event.Repo == e.Repo && out[n-1].event.Number == e.Number {
			out[n-1].count++
			out[n-1].verb = mergeVerbs(out[n-1].verb, verb)
			continue
		}
		out = append(out, feedRow{event: e, count: 1, verb: verb})
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// mergeVerbs labels a folded run.
//
// A run of one verb keeps it. A mixed run becomes "commented", which is
// accurate rather than a compromise: everything foldable *is* a comment by
// construction — an issue comment, a review comment, or a review whose
// state is "commented". Approvals and change-requests are excluded from
// folding precisely so this collapse never swallows them.
//
// The earlier version joined the verbs with a slash, which produced
// "commented/reviewed" — eighteen characters that truncated to
// "commented/r…" in the column and said nothing the single word does not.
func mergeVerbs(existing, next string) string {
	if existing == next {
		return existing
	}
	return "commented"
}

// feedRows is the row pipeline: filter, then fold. Cursor, renderer and key
// actions all consume it, so the highlighted row and the selected row can
// never be different rows.
func feedRows(events []github.Event, query string, publicOnly bool) []feedRow {
	return collapseChatter(visibleEvents(events, query, publicOnly))
}

// selectedEvent returns the event under the cursor within the same
// pipeline the renderer used. For a folded row that is the newest event of
// the run, whose comment anchor is also the most useful place to land.
func (fm FeedModel) selectedEvent(publicOnly bool) (github.Event, bool) {
	rows := feedRows(fm.events, fm.query, publicOnly)
	if len(rows) == 0 {
		return github.Event{}, false
	}
	idx := fm.cursor
	if idx < 0 {
		idx = 0
	}
	if idx >= len(rows) {
		idx = len(rows) - 1
	}
	return rows[idx].event, true
}

// Update handles keys while the feed sub-view is showing.
func (fm FeedModel) Update(msg tea.KeyMsg, publicOnly bool) (FeedModel, tea.Cmd) {
	if fm.searchActive {
		return fm.updateSearch(msg), nil
	}

	rows := feedRows(fm.events, fm.query, publicOnly)
	n := len(rows)

	// Clamp before acting, not only when rendering. The row set shrinks
	// underneath the cursor whenever public-only is toggled or a filter is
	// typed, and a cursor left past the end would then step *down* from an
	// index that no longer exists — the renderer's own clamp hides that on
	// screen while enter opens the wrong row.
	if fm.cursor >= n {
		fm.cursor = n - 1
	}
	if fm.cursor < 0 {
		fm.cursor = 0
	}

	switch msg.String() {
	case "up", "k":
		if fm.cursor > 0 {
			fm.cursor--
		}
	case "down", "j":
		if fm.cursor < n-1 {
			fm.cursor++
		}
	case "pgup":
		fm.cursor -= feedPageJump
		if fm.cursor < 0 {
			fm.cursor = 0
		}
	case "pgdown", " ":
		// Space pages down here for the same reason it does on the heatmap
		// half — the two sub-views share a tab, and a key that scrolls one
		// and does nothing on the other reads as a bug.
		fm.cursor += feedPageJump
		if fm.cursor > n-1 {
			fm.cursor = n - 1
		}
		if fm.cursor < 0 {
			fm.cursor = 0
		}
	case "home", "g":
		fm.cursor = 0
	case "end", "G":
		if n > 0 {
			fm.cursor = n - 1
		}
	case "/":
		fm.searchActive = true
	case "enter", "o":
		if n == 0 || fm.cursor >= n {
			return fm, nil
		}
		return fm, openURLCmd(rows[fm.cursor].event.URL)
	case "c":
		if n == 0 || fm.cursor >= n {
			return fm, nil
		}
		return fm, copyURLCmd(rows[fm.cursor].event.URL)
	case "esc":
		if fm.query != "" {
			fm.query = ""
			fm.cursor = 0
		}
	}
	return fm, nil
}

// feedPageJump is how far pgup/pgdn move the cursor. A fixed jump rather
// than a screenful: the row budget is computed inside the renderer from a
// height this model does not have, and a page key that moves a plausible
// distance beats one that needs the two to be threaded together.
const feedPageJump = 10

func (fm FeedModel) updateSearch(km tea.KeyMsg) FeedModel {
	// Dispatch on km.Type so pasted batches arrive whole — same reason as
	// ReposModel.updateSearch.
	switch km.Type {
	case tea.KeyEnter:
		fm.searchActive = false
		fm.cursor = 0
	case tea.KeyEsc:
		fm.searchActive = false
		fm.query = ""
		fm.cursor = 0
	case tea.KeyBackspace:
		if r := []rune(fm.query); len(r) > 0 {
			fm.query = string(r[:len(r)-1])
			fm.cursor = 0
		}
	case tea.KeyRunes, tea.KeySpace:
		fm.query += sanitizeFilterInput(string(km.Runes))
		fm.cursor = 0
	}
	return fm
}

// eventPhrase turns an event's raw facts into the two cells the row shows:
// a verb and the subject it applies to.
//
// Phrasing lives here rather than in the github package on purpose — a
// display string built at the fetch boundary would end up in machine-
// readable output as a value GitHub never sent. Same rule Gist.Description
// follows.
//
// The subject for a pull request is a bare "PR #123" with no title, and
// that is not an omission: the events feed truncates payload.pull_request
// to five fields and a title is not among them. Issues, which arrive whole,
// do get theirs. Fetching the missing titles would be one request per row.
func eventPhrase(e github.Event) (verb, subject string) {
	switch e.Type {
	case "PushEvent":
		return "pushed", e.Ref

	case "PullRequestEvent":
		v := e.Action
		if v == "" {
			v = "updated"
		}
		return v, subjectLabel(true, e.Number, e.Title)

	case "PullRequestReviewEvent":
		// Action carries the review *state* for this type (see the field's
		// doc comment): an approval is the most significant thing a review
		// row can say, and it would be invisible under a flat "reviewed".
		switch e.Action {
		case "approved":
			return "approved", subjectLabel(true, e.Number, e.Title)
		case "changes_requested":
			return "changes req", subjectLabel(true, e.Number, e.Title)
		case "dismissed":
			return "dismissed", subjectLabel(true, e.Number, e.Title)
		}
		return "reviewed", subjectLabel(true, e.Number, e.Title)

	case "PullRequestReviewCommentEvent":
		return "commented", subjectLabel(true, e.Number, e.Title)

	case "IssuesEvent":
		v := e.Action
		if v == "" {
			v = "updated"
		}
		return v, subjectLabel(e.IsPullRequest, e.Number, e.Title)

	case "IssueCommentEvent":
		// GitHub files pull-request comments under this type too, with the
		// PR in the `issue` field — which is why this one *does* have a
		// title where PullRequestEvent does not.
		return "commented", subjectLabel(e.IsPullRequest, e.Number, e.Title)

	case "CommitCommentEvent":
		return "commented", "on a commit"

	case "CreateEvent":
		if e.RefType == "repository" {
			return "created", "the repository"
		}
		return "created", strings.TrimSpace(e.RefType + " " + e.Ref)

	case "DeleteEvent":
		return "deleted", strings.TrimSpace(e.RefType + " " + e.Ref)

	case "ReleaseEvent":
		v := e.Action
		if v == "published" || v == "" {
			v = "released"
		}
		return v, e.Ref

	case "WatchEvent":
		return "starred", ""

	case "ForkEvent":
		return "forked", ""

	case "PublicEvent":
		return "made public", ""

	case "MemberEvent":
		v := e.Action
		if v == "" {
			v = "changed"
		}
		return v, "a collaborator"

	case "GollumEvent":
		return "edited", "the wiki"

	case "SponsorshipEvent":
		v := e.Action
		if v == "" {
			v = "changed"
		}
		return v, "a sponsorship"
	}

	// Unknown type. GitHub adds these, and a row that says nothing in the
	// right repo at the right time is far better than a dropped one.
	return humanizeEventType(e.Type), strings.TrimSpace(e.Action)
}

// subjectLabel names the numbered thing an event is about.
//
// The "PR" prefix is not decoration: "commented #123" is ambiguous where
// "commented PR #123" is not, and Event.IsPullRequest is what makes the
// distinction available at all — GitHub files PR comments under the issue
// shape, so the event type cannot answer it.
//
// The title is appended when there is one. For a pull request that is
// usually a title borrowed from a sibling event in the same page (see
// backfillTitles); when the page did not contain one, the bare number is
// what gets shown, because inventing a label is worse than a short row.
func subjectLabel(isPR bool, number int, title string) string {
	prefix := ""
	if isPR {
		prefix = "PR "
	}
	switch {
	case number <= 0 && title == "":
		if isPR {
			return "a pull request"
		}
		return ""
	case number <= 0:
		return title
	case title == "":
		return fmt.Sprintf("%s#%d", prefix, number)
	}
	return fmt.Sprintf("%s#%d %s", prefix, number, title)
}

// humanizeEventType turns "SomeFutureEvent" into "some future" — GitHub's
// own noun, split on the camel humps and stripped of the suffix every type
// shares.
func humanizeEventType(t string) string {
	t = strings.TrimSuffix(t, "Event")
	if t == "" {
		return "did something"
	}
	var b strings.Builder
	for i, r := range t {
		if i > 0 && r >= 'A' && r <= 'Z' {
			b.WriteByte(' ')
		}
		b.WriteRune(r)
	}
	return strings.ToLower(b.String())
}

// renderFeed draws the whole sub-view: header, optional filter line, the
// table, and the hint line.
func (fm FeedModel) renderFeed(available, availableHeight int, publicOnly bool) string {
	switch fm.state {
	case feedIdle:
		return mutedStyle.Render("(loading recent activity…)")
	case feedLoading:
		if len(fm.events) == 0 {
			return mutedStyle.Render("(loading recent activity…)")
		}
	case feedFailed:
		if len(fm.events) == 0 {
			return strings.Join([]string{
				errorStyle.Render("Recent activity could not be loaded."),
				mutedStyle.Render(feedErrorHint(fm.reason)),
				"",
				keyHints("r", "retry"),
			}, "\n")
		}
	}

	rows := feedRows(fm.events, fm.query, publicOnly)
	if len(rows) == 0 {
		if fm.query != "" {
			return strings.Join([]string{
				fm.renderHeaderLine(0, 0, publicOnly),
				"",
				mutedStyle.Render(fmt.Sprintf("(nothing matches %q — esc to clear)", fm.query)),
			}, "\n")
		}
		if publicOnly && len(fm.events) > 0 {
			return mutedStyle.Render("(every recent event happened in a private repository — " +
				"press p to leave public-only mode)")
		}
		return mutedStyle.Render("(no recent activity in the window GitHub keeps)")
	}

	cursor := fm.cursor
	if cursor >= len(rows) {
		cursor = len(rows) - 1
	}
	if cursor < 0 {
		cursor = 0
	}

	// Chrome measured, not estimated: header, blank, table header, rule,
	// blank, window note, blank, hints.
	const feedChrome = 8
	chrome := feedChrome
	if fm.searchActive || fm.query != "" {
		chrome++
	}
	if fm.state == feedFailed {
		chrome++
	}

	rowsVisible := len(rows)
	if availableHeight > 0 {
		rowsVisible = availableHeight - chrome
		if rowsVisible < 3 && availableHeight-chrome >= 3 {
			rowsVisible = 3
		}
		if rowsVisible < 1 {
			rowsVisible = 1
		}
		if rowsVisible > len(rows) {
			rowsVisible = len(rows)
		}
	}

	offset := 0
	if len(rows) > rowsVisible {
		offset = cursor - rowsVisible/2
		if offset < 0 {
			offset = 0
		}
		if offset > len(rows)-rowsVisible {
			offset = len(rows) - rowsVisible
		}
	}
	end := offset + rowsVisible
	if end > len(rows) {
		end = len(rows)
	}

	parts := []string{fm.renderHeaderLine(countEvents(rows), len(rows), publicOnly)}

	if fm.state == feedFailed {
		parts = append(parts, warnStyle.Render("reload failed — showing the previous load. ")+
			mutedStyle.Render(feedErrorHint(fm.reason)))
	}

	switch {
	case fm.searchActive:
		parts = append(parts, mutedStyle.Render("search: ")+
			fm.query+boldStyle.Foreground(colAccent).Render("█")+
			mutedStyle.Render("   (enter confirm · esc cancel)"))
	case fm.query != "":
		parts = append(parts, mutedStyle.Render("filter: ")+fm.query+
			mutedStyle.Render("   (esc to clear)"))
	}

	parts = append(parts, "", renderFeedTable(rows[offset:end], cursor-offset, available, anyPrivate(rows)))

	if note := fm.windowNote(rows, offset, end); note != "" {
		parts = append(parts, "", note)
	}

	parts = append(parts, "", keyHints(
		"↑↓", "move",
		"g/G", "top/bottom",
		"/", "search",
		"enter", "github",
		"c", "copy",
		"r", "reload",
	))
	return strings.Join(parts, "\n")
}

// windowNote states the span the feed actually covers, which is the honest
// version of what a timeline implies. GitHub keeps a limited window of
// events and we ask for one page of it — on a busy account that page was
// **two days**, so a reader who assumed "recent activity" meant weeks would
// be reading a list that silently stops.
func (fm FeedModel) windowNote(rows []feedRow, offset, end int) string {
	if len(rows) == 0 {
		return ""
	}
	newest, oldest := rows[0].event.CreatedAt, rows[len(rows)-1].event.CreatedAt

	var b strings.Builder
	if end-offset < len(rows) {
		b.WriteString(mutedStyle.Render(fmt.Sprintf("rows %d-%d of %d   ", offset+1, end, len(rows))))
	}
	if !newest.IsZero() && !oldest.IsZero() {
		b.WriteString(mutedStyle.Render("covering " +
			oldest.Local().Format("Jan 2 15:04") + " → " +
			newest.Local().Format("Jan 2 15:04")))
	}
	// Only claim the window is capped when it actually is. Below the cap
	// the feed *is* everything GitHub has, and saying otherwise would be
	// the same kind of unfounded hedge the scan's disclosures avoid.
	if len(fm.events) >= github.EventsPageSize {
		b.WriteString(mutedStyle.Render(fmt.Sprintf(
			" · the last %d events, not a full history", github.EventsPageSize)))
	}
	return b.String()
}

// renderHeaderLine counts *events*, not rows — folding is a presentation
// choice and "17 events" after collapsing 100 of them would be a false
// count. shown is how many survived the filter; rows is how many lines
// they occupy, named only when the two differ.
func (fm FeedModel) renderHeaderLine(shown, rows int, publicOnly bool) string {
	label := fmt.Sprintf("%d event", shown)
	if shown != 1 {
		label = fmt.Sprintf("%d events", shown)
	}
	if fm.query != "" && shown != len(fm.events) {
		label = fmt.Sprintf("%d of %d events", shown, len(fm.events))
	}
	if rows > 0 && rows < shown {
		label += fmt.Sprintf(" in %d rows", rows)
	}

	out := sectionTitleStyle.Render(label)
	if publicOnly {
		out += mutedStyle.Render("   public only")
	}
	if fm.state == feedLoading {
		out += mutedStyle.Render("   reloading…")
	} else if !fm.fetchedAt.IsZero() {
		out += mutedStyle.Render("   loaded " + formatRelativeAgo(fm.fetchedAt))
	}
	return out
}

// feedErrorHint turns a classified failure into the one sentence that says
// what to do about it. Same vocabulary the footer uses for the dashboard's
// own errors, so a token problem reads the same wherever it surfaces.
func feedErrorHint(reason github.FetchErrorReason) string {
	switch reason {
	case github.ReasonAuth:
		return "GitHub rejected the token — it may be expired or revoked."
	case github.ReasonAuthScope:
		return "The token cannot read this account's events."
	case github.ReasonNotFound:
		return "GitHub has no events endpoint for that account."
	case github.ReasonRateLimitPrimary, github.ReasonRateLimitSecondary:
		return "Rate-limited — try again shortly."
	case github.ReasonNetwork:
		return "The request did not reach GitHub."
	case github.ReasonServer:
		return "GitHub answered with a server error."
	}
	return "Press r to try again."
}

const (
	feedCursorW = 2
	feedWhenW   = 9
	feedVisW    = 8
	feedVerbW   = 12
	feedCountW  = 5
	feedRepoW   = 32
	// feedSubjectMin keeps the subject column readable rather than letting
	// a narrow terminal collapse it to nothing — below this the repo column
	// gives ground first, since the subject is the part that varies.
	feedSubjectMin = 16
)

// countEvents totals the events behind a set of rows, so the header can
// report events while the table shows folded lines.
func countEvents(rows []feedRow) int {
	n := 0
	for _, r := range rows {
		n += r.count
	}
	return n
}

// anyPrivate reports whether any visible row happened in a private repo,
// which is what decides whether the visibility column is worth its width.
func anyPrivate(rows []feedRow) bool {
	for _, r := range rows {
		if !r.event.IsPublic {
			return true
		}
	}
	return false
}

// renderFeedTable draws the rows.
//
// showVis is passed rather than derived per-row: the column is dropped
// entirely when every visible row is public, which is always the case under
// --public-only. A column of identical values is eight characters of noise
// taken from the subject, which is the one column that always varies.
func renderFeedTable(rows []feedRow, cursorRow, available int, showVis bool) string {
	repoW, subjectW := feedColumnWidths(available, showVis)

	headerCells := []string{
		strings.Repeat(" ", feedCursorW),
		mutedStyle.Render(padRight("When", feedWhenW)),
	}
	if showVis {
		headerCells = append(headerCells, mutedStyle.Render(padRight("Vis", feedVisW)))
	}
	headerCells = append(headerCells,
		mutedStyle.Render(padRight("What", feedVerbW)),
		mutedStyle.Render(padRight("", feedCountW)),
		mutedStyle.Render(padRight("Repository", repoW)),
		mutedStyle.Render(padRight("Subject", subjectW)),
	)
	header := strings.Join(headerCells, "  ")
	rule := tabRuleStyle.Render(strings.Repeat("─", lipgloss.Width(header)))

	out := []string{header, rule}
	for i, r := range rows {
		active := i == cursorRow
		e := r.event
		_, subject := eventPhrase(e)

		marker := "  "
		if active {
			marker = activeTabStyle.Render("▸ ")
		}

		when := padRight(formatRelativeAgo(e.CreatedAt), feedWhenW)
		verbCell := padRightRaw(styleEventVerb(truncate(r.verb, feedVerbW), e), feedVerbW)

		count := ""
		if r.count > 1 {
			count = fmt.Sprintf("×%d", r.count)
		}
		countCell := padRight(count, feedCountW)

		repo := padRight(truncate(e.Repo, repoW), repoW)
		subjectCell := padRight(truncate(subject, subjectW), subjectW)

		if active {
			repo = boldStyle.Foreground(colAccent).Render(repo)
		} else {
			repo = valueStyle.Render(repo)
			when = mutedStyle.Render(when)
			subjectCell = mutedStyle.Render(subjectCell)
		}
		countCell = mutedStyle.Render(countCell)

		cells := []string{marker + when}
		if showVis {
			vis := "public"
			if !e.IsPublic {
				vis = "private"
			}
			visCell := padRight(vis, feedVisW)
			if !active {
				visCell = mutedStyle.Render(visCell)
			}
			cells = append(cells, visCell)
		}
		cells = append(cells, verbCell, countCell, repo, subjectCell)
		out = append(out, strings.Join(cells, "  "))
	}
	return strings.Join(out, "\n")
}

// feedColumnWidths splits the leftover width between repo and subject. The
// fixed columns are known, so this is arithmetic rather than guesswork —
// and it degrades by shrinking the repo name, which stays recognisable
// truncated, before the subject, which does not.
func feedColumnWidths(available int, showVis bool) (repoW, subjectW int) {
	cols := []int{feedCursorW, feedWhenW, feedVerbW, feedCountW}
	if showVis {
		cols = append(cols, feedVisW)
	}
	fixed := 0
	for _, c := range cols {
		fixed += c
	}
	// One two-space separator between every pair of columns, including the
	// two adaptive ones.
	fixed += (len(cols) + 1) * 2

	leftover := available - fixed
	if leftover < feedRepoW+feedSubjectMin {
		// Too narrow for both at full size: give the subject its floor and
		// let the repo take what is left, with its own floor so the column
		// can never come out negative and panic strings.Repeat.
		repoW = leftover - feedSubjectMin
		if repoW < 10 {
			repoW = 10
		}
		return repoW, feedSubjectMin
	}
	return feedRepoW, leftover - feedRepoW
}

// styleEventVerb colours the verb by what it means rather than by type, so
// the column reads as a status at a glance: things that landed are green,
// a rejection or an unmerged close is a warning, things that went away are
// muted, and what just opened takes the accent.
func styleEventVerb(verb string, e github.Event) string {
	switch {
	case verb == "merged" || verb == "released" || verb == "approved":
		return okStyle.Render(verb)
	case verb == "changes req":
		return warnStyle.Render(verb)
	case verb == "closed" && e.Type == "PullRequestEvent":
		// A closed PR was not merged — that is the whole distinction, and
		// GitHub sends "merged" as its own action in this feed.
		return warnStyle.Render(verb)
	case verb == "deleted":
		return mutedStyle.Render(verb)
	case verb == "opened" || verb == "reopened":
		return accentStyle.Render(verb)
	}
	return verb
}

// feedLoadedMsg carries a FetchEvents round-trip back to Update.
//
// It has no discriminator for staleness — unlike the drill-ins, which key
// theirs by URL. There is only ever one feed and one account, so a second
// response cannot belong to a different subject; the worst a late reply
// does is re-install the same rows.
type feedLoadedMsg struct {
	events []github.Event
	err    error
	reason github.FetchErrorReason
	at     time.Time
}

// fetchEventsCmd runs the events request off the UI goroutine. The 10s
// timeout matches the rate-limit panel's: long enough for a slow network,
// short enough that a hung request does not leave the sub-view saying
// "loading" forever.
func fetchEventsCmd(client *github.Client, login string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		events, err := client.FetchEvents(ctx, login)
		msg := feedLoadedMsg{events: events, err: err, at: time.Now()}
		if err != nil {
			var fe *github.FetchError
			if errors.As(err, &fe) {
				msg.reason = fe.Reason
			}
		}
		return msg
	}
}

// feedLogin is the account whose events to ask for.
//
// The events endpoint has no `viewer` form, so this needs the resolved
// login — which lives on the fetched profile. Before the first successful
// FetchStats it is empty, and the caller treats that as "not yet": the load
// is simply not fired, and the next entry into the sub-tab tries again.
// Reading it from stats rather than from the client is also what makes the
// feed follow `--login <someone>` to that person's public events.
func (m Model) feedLogin() string {
	if m.stats == nil {
		return ""
	}
	return m.stats.Login
}

// maybeLoadFeed fires the on-demand fetch when the feed is the visible
// sub-view and has never been loaded. Safe to call on every tab switch:
// NeedsLoad goes false the moment a load starts, so repeated switching
// cannot stack requests, and a failed load waits for `r` rather than
// retrying on sight.
func (m Model) maybeLoadFeed() (Model, tea.Cmd) {
	if m.activeTab != TabActivity || m.activitySub != ActivitySubFeed {
		return m, nil
	}
	if !m.feed.NeedsLoad() {
		return m, nil
	}
	login := m.feedLogin()
	if login == "" {
		return m, nil
	}
	m.feed = m.feed.startLoading()
	return m, fetchEventsCmd(m.client, login)
}

// switchActivitySub moves between the heatmap and the feed, loading the
// latter the first time it is shown.
func (m Model) switchActivitySub(to ActivitySub) (Model, tea.Cmd) {
	m.activitySub = to
	return m.maybeLoadFeed()
}
