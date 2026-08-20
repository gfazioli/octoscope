package ui

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/gfazioli/octoscope/internal/github"
)

func nt(reason, repo, title string, private bool, ago time.Duration) github.Notification {
	return github.Notification{
		ID:        reason + repo + title,
		Reason:    reason,
		Type:      "PullRequest",
		Title:     title,
		Repo:      repo,
		RepoURL:   "https://github.com/" + repo,
		URL:       "https://github.com/" + repo + "/pull/1",
		Unread:    true,
		UpdatedAt: time.Now().Add(-ago),
		IsPrivate: private,
	}
}

func inboxFixture() []github.Notification {
	return []github.Notification{
		nt("mention", "octocat/open", "you were mentioned", false, time.Minute),
		nt("ci_activity", "octocat/open", "ci workflow run succeeded", false, 2*time.Minute),
		nt("ci_activity", "octocat/open", "ci workflow run failed", false, 3*time.Minute),
		nt("review_requested", "acme/secret", "please review", true, 4*time.Minute),
		nt("subscribed", "octocat/other", "something changed", false, 5*time.Minute),
	}
}

// Notifications are not part of Stats, so Stats.Public() cannot reach them.
// Unlike sponsors this is a real per-item flag, so the filter is a skip loop
// rather than dropping the whole surface.
func TestInboxDropsPrivateRepositoriesUnderPublicOnly(t *testing.T) {
	rows := visibleInbox(inboxFixture(), InboxAll, "", true)
	if len(rows) != 4 {
		t.Fatalf("kept %d rows, want 4", len(rows))
	}
	for _, r := range rows {
		if r.IsPrivate {
			t.Errorf("a private-repo notification survived public-only: %+v", r)
		}
	}
}

func TestInboxFilterCycle(t *testing.T) {
	items := inboxFixture()
	for _, tc := range []struct {
		f    InboxFilter
		want int
	}{
		{InboxAll, 5},
		{InboxInvolving, 2}, // mention + review_requested
		{InboxCI, 2},        // the two ci_activity rows
	} {
		if got := len(visibleInbox(items, tc.f, "", false)); got != tc.want {
			t.Errorf("filter %q kept %d, want %d", inboxFilterLabels[tc.f], got, tc.want)
		}
	}

	// And it cycles back round, so `s` is never a dead end.
	f := InboxAll
	for i := 0; i < len(inboxFilterLabels); i++ {
		f = f.next()
	}
	if f != InboxAll {
		t.Errorf("cycling %d times landed on %v, want back at all", len(inboxFilterLabels), f)
	}
}

// "subscribed" and "ci_activity" are things you signed up for; the involving
// set is the things that name you. Getting that membership wrong is the
// difference between a useful filter and a filter that hides your mentions.
func TestInvolvingReasonsAreTheOnesThatNameYou(t *testing.T) {
	for _, r := range []string{"mention", "team_mention", "review_requested", "assign", "author", "security_alert"} {
		if !involvingReasons[r] {
			t.Errorf("%q should count as involving you", r)
		}
	}
	for _, r := range []string{"ci_activity", "subscribed", "state_change", "comment", ""} {
		if involvingReasons[r] {
			t.Errorf("%q should not count as involving you", r)
		}
	}
}

// The row the cursor highlights and the row enter opens must stay the same
// row after the filter shrinks the set underneath it.
func TestInboxCursorSurvivesTheFilterShrinking(t *testing.T) {
	im := InboxModel{state: inboxReady, items: inboxFixture()}
	for i := 0; i < 4; i++ {
		im, _ = im.Update(key("down"), false)
	}
	if im.cursor != 4 {
		t.Fatalf("cursor = %d, want 4", im.cursor)
	}

	// s cycles to "involving you", which leaves two rows.
	im, _ = im.Update(key("s"), false)
	got, ok := im.selected(false)
	if !ok {
		t.Fatal("no selection after the filter shrank the set")
	}
	if got.Reason != "mention" {
		t.Errorf("selected %q; s resets the cursor to the top", got.Reason)
	}

	// And walking off the end of the shorter list must not reach past it.
	im, _ = im.Update(key("end"), false)
	if im.cursor != 1 {
		t.Errorf("cursor = %d after end, want 1 (two rows)", im.cursor)
	}
}

