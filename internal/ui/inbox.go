package ui

// Inbox — the real GitHub notification inbox (#74).
//
// **Loaded on demand**, like the Activity feed and for the same measured
// reason: the endpoint answers with `X-Poll-Interval: 60` while octoscope's
// `--refresh` floor is 5s. The issue asked for a feature flag so this would
// not be "folded silently into the refresh"; on-demand loading is the
// stronger version of that request — nothing reaches `/notifications`
// unless the tab is opened, so a flag would guard something that already
// does not run.
//
// **Read-only.** Marking a thread read is a `PATCH`, and octoscope does not
// mutate GitHub state. The issue named the alternative — an
// `--allow-mutations` opt-in — and it was declined: that flag makes this a
// product with a different trust model, which is not a decision to take
// inside a release cycle. So the inbox links out, and the hint line says
// so rather than leaving the user to wonder where `x` is.
//
// **Viewer-only.** The endpoint has no login parameter — it is always the
// caller's inbox — so under `octoscope <someone>` it would show *your*
// notifications beneath *their* profile. The tab says that instead.

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

// inboxState mirrors feedState: "not asked yet" and "asked, and there is
// nothing" are different sentences and only one of them is a claim.
type inboxState int

const (
	inboxIdle inboxState = iota
	inboxLoading
	inboxReady
	inboxFailed
)

// InboxFilter cuts the wall of CI noise down to something readable.
//
// It exists because of a measurement, not a preference: of 104
// notifications on a real account, **77 were ci_activity** — three
// quarters of an inbox saying a workflow succeeded. Showing everything by
// default is still right (an inbox that hides things silently is worse
// than a noisy one), but a surface with no way through the noise is the
// Gists lesson again.
type InboxFilter int

const (
	// InboxAll is the default: everything GitHub considers unread.
	InboxAll InboxFilter = iota
	// InboxInvolving keeps the reasons that name *you* specifically.
	InboxInvolving
	// InboxCI is the complement, for when the build is what you came for.
	InboxCI
)

var inboxFilterLabels = [...]string{
	InboxAll:       "all",
	InboxInvolving: "involving you",
	InboxCI:        "ci",
}

// involvingReasons are the reasons that put a thread in front of a person
// rather than in front of a subscription. GitHub's vocabulary, verbatim.
var involvingReasons = map[string]bool{
	"mention":          true,
	"team_mention":     true,
	"review_requested": true,
	"assign":           true,
	"author":           true,
	"security_alert":   true,
}

func (f InboxFilter) next() InboxFilter {
	return (f + 1) % InboxFilter(len(inboxFilterLabels))
}

// keeps reports whether a notification survives this filter.
func (f InboxFilter) keeps(n github.Notification) bool {
	switch f {
	case InboxInvolving:
		return involvingReasons[n.Reason]
	case InboxCI:
		return n.Reason == "ci_activity"
	}
	return true
}

// InboxModel is the tab's state.
type InboxModel struct {
	state  inboxState
	items  []github.Notification
	err    error
	reason github.FetchErrorReason

	// gen counts loads, so a superseded reply cannot overwrite a newer
	// one. Same shape as FeedModel.gen, and it is here from the start
	// rather than after a reviewer found the race on #124.
	gen int

	fetchedAt time.Time

	cursor       int
	filter       InboxFilter
	query        string
	searchActive bool
}

// IsInputMode reports whether keystrokes are literal filter text.
func (im InboxModel) IsInputMode() bool { return im.searchActive }

// NeedsLoad is true until something has been attempted. A failed load waits
// for `r` rather than retrying every time the tab is looked at.
func (im InboxModel) NeedsLoad() bool { return im.state == inboxIdle }

func (im InboxModel) startLoading() InboxModel {
	im.gen++
	im.state = inboxLoading
	return im
}

// loaded installs a result. Existing rows survive a failed reload — see
// failed — so a network blip does not blank an inbox being read.
func (im InboxModel) loaded(items []github.Notification, at time.Time) InboxModel {
	im.state = inboxReady
	im.items = items
	im.err = nil
	im.fetchedAt = at
	if im.cursor >= len(items) {
		im.cursor = 0
	}
	return im
}

func (im InboxModel) failed(err error, reason github.FetchErrorReason) InboxModel {
	im.state = inboxFailed
	im.err = err
	im.reason = reason
	return im
}

