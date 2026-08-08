package ui

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/gfazioli/octoscope/internal/github"
)

func ev(typ, repo string, number int, title string, public bool, ago time.Duration) github.Event {
	return github.Event{
		ID:        typ + repo,
		Type:      typ,
		Repo:      repo,
		Number:    number,
		Title:     title,
		IsPublic:  public,
		CreatedAt: time.Now().Add(-ago),
		URL:       "https://github.com/" + repo,
	}
}

// The privacy invariant for the `p` toggle. The fetch already asks the
// public endpoint at startup, but toggling mid-session has to filter what
// is already in memory — and this is the one list Stats.Public() cannot
// reach, because events are not part of Stats.
func TestPublicOnlyDropsPrivateEventsAlreadyInMemory(t *testing.T) {
	events := []github.Event{
		ev("PushEvent", "acme/secret", 0, "", false, time.Minute),
		ev("PushEvent", "octocat/open", 0, "", true, 2*time.Minute),
		ev("IssuesEvent", "acme/secret", 4, "internal thing", false, 3*time.Minute),
	}
	rows := visibleEvents(events, "", true)
	if len(rows) != 1 {
		t.Fatalf("kept %d rows, want only the public one", len(rows))
	}
	if rows[0].Repo != "octocat/open" {
		t.Errorf("kept %q", rows[0].Repo)
	}
	for _, r := range rows {
		if !r.IsPublic {
			t.Errorf("a private event survived public-only: %+v", r)
		}
	}
}

// The filter has to match what the row shows. Filtering the raw type would
// make "merged" find nothing while the screen says merged.
func TestFeedFilterMatchesTheRenderedRow(t *testing.T) {
	events := []github.Event{
		ev("PullRequestEvent", "octocat/one", 7, "", true, time.Minute),
		ev("IssuesEvent", "octocat/two", 9, "Broken pipe", true, 2*time.Minute),
	}
	events[0].Action = "merged"

	for _, tc := range []struct {
		query    string
		wantRepo string
	}{
		{"merged", "octocat/one"},      // the verb, which is derived not stored
		{"broken", "octocat/two"},      // the title
		{"octocat/one", "octocat/one"}, // the repo
		{"PR #7", "octocat/one"},       // the subject as printed
	} {
		rows := visibleEvents(events, tc.query, false)
		if len(rows) != 1 || rows[0].Repo != tc.wantRepo {
			t.Errorf("query %q matched %d rows (want 1, %s)", tc.query, len(rows), tc.wantRepo)
		}
	}
}

// The wall of noise this exists to fix: a run of reviews and comments on
// the same pull request becomes one line.
func TestCollapseChatterFoldsARunOnOneSubject(t *testing.T) {
	var events []github.Event
	events = append(events, ev("PullRequestEvent", "o/r", 123, "the tab", true, time.Minute))
	events[0].Action = "merged"
	for i := 0; i < 8; i++ {
		e := ev("IssueCommentEvent", "o/r", 123, "the tab", true, time.Duration(i+2)*time.Minute)
		events = append(events, e)
		r := ev("PullRequestReviewEvent", "o/r", 123, "the tab", true, time.Duration(i+2)*time.Minute)
		r.Action = "commented"
		events = append(events, r)
	}

	rows := collapseChatter(events)
	if len(rows) != 2 {
		t.Fatalf("got %d rows, want 2 (the merge, then one folded run)", len(rows))
	}
	if rows[0].count != 1 || rows[0].verb != "merged" {
		t.Errorf("the state change was folded away: %+v", rows[0])
	}
	if rows[1].count != 16 {
		t.Errorf("folded run counts %d, want 16", rows[1].count)
	}
	// A mixed run of comments and comment-state reviews is labelled by
	// what they all are. Not a compromise: an approval would never have
	// reached this row, because approvals do not fold.
	if rows[1].verb != "commented" {
		t.Errorf("verb = %q, want %q", rows[1].verb, "commented")
	}
	if n := countEvents(rows); n != len(events) {
		t.Errorf("countEvents = %d, want %d — folding must not lose events", n, len(events))
	}
}