// "Your inbox is clear" and "your filter hides everything" are different
// sentences, and only one of them is about the account.
func TestInboxEmptyStateSaysWhichKindOfEmpty(t *testing.T) {
	_ = applyTheme("octoscope", "")

	clear := InboxModel{state: inboxReady}
	if got := clear.emptyReason(false); !strings.Contains(got, "inbox is clear") {
		t.Errorf("a genuinely empty inbox reads as %q", got)
	}

	filtered := InboxModel{state: inboxReady, items: inboxFixture(), filter: InboxCI}
	filtered.filter = InboxInvolving
	filtered.items = []github.Notification{nt("ci_activity", "o/r", "x", false, time.Minute)}
	if got := filtered.emptyReason(false); !strings.Contains(got, "press s") {
		t.Errorf("a filtered-empty inbox reads as %q", got)
	}

	priv := InboxModel{state: inboxReady, items: []github.Notification{
		nt("mention", "acme/secret", "x", true, time.Minute),
	}}
	if got := priv.emptyReason(true); !strings.Contains(got, "private repository") {
		t.Errorf("empty-under-public-only reads as %q", got)
	}

	searched := InboxModel{state: inboxReady, items: inboxFixture(), query: "zzz"}
	if got := searched.emptyReason(false); !strings.Contains(got, "esc to clear") {
		t.Errorf("a no-match search reads as %q", got)
	}
}

// A filter that quietly removes three quarters of an inbox is the failure
// the note exists to prevent.
func TestInboxStatesWhatTheFilterIsHiding(t *testing.T) {
	_ = applyTheme("octoscope", "")
	im := InboxModel{state: inboxReady, items: inboxFixture(), filter: InboxInvolving}
	out := ansi.Strip(im.renderInboxTab(true, false, 150, 30))
	if !strings.Contains(out, `3 by the "involving you" filter`) {
		t.Errorf("the hidden count is not stated, or blames the wrong thing:\n%s", out)
	}
	if !strings.Contains(out, "2 of 5 unread") {
		t.Errorf("the header does not state both counts:\n%s", out)
	}
}

// Hiding is stated *with its cause*, and there are two causes that compose.
// The first version blamed everything on "the current filter", so a
// screenshot taken with the filter set to `all` — which hides nothing —
// read "1 more unread hidden by the current filter". Found by looking at
// the picture, not by a test.
func TestInboxNamesWhatIsHidingRows(t *testing.T) {
	_ = applyTheme("octoscope", "")

	// The assertion has to read the *hidden:* line and nothing else. A
	// first attempt looked for "filter" anywhere after it and failed on the
	// hint line below ("s filter: all"), which is a test bug and not a
	// defect — worth keeping the note, since the same crude search would
	// pass for the wrong reason just as easily as it failed.
	hiddenLine := func(out string) string {
		for _, l := range strings.Split(out, "\n") {
			if strings.Contains(l, "hidden:") {
				return strings.TrimSpace(l)
			}
		}
		return ""
	}

	// Privacy alone: filter is `all`, so it cannot be the culprit.
	privacyOnly := InboxModel{state: inboxReady, items: inboxFixture()}
	out := ansi.Strip(privacyOnly.renderInboxTab(true, true, 150, 30))
	line := hiddenLine(out)
	if !strings.Contains(line, "1 in private repositories") {
		t.Errorf("privacy hiding is not named: %q", line)
	}
	if strings.Contains(line, "filter") {
		t.Errorf("the `all` filter was blamed for a privacy drop: %q", line)
	}

	// Both at once: they compose, so both are named.
	both := InboxModel{state: inboxReady, items: inboxFixture(), filter: InboxCI}
	line = hiddenLine(ansi.Strip(both.renderInboxTab(true, true, 150, 30)))
	if !strings.Contains(line, "in private repositories") || !strings.Contains(line, `"ci" filter`) {
		t.Errorf("both causes should be named: %q", line)
	}

	// Neither: nothing is hidden, so nothing is claimed.
	clean := InboxModel{state: inboxReady, items: []github.Notification{
		nt("mention", "octocat/open", "x", false, time.Minute),
	}}
	out = ansi.Strip(clean.renderInboxTab(true, false, 150, 30))
	if strings.Contains(out, "hidden:") {
		t.Errorf("nothing is hidden but the tab says otherwise:\n%s", out)
	}
}