// visibleInbox is the single source of truth for the row pipeline: cursor,
// renderer and key actions all consume it.
//
// **publicOnly is applied here.** Notifications are not part of Stats — the
// tab loads on demand — so Stats.Public() cannot reach them. Unlike
// sponsors this is a real per-item flag (17 of 104 measured), so it is a
// skip loop rather than dropping the surface.
func visibleInbox(items []github.Notification, f InboxFilter, query string, publicOnly bool) []github.Notification {
	needle := strings.ToLower(strings.TrimSpace(query))
	out := make([]github.Notification, 0, len(items))
	for _, n := range items {
		if publicOnly && n.IsPrivate {
			continue
		}
		if !f.keeps(n) {
			continue
		}
		if needle != "" && !inboxMatches(n, needle) {
			continue
		}
		out = append(out, n)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// inboxMatches matches what the row shows — repo, title and the phrased
// reason — rather than the raw fields, so "review" finds a review request
// whose stored reason is "review_requested".
func inboxMatches(n github.Notification, needle string) bool {
	for _, hay := range []string{n.Repo, n.Title, inboxReason(n), n.Type} {
		if strings.Contains(strings.ToLower(hay), needle) {
			return true
		}
	}
	return false
}

// inboxReason turns GitHub's snake_case vocabulary into something readable
// without inventing meaning: the underscores go, nothing else.
func inboxReason(n github.Notification) string {
	if n.Reason == "" {
		return "—"
	}
	return strings.ReplaceAll(n.Reason, "_", " ")
}

func (im InboxModel) selected(publicOnly bool) (github.Notification, bool) {
	rows := visibleInbox(im.items, im.filter, im.query, publicOnly)
	if len(rows) == 0 {
		return github.Notification{}, false
	}
	idx := im.cursor
	if idx < 0 {
		idx = 0
	}
	if idx >= len(rows) {
		idx = len(rows) - 1
	}
	return rows[idx], true
}

const inboxPageJump = 10

// Update handles keys while the Inbox tab is active.
func (im InboxModel) Update(msg tea.KeyMsg, publicOnly bool) (InboxModel, tea.Cmd) {
	if im.searchActive {
		return im.updateSearch(msg), nil
	}

	rows := visibleInbox(im.items, im.filter, im.query, publicOnly)
	n := len(rows)

	// Clamp before acting: the row set shrinks underneath the cursor
	// whenever the filter cycles or public-only is toggled, and a stale
	// index would open a different row than the one highlighted.
	if im.cursor >= n {
		im.cursor = n - 1
	}
	if im.cursor < 0 {
		im.cursor = 0
	}

	switch msg.String() {
	case "up", "k":
		if im.cursor > 0 {
			im.cursor--
		}
	case "down", "j":
		if im.cursor < n-1 {
			im.cursor++
		}
	case "pgup":
		im.cursor -= inboxPageJump
		if im.cursor < 0 {
			im.cursor = 0
		}
	case "pgdown":
		// No space here: on a list tab space opens the action menu, and
		// the root model claims it before this ever runs. Binding it to
		// a page-down would be dead code that reads as a feature.
		im.cursor += inboxPageJump
		if im.cursor > n-1 {
			im.cursor = n - 1
		}
		if im.cursor < 0 {
			im.cursor = 0
		}
	case "home", "g":
		im.cursor = 0
	case "end", "G":
		if n > 0 {
			im.cursor = n - 1
		}
	case "s":
		im.filter = im.filter.next()
		im.cursor = 0
	case "/":
		im.searchActive = true
	case "enter", "o":
		if n == 0 || im.cursor >= n {
			return im, nil
		}
		return im, openURLCmd(rows[im.cursor].URL)
	case "c":
		if n == 0 || im.cursor >= n {
			return im, nil
		}
		return im, copyURLCmd(rows[im.cursor].URL)
	case "esc":
		if im.query != "" {
			im.query = ""
			im.cursor = 0
		}
	}
	return im, nil
}

func (im InboxModel) updateSearch(km tea.KeyMsg) InboxModel {
	switch km.Type {
	case tea.KeyEnter:
		im.searchActive = false
		im.cursor = 0
	case tea.KeyEsc:
		im.searchActive = false
		im.query = ""
		im.cursor = 0
	case tea.KeyBackspace:
		if r := []rune(im.query); len(r) > 0 {
			im.query = string(r[:len(r)-1])
			im.cursor = 0
		}
	case tea.KeyRunes, tea.KeySpace:
		im.query += sanitizeFilterInput(string(km.Runes))
		im.cursor = 0
	}
	return im
}

// inboxLoadedMsg carries a FetchNotifications round-trip back to Update.
type inboxLoadedMsg struct {
	gen    int
	items  []github.Notification
	err    error
	reason github.FetchErrorReason
	at     time.Time
}

func fetchNotificationsCmd(client *github.Client, gen int) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		items, err := client.FetchNotifications(ctx)
		msg := inboxLoadedMsg{gen: gen, items: items, err: err, at: time.Now()}
		if err != nil {
			var fe *github.FetchError
			if errors.As(err, &fe) {
				msg.reason = fe.Reason
			}
		}
		return msg
	}
}