// An approval is the one review that matters. Folding it into a "×12" of
// drive-by comments is exactly how it would disappear.
func TestApprovalNeverFoldsIntoChatter(t *testing.T) {
	c1 := ev("IssueCommentEvent", "o/r", 5, "x", true, time.Minute)
	appr := ev("PullRequestReviewEvent", "o/r", 5, "x", true, 2*time.Minute)
	appr.Action = "approved"
	c2 := ev("IssueCommentEvent", "o/r", 5, "x", true, 3*time.Minute)

	rows := collapseChatter([]github.Event{c1, appr, c2})
	if len(rows) != 3 {
		t.Fatalf("got %d rows, want 3 — the approval must stand alone", len(rows))
	}
	if rows[1].verb != "approved" {
		t.Errorf("middle row verb = %q, want %q", rows[1].verb, "approved")
	}
}

// Folding must not reorder or jump a row of a different kind, or the
// timeline stops being the timeline GitHub sent.
func TestCollapseChatterOnlyFoldsAdjacentRunsOnTheSameSubject(t *testing.T) {
	rows := collapseChatter([]github.Event{
		ev("IssueCommentEvent", "o/r", 1, "a", true, time.Minute),
		ev("IssueCommentEvent", "o/r", 2, "b", true, 2*time.Minute), // different number
		ev("IssueCommentEvent", "o/r", 1, "a", true, 3*time.Minute), // same as the first, but not adjacent
	})
	if len(rows) != 3 {
		t.Fatalf("got %d rows, want 3 — non-adjacent runs must not merge", len(rows))
	}
	for i, r := range rows {
		if r.count != 1 {
			t.Errorf("row %d folded %d events", i, r.count)
		}
	}
}

// Numbers are only unique *within* a repository, so #1 over here and #1
// over there are unrelated. Folding on the number alone would merge two
// different conversations into one row.
func TestCollapseChatterDoesNotFoldAcrossRepositories(t *testing.T) {
	rows := collapseChatter([]github.Event{
		ev("IssueCommentEvent", "octocat/one", 1, "a", true, time.Minute),
		ev("IssueCommentEvent", "octocat/two", 1, "b", true, 2*time.Minute),
	})
	if len(rows) != 2 {
		t.Fatalf("got %d rows, want 2 — same number, different repository", len(rows))
	}
}

// The row the cursor highlights and the row enter opens have to be the
// same row after the set shrinks underneath it.
func TestFeedCursorSurvivesTheRowSetShrinking(t *testing.T) {
	// Two public rows survive the toggle, deliberately: with only one left
	// every cursor value clamps to it at render time and a missing clamp
	// inside Update would be invisible.
	fm := FeedModel{state: feedReady, events: []github.Event{
		ev("PushEvent", "octocat/first", 0, "", true, time.Minute),
		ev("PushEvent", "acme/secret1", 0, "", false, 2*time.Minute),
		ev("PushEvent", "acme/secret2", 0, "", false, 3*time.Minute),
		ev("PushEvent", "octocat/second", 0, "", true, 4*time.Minute),
		ev("PushEvent", "acme/secret3", 0, "", false, 5*time.Minute),
	}}

	// Walk to the last row with everything visible.
	for i := 0; i < 4; i++ {
		fm, _ = fm.Update(key("down"), false)
	}
	if fm.cursor != 4 {
		t.Fatalf("cursor = %d, want 4", fm.cursor)
	}

	// Public-only now leaves two rows. The stale index 4 has to be clamped
	// to the new last row (1) *before* the keystroke is applied, so up
	// lands on row 0. Without the clamp it goes 4 → 3, the renderer hides
	// it, and enter opens the last row while the cursor looks elsewhere.
	fm, _ = fm.Update(key("up"), true)
	if fm.cursor != 0 {
		t.Errorf("cursor = %d after up, want 0", fm.cursor)
	}
	got, ok := fm.selectedEvent(true)
	if !ok {
		t.Fatal("no selection after the set shrank")
	}
	if got.Repo != "octocat/first" {
		t.Errorf("selected %q, want octocat/first", got.Repo)
	}
}