// The read-only decision has to be visible, or the user hunts for a key
// that does not exist.
func TestInboxSaysItIsReadOnlyWhenItHasRoom(t *testing.T) {
	_ = applyTheme("octoscope", "")
	im := InboxModel{state: inboxReady, items: []github.Notification{
		nt("mention", "octocat/open", "x", false, time.Minute),
	}}
	out := ansi.Strip(im.renderInboxTab(true, false, 150, 30))
	if !strings.Contains(out, "read-only") {
		t.Errorf("nothing says the tab is read-only:\n%s", out)
	}
}

// The endpoint has no login parameter, so under someone else's profile it
// would show the caller's own inbox beneath their name.
func TestInboxDeclinesToShowSomebodyElsesProfile(t *testing.T) {
	_ = applyTheme("octoscope", "")
	im := InboxModel{state: inboxReady, items: inboxFixture()}
	out := ansi.Strip(im.renderInboxTab(false, false, 150, 30))
	if strings.Contains(out, "octocat/open") {
		t.Errorf("the caller's own inbox rendered under another profile:\n%s", out)
	}
	if !strings.Contains(out, "nothing to show") {
		t.Errorf("no explanation given:\n%s", out)
	}
}

func TestInboxColumnWidthsNeverGoNegative(t *testing.T) {
	for w := 0; w < 220; w++ {
		repoW, titleW := inboxColumnWidths(w)
		if repoW < 1 || titleW < 1 {
			t.Fatalf("available=%d → repo %d, title %d", w, repoW, titleW)
		}
	}
}

func TestRenderInboxSurvivesExtremeGeometry(t *testing.T) {
	_ = applyTheme("octoscope", "")
	im := InboxModel{state: inboxReady}
	for i := 0; i < 60; i++ {
		im.items = append(im.items,
			nt("ci_activity", "octocat/a-very-long-repository-name", strings.Repeat("title ", 20), i%4 == 0, time.Duration(i)*time.Minute))
	}
	for _, w := range []int{0, 1, 20, 80, 400} {
		for _, h := range []int{0, 1, 3, 12, 200} {
			_ = im.renderInboxTab(true, false, w, h)
			_ = im.renderInboxTab(true, true, w, h)
		}
	}
}

// Two reloads race; the older reply must not overwrite the newer one.
func TestSupersededInboxReloadIsDropped(t *testing.T) {
	m := newFeedRoutingModel(t)
	m.activeTab = TabInbox
	m.inbox = m.inbox.startLoading()
	stale := m.inbox.gen
	m.inbox = m.inbox.startLoading()

	next, _ := m.Update(inboxLoadedMsg{
		gen:   m.inbox.gen,
		items: []github.Notification{nt("mention", "o/newer", "x", false, time.Minute)},
		at:    time.Now(),
	})
	m = next.(Model)

	next, _ = m.Update(inboxLoadedMsg{
		gen:   stale,
		items: []github.Notification{nt("mention", "o/older", "x", false, time.Hour)},
		at:    time.Now().Add(-time.Hour),
	})
	m = next.(Model)

	if len(m.inbox.items) != 1 || m.inbox.items[0].Repo != "o/newer" {
		t.Errorf("the superseded reply won: %+v", m.inbox.items)
	}

	next, _ = m.Update(inboxLoadedMsg{gen: stale, err: errors.New("timeout")})
	if next.(Model).inbox.state != inboxReady {
		t.Error("a stale failure knocked a newer success into the error state")
	}
}