const (
	inboxCursorW  = 2
	inboxWhenW    = 9
	inboxReasonW  = 17
	inboxRepoW    = 28
	inboxTitleMin = 20
)

// renderInboxTab draws the whole tab.
//
// viewer is false when octoscope is showing somebody else's profile, and
// the tab says so instead of drawing the caller's own inbox under their
// name — the endpoint has no login parameter, so there is no such thing as
// "their inbox" to fetch.
func (im InboxModel) renderInboxTab(viewer, publicOnly bool, available, availableHeight int) string {
	if !viewer {
		return mutedStyle.Render(
			"(the inbox is always the authenticated account's own — GitHub has no endpoint " +
				"for somebody else's notifications, so there is nothing to show here)")
	}

	switch im.state {
	case inboxIdle:
		return mutedStyle.Render("(loading your inbox…)")
	case inboxLoading:
		if len(im.items) == 0 {
			return mutedStyle.Render("(loading your inbox…)")
		}
	case inboxFailed:
		if len(im.items) == 0 {
			return strings.Join([]string{
				errorStyle.Render("Your inbox could not be loaded."),
				mutedStyle.Render(inboxErrorHint(im.reason)),
				"",
				keyHints("r", "retry"),
			}, "\n")
		}
	}

	rows := visibleInbox(im.items, im.filter, im.query, publicOnly)
	if len(rows) == 0 {
		return strings.Join([]string{
			im.renderHeaderLine(0, publicOnly),
			"",
			mutedStyle.Render(im.emptyReason(publicOnly)),
		}, "\n")
	}

	cursor := im.cursor
	if cursor >= len(rows) {
		cursor = len(rows) - 1
	}
	if cursor < 0 {
		cursor = 0
	}

	// Chrome: header, blank, table header, rule, blank, note, blank, hints.
	const inboxChrome = 8
	chrome := inboxChrome
	if im.searchActive || im.query != "" {
		chrome++
	}
	if im.state == inboxFailed {
		chrome++
	}

	rowsVisible := len(rows)
	if availableHeight > 0 {
		rowsVisible = availableHeight - chrome
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

	parts := []string{im.renderHeaderLine(len(rows), publicOnly)}

	if im.state == inboxFailed {
		parts = append(parts, warnStyle.Render("reload failed — showing the previous load. ")+
			mutedStyle.Render(inboxErrorHint(im.reason)))
	}

	switch {
	case im.searchActive:
		parts = append(parts, mutedStyle.Render("search: ")+
			im.query+boldStyle.Foreground(colAccent).Render("█")+
			mutedStyle.Render("   (enter confirm · esc cancel)"))
	case im.query != "":
		parts = append(parts, mutedStyle.Render("filter: ")+im.query+
			mutedStyle.Render("   (esc to clear)"))
	}

	parts = append(parts, "", renderInboxTable(rows[offset:end], cursor-offset, available))
	parts = append(parts, "", im.windowNote(len(rows), offset, end, publicOnly))
	parts = append(parts, "", keyHints(
		"↑↓", "move",
		"s", "filter: "+inboxFilterLabels[im.filter],
		"/", "search",
		"enter", "github",
		"c", "copy",
		"r", "reload",
	))
	return strings.Join(parts, "\n")
}

// emptyReason says *why* there is nothing, because "your inbox is empty"
// and "everything here is filtered out" are different sentences and only
// one of them is about the account.
func (im InboxModel) emptyReason(publicOnly bool) string {
	switch {
	case im.query != "":
		return fmt.Sprintf("(nothing matches %q — esc to clear)", im.query)
	case im.filter != InboxAll && len(im.items) > 0:
		return fmt.Sprintf("(nothing under the %q filter — press s to widen it)",
			inboxFilterLabels[im.filter])
	case publicOnly && len(im.items) > 0:
		return "(every unread notification is from a private repository — press p to leave public-only mode)"
	}
	return "(nothing unread — your inbox is clear)"
}

// windowNote states what is hidden, and **by what**. A filter that quietly
// removes three quarters of an inbox is the failure this line exists to
// prevent — but naming the wrong cause is its own failure, and the first
// version did exactly that: it attributed every hidden row to "the current
// filter" while public-only was the one doing the hiding, so a screenshot
// taken with filter `all` read "1 more unread hidden by the current filter"
// over a filter that hides nothing. Found by looking at the screenshot.
//
// So the two are counted separately. They compose — a row can be dropped by
// public-only *and* excluded by the filter — which is why privacy is
// measured first and the filter's count is taken from what survives it,
// rather than both from the raw total.
func (im InboxModel) windowNote(shown, offset, end int, publicOnly bool) string {
	afterPrivacy := len(im.items)
	if publicOnly {
		afterPrivacy = 0
		for _, n := range im.items {
			if !n.IsPrivate {
				afterPrivacy++
			}
		}
	}

	var hiddenParts []string
	if h := len(im.items) - afterPrivacy; h > 0 {
		hiddenParts = append(hiddenParts,
			fmt.Sprintf("%d in private repositories", h))
	}
	if h := afterPrivacy - shown; h > 0 {
		hiddenParts = append(hiddenParts,
			fmt.Sprintf("%d by the %q filter", h, inboxFilterLabels[im.filter]))
	}

	var b strings.Builder
	if end-offset < shown {
		b.WriteString(mutedStyle.Render(fmt.Sprintf("rows %d-%d of %d   ", offset+1, end, shown)))
	}
	if len(hiddenParts) > 0 {
		b.WriteString(mutedStyle.Render("hidden: " + strings.Join(hiddenParts, ", ")))
	}
	if len(im.items) >= github.NotificationsPageSize {
		if b.Len() > 0 {
			b.WriteString(mutedStyle.Render(" · "))
		}
		b.WriteString(mutedStyle.Render(fmt.Sprintf(
			"the first %d unread, not the whole inbox", github.NotificationsPageSize)))
	}
	if b.Len() == 0 {
		// Something has to occupy the row the chrome budget reserved, or
		// the table gains a line and the footer loses one.
		return mutedStyle.Render("read-only — enter opens the thread on GitHub, where it can be marked read")
	}
	return b.String()
}

func (im InboxModel) renderHeaderLine(shown int, publicOnly bool) string {
	label := fmt.Sprintf("%d unread", shown)
	if shown != len(im.items) {
		label = fmt.Sprintf("%d of %d unread", shown, len(im.items))
	}
	out := sectionTitleStyle.Render(label)
	out += mutedStyle.Render("   filter: ") + valueStyle.Render(inboxFilterLabels[im.filter])
	if publicOnly {
		out += mutedStyle.Render("   public only")
	}
	if im.state == inboxLoading {
		out += mutedStyle.Render("   reloading…")
	} else if !im.fetchedAt.IsZero() {
		out += mutedStyle.Render("   loaded " + formatRelativeAgo(im.fetchedAt))
	}
	return out
}

func renderInboxTable(items []github.Notification, cursorRow, available int) string {
	repoW, titleW := inboxColumnWidths(available)

	header := strings.Join([]string{
		strings.Repeat(" ", inboxCursorW),
		mutedStyle.Render(padRight("When", inboxWhenW)),
		mutedStyle.Render(padRight("Why", inboxReasonW)),
		mutedStyle.Render(padRight("Repository", repoW)),
		mutedStyle.Render(padRight("Subject", titleW)),
	}, "  ")
	rule := tabRuleStyle.Render(strings.Repeat("─", lipgloss.Width(header)))

	out := []string{header, rule}
	for i, n := range items {
		active := i == cursorRow

		marker := "  "
		if active {
			marker = activeTabStyle.Render("▸ ")
		}

		when := padRight(formatRelativeAgo(n.UpdatedAt), inboxWhenW)
		why := padRightRaw(styleInboxReason(inboxReason(n), n.Reason), inboxReasonW)
		repo := padRight(truncate(n.Repo, repoW), repoW)
		title := padRight(truncate(n.Title, titleW), titleW)

		if active {
			repo = boldStyle.Foreground(colAccent).Render(repo)
		} else {
			repo = valueStyle.Render(repo)
			when = mutedStyle.Render(when)
			title = mutedStyle.Render(title)
		}

		out = append(out, marker+when+"  "+why+"  "+repo+"  "+title)
	}
	return strings.Join(out, "\n")
}

// styleInboxReason marks the reasons that name a person. Everything else
// stays neutral, so the eye lands on the rows that want an answer rather
// than on the three quarters that are a build saying it passed.
func styleInboxReason(label, reason string) string {
	if involvingReasons[reason] {
		return accentStyle.Render(label)
	}
	return mutedStyle.Render(label)
}

// inboxColumnWidths splits the leftover between repository and subject,
// giving the subject its floor first — it is the column that varies.
func inboxColumnWidths(available int) (repoW, titleW int) {
	const gaps = 4 * 2
	fixed := inboxCursorW + inboxWhenW + inboxReasonW + gaps

	leftover := available - fixed
	if leftover < inboxRepoW+inboxTitleMin {
		repoW = leftover - inboxTitleMin
		if repoW < 10 {
			repoW = 10
		}
		return repoW, inboxTitleMin
	}
	return inboxRepoW, leftover - inboxRepoW
}

// inboxAvailable reports whether there is an inbox to fetch at all.
//
// The endpoint has no login parameter — it is always the caller's own
// inbox — so under `octoscope <someone>` there is nothing to ask for.
// Unauthenticated there is no "your" anything either.
//
// Read off Stats rather than the client because that is where both facts
// already live together, and because it also means the tab waits for the
// first fetch instead of firing a request whose answer it could not label.
func (m Model) inboxAvailable() bool {
	return m.stats != nil && m.stats.Authenticated && m.stats.IsViewer
}

// maybeLoadInbox fires the on-demand fetch the first time the tab is shown.
func (m Model) maybeLoadInbox() (Model, tea.Cmd) {
	if m.activeTab != TabInbox || !m.inbox.NeedsLoad() || !m.inboxAvailable() {
		return m, nil
	}
	m.inbox = m.inbox.startLoading()
	return m, fetchNotificationsCmd(m.client, m.inbox.gen)
}

// afterTabSwitch fires whatever the newly-visible tab loads on demand.
//
// One entry point rather than a call per surface: the feed and the inbox
// both load lazily, and the next lazy tab will want the same treatment.
// Every route onto a tab — a digit, tab/shift+tab, the first successful
// fetch resolving the login — goes through here, because a user cannot
// tell which route they took and all of them must behave the same.
func (m Model) afterTabSwitch() (Model, tea.Cmd) {
	m, feedCmd := m.maybeLoadFeed()
	m, inboxCmd := m.maybeLoadInbox()
	return m, tea.Batch(feedCmd, inboxCmd)
}

// inboxErrorHint is the inbox's own error vocabulary rather than the feed's.
//
// Two reasons it cannot be shared. The feed's wording names *events*, which
// is the wrong noun here — and more importantly, a 403 on this endpoint has
// one overwhelmingly likely cause that no other surface shares:
// `/notifications` **only accepts a classic personal access token**.
// GitHub's own reference says so — "These endpoints only support
// authentication using a personal access token (classic)" — and the
// response carries `X-Accepted-Oauth-Scopes: notifications, repo`.
//
// That matters because octoscope's README *recommends* a fine-grained
// token, so the likeliest reader of this message is somebody who followed
// the documentation and cannot load this one tab. "The token cannot read
// this" would send them hunting for a missing permission that does not
// exist; naming the token type is the difference between a dead end and a
// fix. Raised by review on #131.
func inboxErrorHint(reason github.FetchErrorReason) string {
	switch reason {
	case github.ReasonAuth:
		return "GitHub rejected the token — it may be expired or revoked."
	case github.ReasonAuthScope:
		return "GitHub's notifications endpoint only accepts a *classic* personal " +
			"access token, with the notifications or repo scope — a fine-grained " +
			"token cannot read it, whatever permissions it is given."
	case github.ReasonNotFound:
		return "GitHub did not recognise the notifications endpoint."
	case github.ReasonRateLimitPrimary, github.ReasonRateLimitSecondary:
		return "Rate-limited — try again shortly."
	case github.ReasonNetwork:
		return "The request did not reach GitHub."
	case github.ReasonServer:
		return "GitHub answered with a server error."
	}
	return "Press r to try again."
}