// Loading is fired once. A failed load waits for `r` rather than retrying
// every time the sub-tab is looked at, so a bad token cannot turn tab
// switching into a request loop.
func TestFeedLoadsOnceAndDoesNotRetryOnSight(t *testing.T) {
	fm := FeedModel{}
	if !fm.NeedsLoad() {
		t.Fatal("a fresh feed should want a load")
	}
	fm = fm.startLoading()
	if fm.NeedsLoad() {
		t.Error("an in-flight load should not be fired again")
	}
	fm = fm.failed(errors.New("nope"), github.ReasonAuthScope)
	if fm.NeedsLoad() {
		t.Error("a failed load should wait for r, not retry on sight")
	}
}

// A reload that fails must not blank a feed the user is reading — the
// previous rows are still the best answer available.
func TestFailedReloadKeepsThePreviousRows(t *testing.T) {
	fm := FeedModel{}
	fm = fm.startLoading()
	fm = fm.loaded([]github.Event{ev("PushEvent", "o/r", 0, "", true, time.Minute)}, time.Now())
	fm = fm.startLoading()
	fm = fm.failed(errors.New("boom"), github.ReasonNetwork)

	if len(fm.events) != 1 {
		t.Fatalf("kept %d events across a failed reload, want 1", len(fm.events))
	}
	out := fm.renderFeed(120, 30, false)
	if !strings.Contains(out, "reload failed") {
		t.Error("the failure is not stated above the stale rows")
	}
	if !strings.Contains(out, "o/r") {
		t.Error("the previous rows were replaced by the error")
	}
}

// A load that has never been attempted and one that came back empty are
// different sentences, and only one of them is a claim about the account.
func TestIdleAndEmptyReadDifferently(t *testing.T) {
	idle := FeedModel{}.renderFeed(120, 30, false)
	if !strings.Contains(idle, "loading") {
		t.Errorf("idle state says %q", idle)
	}
	empty := FeedModel{state: feedReady}.renderFeed(120, 30, false)
	if strings.Contains(empty, "loading") {
		t.Errorf("an empty result still claims to be loading: %q", empty)
	}
}

// Under public-only with nothing public left, the reason has to be the
// mode rather than the account — otherwise the tab tells an active user
// they have done nothing.
func TestPublicOnlyEmptyStateBlamesTheMode(t *testing.T) {
	fm := FeedModel{state: feedReady, events: []github.Event{
		ev("PushEvent", "acme/secret", 0, "", false, time.Minute),
	}}
	out := fm.renderFeed(120, 30, true)
	if !strings.Contains(out, "private repository") {
		t.Errorf("empty-under-public-only reads as %q", out)
	}
}

// The truncation notice is a claim, and below the cap it is a false one.
func TestWindowNoteOnlyClaimsTruncationAtTheCap(t *testing.T) {
	short := FeedModel{state: feedReady, events: []github.Event{
		ev("PushEvent", "o/r", 0, "", true, time.Minute),
	}}
	if got := short.renderFeed(140, 40, false); strings.Contains(got, "not a full history") {
		t.Error("a short feed claims to be truncated")
	}

	full := FeedModel{state: feedReady}
	for i := 0; i < github.EventsPageSize; i++ {
		full.events = append(full.events,
			ev("PushEvent", "o/r", 0, "", true, time.Duration(i)*time.Minute))
	}
	if got := full.renderFeed(140, 40, false); !strings.Contains(got, "not a full history") {
		t.Error("a feed at the page cap does not say so")
	}
}

// The visibility column is worth eight characters only when it separates
// something. Under public-only it never does.
func TestVisColumnAppearsOnlyWhenItSeparatesSomething(t *testing.T) {
	mixed := FeedModel{state: feedReady, events: []github.Event{
		ev("PushEvent", "acme/secret", 0, "", false, time.Minute),
		ev("PushEvent", "octocat/open", 0, "", true, 2*time.Minute),
	}}
	if !strings.Contains(mixed.renderFeed(140, 30, false), "private") {
		t.Error("a mixed feed hides which rows are private")
	}
	if strings.Contains(mixed.renderFeed(140, 30, true), "Vis") {
		t.Error("public-only still spends a column on a constant value")
	}
}