// Routing, which is the half that breaks silently.
func TestInboxTabRouting(t *testing.T) {
	m := newFeedRoutingModel(t)
	m.stats.Authenticated = true
	m.stats.IsViewer = true

	next, cmd := m.Update(key("7"))
	m = next.(Model)
	if m.activeTab != TabInbox {
		t.Fatalf("key 7 landed on %v, want TabInbox", m.activeTab)
	}
	if cmd == nil {
		t.Error("opening the Inbox did not fire its load")
	}
	if m.inbox.NeedsLoad() {
		t.Error("the inbox still wants a load after one was fired")
	}

	// Away and back: no second request. The returned model is kept — the
	// first draft dropped it and then sent `s` while still on Overview,
	// which failed for a reason that had nothing to do with the binding.
	next, _ = m.Update(key("1"))
	m = next.(Model)
	next, cmd = m.Update(key("7"))
	m = next.(Model)
	if cmd != nil {
		t.Error("re-entering the Inbox fired a second load")
	}
	if m.activeTab != TabInbox {
		t.Fatalf("precondition: back on %v, not the Inbox", m.activeTab)
	}

	// s reaches the sub-model rather than being swallowed globally.
	m.inbox = m.inbox.loaded(inboxFixture(), time.Now())
	next, _ = m.Update(key("s"))
	if got := next.(Model).inbox.filter; got != InboxInvolving {
		t.Errorf("s left the filter on %v", got)
	}
}

// Without a token, or under somebody else's profile, there is nothing to
// fetch — and firing the request anyway would answer with the caller's own
// inbox under the wrong name.
func TestInboxDoesNotLoadWhenThereIsNoInboxToLoad(t *testing.T) {
	for _, tc := range []struct {
		name          string
		authenticated bool
		viewer        bool
	}{
		{"unauthenticated", false, true},
		{"another profile", true, false},
	} {
		m := newFeedRoutingModel(t)
		m.stats.Authenticated = tc.authenticated
		m.stats.IsViewer = tc.viewer
		_, cmd := m.Update(key("7"))
		if cmd != nil {
			t.Errorf("%s: a load was fired with no inbox to fetch", tc.name)
		}
	}
}

// While a filter is being typed the keystrokes are literal text, so the
// footer must not advertise global hotkeys that will not fire.
func TestFooterKnowsTheInboxIsTakingLiteralText(t *testing.T) {
	m := newFeedRoutingModel(t)
	m.activeTab = TabInbox
	if m.listInputMode() {
		t.Fatal("precondition: not in input mode yet")
	}
	m.inbox.searchActive = true
	if !m.listInputMode() {
		t.Error("the footer still thinks the global hotkeys are live")
	}
}

// The clamp inside Update, which the filter-cycle test cannot reach because
// cycling resets the cursor to zero. Toggling public-only does not reset it:
// the set shrinks underneath a cursor that stays where it was, and without
// the clamp the next keystroke steps from an index that no longer exists —
// invisible on screen, because the renderer clamps too, while enter opens
// the wrong row.
func TestInboxCursorIsClampedBeforeTheKeystrokeNotAfter(t *testing.T) {
	im := InboxModel{state: inboxReady, items: []github.Notification{
		nt("mention", "octocat/first", "a", false, time.Minute),
		nt("mention", "acme/secret1", "b", true, 2*time.Minute),
		nt("mention", "acme/secret2", "c", true, 3*time.Minute),
		nt("mention", "octocat/second", "d", false, 4*time.Minute),
		nt("mention", "acme/secret3", "e", true, 5*time.Minute),
	}}

	for i := 0; i < 4; i++ {
		im, _ = im.Update(key("down"), false)
	}
	if im.cursor != 4 {
		t.Fatalf("cursor = %d, want 4", im.cursor)
	}

	// Two public rows remain. The stale index 4 has to be clamped to 1
	// before `up` is applied, so it lands on 0.
	im, _ = im.Update(key("up"), true)
	if im.cursor != 0 {
		t.Errorf("cursor = %d after up, want 0", im.cursor)
	}
	got, ok := im.selected(true)
	if !ok {
		t.Fatal("no selection after the set shrank")
	}
	if got.Repo != "octocat/first" {
		t.Errorf("selected %q, want octocat/first", got.Repo)
	}
}