func TestEventPhrase(t *testing.T) {
	mk := func(typ, action, ref, title string, num int) github.Event {
		e := github.Event{Type: typ, Action: action, Ref: ref, Title: title, Number: num}
		if typ == "CreateEvent" || typ == "DeleteEvent" {
			e.RefType = "branch"
		}
		return e
	}
	tests := []struct {
		e          github.Event
		verb, subj string
	}{
		{mk("PushEvent", "", "main", "", 0), "pushed", "main"},
		{mk("PullRequestEvent", "merged", "", "", 12), "merged", "PR #12"},
		{mk("PullRequestEvent", "opened", "", "the fix", 12), "opened", "PR #12 the fix"},
		{mk("PullRequestReviewEvent", "approved", "", "", 12), "approved", "PR #12"},
		{mk("PullRequestReviewEvent", "changes_requested", "", "", 12), "changes req", "PR #12"},
		{mk("PullRequestReviewEvent", "commented", "", "", 12), "reviewed", "PR #12"},
		{mk("IssuesEvent", "closed", "", "Gists section", 76), "closed", "#76 Gists section"},
		{mk("IssueCommentEvent", "created", "", "the tab", 123), "commented", "#123 the tab"},
		{mk("CreateEvent", "", "feat/x", "", 0), "created", "branch feat/x"},
		{mk("DeleteEvent", "", "feat/x", "", 0), "deleted", "branch feat/x"},
		{mk("ReleaseEvent", "published", "v1.2.0", "", 0), "released", "v1.2.0"},
		{mk("WatchEvent", "started", "", "", 0), "starred", ""},
		{mk("ForkEvent", "", "", "", 0), "forked", ""},
		{mk("SomeFutureEvent", "", "", "", 0), "some future", ""},
	}
	for _, tc := range tests {
		verb, subj := eventPhrase(tc.e)
		if verb != tc.verb || subj != tc.subj {
			t.Errorf("%s/%s → (%q, %q), want (%q, %q)",
				tc.e.Type, tc.e.Action, verb, subj, tc.verb, tc.subj)
		}
	}
}

// A PR with no title anywhere in the page shows the bare number rather
// than an invented label.
func TestPullRequestWithoutATitleShowsTheBareNumber(t *testing.T) {
	_, subj := eventPhrase(github.Event{Type: "PullRequestEvent", Action: "opened", Number: 9})
	if subj != "PR #9" {
		t.Errorf("subject = %q, want %q", subj, "PR #9")
	}
}

// A CreateEvent for a whole repository has no ref, and "created branch"
// with an empty name would be worse than saying what happened.
func TestCreatedRepositoryReadsAsARepository(t *testing.T) {
	_, subj := eventPhrase(github.Event{Type: "CreateEvent", RefType: "repository"})
	if subj != "the repository" {
		t.Errorf("subject = %q", subj)
	}
}

// A negative column width panics strings.Repeat, so the narrow case has to
// be arithmetic rather than optimism.
func TestFeedColumnWidthsNeverGoNegative(t *testing.T) {
	for _, showVis := range []bool{false, true} {
		for w := 0; w < 200; w++ {
			repoW, subjectW := feedColumnWidths(w, showVis)
			if repoW < 1 || subjectW < 1 {
				t.Fatalf("available=%d showVis=%v → repo %d, subject %d", w, showVis, repoW, subjectW)
			}
		}
	}
}

// Rendering at absurd sizes must not panic — the terminal can be anything.
func TestRenderFeedSurvivesExtremeGeometry(t *testing.T) {
	fm := FeedModel{state: feedReady, events: []github.Event{
		ev("PushEvent", "octocat/very-long-repository-name-that-keeps-going", 0, "", true, time.Minute),
		ev("IssuesEvent", "o/r", 1, strings.Repeat("long title ", 30), true, 2*time.Minute),
	}}
	for _, w := range []int{0, 1, 20, 80, 400} {
		for _, h := range []int{0, 1, 3, 10, 200} {
			_ = fm.renderFeed(w, h, false)
			_ = fm.renderFeed(w, h, true)
		}
	}
}