// Every other list tab opens the action menu on space. A list tab where
// space does something else is the surprise worth avoiding — and the guard
// that admits a tab to the menu has been forgotten before, on the Gists
// tab, where the case existed while the guard did not.
func TestSpaceOpensTheActionMenuOnTheInbox(t *testing.T) {
	m := newFeedRoutingModel(t)
	m.stats.Authenticated = true
	m.stats.IsViewer = true
	m.activeTab = TabInbox
	m.inbox = m.inbox.loaded(inboxFixture(), time.Now())

	next, _ := m.Update(key(" "))
	m = next.(Model)
	if !m.actionMenu.IsOpen() {
		t.Fatal("space did not open the action menu on the Inbox")
	}

	out := ansi.Strip(m.actionMenu.View(150))
	for _, want := range []string{"Open in GitHub", "Copy URL"} {
		if !strings.Contains(out, want) {
			t.Errorf("the menu is missing %q:\n%s", want, out)
		}
	}
	// The title has to name the *row*, not its repository: an inbox
	// routinely holds several threads from one repo, so a repo-titled menu
	// cannot say which one it is about. The fixture has three rows from
	// octocat/open for exactly this reason.
	if !strings.Contains(out, "you were mentioned") {
		t.Errorf("the title does not identify the selected row:\n%s", out)
	}
	// No drill-in: a notification has nothing to show that the thread does
	// not, so offering "View details" would promise a view that does not
	// exist.
	if strings.Contains(out, "View details") {
		t.Errorf("the Inbox menu offers a drill-in it does not have:\n%s", out)
	}

	// Move to the next row — same repository, different thread — and the
	// title has to follow. This is the assertion a repo-based title passes
	// while being useless.
	next, _ = m.Update(key("esc"))
	m = next.(Model)
	next, _ = m.Update(key("down"))
	m = next.(Model)
	next, _ = m.Update(key(" "))
	m = next.(Model)
	second := ansi.Strip(m.actionMenu.View(150))
	if !strings.Contains(second, "ci workflow run succeeded") {
		t.Errorf("the title did not follow the cursor to the next row:\n%s", second)
	}
	if strings.Contains(second, "you were mentioned") {
		t.Errorf("the title still names the previous row:\n%s", second)
	}
}

// A 403 here has one overwhelmingly likely cause that no other surface
// shares: GitHub documents /notifications as accepting a classic personal
// access token only. Since the README otherwise recommends a fine-grained
// one, the likeliest reader of this message followed the documentation —
// so it has to name the token type rather than a permission that does not
// exist. Raised by review on #131.
func TestInboxScopeErrorNamesTheClassicTokenRequirement(t *testing.T) {
	got := inboxErrorHint(github.ReasonAuthScope)
	for _, want := range []string{"classic", "notifications", "fine-grained"} {
		if !strings.Contains(strings.ToLower(got), want) {
			t.Errorf("the hint does not mention %q: %q", want, got)
		}
	}
	// And it must not be the feed's wording, which names the wrong noun.
	if strings.Contains(got, "events") {
		t.Errorf("the inbox is reusing the feed's hint: %q", got)
	}
	if got == feedErrorHint(github.ReasonAuthScope) {
		t.Error("the inbox and the feed share a hint for a cause only one of them has")
	}
}

// And the tab has to actually use it. The assertion above passes while the
// renderer still calls feedErrorHint — mutation-checked, and it did — so
// the function being right proves nothing about what reaches the screen.
func TestInboxRendersItsOwnErrorHint(t *testing.T) {
	_ = applyTheme("octoscope", "")

	// Both paths: no rows at all, and a failed reload over existing rows.
	empty := InboxModel{}.failed(errors.New("403"), github.ReasonAuthScope)
	stale := InboxModel{state: inboxReady, items: inboxFixture()}.
		failed(errors.New("403"), github.ReasonAuthScope)

	for name, im := range map[string]InboxModel{"first load": empty, "failed reload": stale} {
		out := ansi.Strip(im.renderInboxTab(true, false, 150, 30))
		if !strings.Contains(out, "classic") {
			t.Errorf("%s: the screen does not name the classic-token requirement:\n%s", name, out)
		}
		if strings.Contains(out, "account's events") {
			t.Errorf("%s: the screen shows the feed's wording:\n%s", name, out)
		}
	}
}