func TestMergeVerbsCollapsesAMixedRunToWhatTheyAllAre(t *testing.T) {
	if got := mergeVerbs("reviewed", "reviewed"); got != "reviewed" {
		t.Errorf("a single-verb run should keep its verb, got %q", got)
	}
	if got := mergeVerbs("commented", "reviewed"); got != "commented" {
		t.Errorf("got %q, want %q", got, "commented")
	}
	// Stable once collapsed: a third verb must not push it somewhere else.
	if got := mergeVerbs("commented", "reviewed"); got != mergeVerbs(mergeVerbs("commented", "reviewed"), "reviewed") {
		t.Error("the collapsed label is not stable under further folding")
	}
}

// The PR prefix is what makes a comment row unambiguous, and the title is
// what makes it useful. Both come from the join, not from the event.
func TestSubjectLabelDistinguishesPullRequestsFromIssues(t *testing.T) {
	for _, tc := range []struct {
		isPR   bool
		number int
		title  string
		want   string
	}{
		{true, 123, "the tab", "PR #123 the tab"},
		{true, 123, "", "PR #123"},
		{false, 76, "Gists section", "#76 Gists section"},
		{false, 76, "", "#76"},
		{false, 0, "", ""},
		{true, 0, "", "a pull request"},
	} {
		if got := subjectLabel(tc.isPR, tc.number, tc.title); got != tc.want {
			t.Errorf("subjectLabel(%v, %d, %q) = %q, want %q",
				tc.isPR, tc.number, tc.title, got, tc.want)
		}
	}
}

func TestHumanizeEventType(t *testing.T) {
	for in, want := range map[string]string{
		"PushEvent":        "push",
		"SomeFutureEvent":  "some future",
		"WorkflowJobEvent": "workflow job",
		"Event":            "did something",
		"":                 "did something",
	} {
		if got := humanizeEventType(in); got != want {
			t.Errorf("humanizeEventType(%q) = %q, want %q", in, got, want)
		}
	}
}

// The sub-tab bar costs rows, and the renderer and the viewport sync both
// have to subtract the same number or the heatmap stops two lines early.
func TestActivityBodyHeightSubtractsTheBarExactlyOnce(t *testing.T) {
	if got := activityBodyHeight(0); got != 0 {
		t.Errorf("unknown height should stay unknown, got %d", got)
	}
	if got := activityBodyHeight(30); got != 30-activitySubBarHeight {
		t.Errorf("activityBodyHeight(30) = %d, want %d", got, 30-activitySubBarHeight)
	}
	for h := 1; h < 5; h++ {
		if got := activityBodyHeight(h); got < 1 {
			t.Errorf("activityBodyHeight(%d) = %d, want at least 1", h, got)
		}
	}
}

func TestActivitySubCyclesBothWays(t *testing.T) {
	if got := ActivitySubHeatmap.next(1); got != ActivitySubFeed {
		t.Errorf("right from heatmap = %v", got)
	}
	if got := ActivitySubFeed.next(1); got != ActivitySubHeatmap {
		t.Errorf("right from feed should wrap, got %v", got)
	}
	if got := ActivitySubHeatmap.next(-1); got != ActivitySubFeed {
		t.Errorf("left from heatmap should wrap, got %v", got)
	}
}

// Typing a filter must not leave the search box on a keystroke the box
// should have swallowed.
func TestFeedSearchCapturesKeystrokes(t *testing.T) {
	fm := FeedModel{state: feedReady, events: []github.Event{
		ev("PushEvent", "octocat/one", 0, "", true, time.Minute),
		ev("PushEvent", "acme/two", 0, "", true, 2*time.Minute),
	}}
	fm, _ = fm.Update(key("/"), false)
	if !fm.IsInputMode() {
		t.Fatal("/ did not open the search box")
	}
	for _, r := range "acme" {
		fm, _ = fm.Update(key(string(r)), false)
	}
	fm, _ = fm.Update(key("enter"), false)
	if fm.IsInputMode() {
		t.Error("enter did not close the search box")
	}
	rows := feedRows(fm.events, fm.query, false)
	if len(rows) != 1 || rows[0].event.Repo != "acme/two" {
		t.Errorf("filter %q left %d rows", fm.query, len(rows))
	}

	fm, _ = fm.Update(key("esc"), false)
	if fm.query != "" {
		t.Errorf("esc did not clear the filter, query = %q", fm.query)
	}
}
