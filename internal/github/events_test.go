package github

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The fixtures below are the *shapes* the live API returned on
// 2026-08-08, with repository and account names replaced. The shapes are
// the point — several of them contradict GitHub's published reference,
// and a test written from the docs instead of the wire would have locked
// in fields that never arrive.

const pushEventJSON = `{
  "id":"17119310465","type":"PushEvent","public":true,
  "created_at":"2026-08-08T10:18:51Z",
  "repo":{"name":"octocat/hello-world"},
  "payload":{"before":"c3d9847e7b1c983b2081ecaea63db85ae19a0f54",
             "head":"9ef24dcdc5ee3a9c984d45002e83c94a77135173",
             "push_id":39436460914,"ref":"refs/heads/main","repository_id":1}
}`

// The pull_request object as it actually arrives: five fields, no title,
// no html_url. This fixture is the executable record of that finding.
const prEventJSON = `{
  "id":"17119000001","type":"PullRequestEvent","public":true,
  "created_at":"2026-08-08T09:52:00Z",
  "repo":{"name":"octocat/hello-world"},
  "payload":{"action":"merged","number":123,
             "pull_request":{"base":{},"head":{},"id":99,"number":123,
                             "url":"https://api.github.com/repos/octocat/hello-world/pulls/123"}}
}`

const issuesEventJSON = `{
  "id":"17118000002","type":"IssuesEvent","public":true,
  "created_at":"2026-08-08T09:30:00Z",
  "repo":{"name":"octocat/hello-world"},
  "payload":{"action":"closed",
             "issue":{"number":76,"title":"Gists section",
                      "html_url":"https://github.com/octocat/hello-world/issues/76"}}
}`

const issueCommentEventJSON = `{
  "id":"17117000003","type":"IssueCommentEvent","public":true,
  "created_at":"2026-08-08T08:14:00Z",
  "repo":{"name":"octocat/hello-world"},
  "payload":{"action":"created",
             "issue":{"number":123,"title":"feat(gists): the Gists tab",
                      "html_url":"https://github.com/octocat/hello-world/pull/123",
                      "pull_request":{"url":"https://api.github.com/repos/octocat/hello-world/pulls/123"}},
             "comment":{"html_url":"https://github.com/octocat/hello-world/pull/123#issuecomment-5225673218"}}
}`

const reviewEventJSON = `{
  "id":"17116000004","type":"PullRequestReviewEvent","public":true,
  "created_at":"2026-08-08T08:00:00Z",
  "repo":{"name":"octocat/hello-world"},
  "payload":{"action":"created",
             "pull_request":{"id":99,"number":123,
                             "url":"https://api.github.com/repos/octocat/hello-world/pulls/123"},
             "review":{"state":"commented",
                       "html_url":"https://github.com/octocat/hello-world/pull/123#pullrequestreview-4888623480"}}
}`

const releaseEventJSON = `{
  "id":"17115000005","type":"ReleaseEvent","public":false,
  "created_at":"2026-08-07T18:00:00Z",
  "repo":{"name":"acme/private-thing"},
  "payload":{"action":"published",
             "release":{"tag_name":"v26.4.0-rc.0","name":"v26.4.0-rc.0",
                        "html_url":"https://github.com/acme/private-thing/releases/tag/v26.4.0-rc.0"}}
}`

const createBranchEventJSON = `{
  "id":"17114000006","type":"CreateEvent","public":true,
  "created_at":"2026-08-07T12:00:00Z",
  "repo":{"name":"octocat/hello-world"},
  "payload":{"ref":"docs/next-version-script-trap","ref_type":"branch",
             "master_branch":"main","pusher_type":"user"}
}`

const deleteBranchEventJSON = `{
  "id":"17113000007","type":"DeleteEvent","public":true,
  "created_at":"2026-08-07T11:00:00Z",
  "repo":{"name":"octocat/hello-world"},
  "payload":{"ref":"feat/gists-tab","ref_type":"branch","pusher_type":"user"}
}`

func decodeOne(t *testing.T, body string) Event {
	t.Helper()
	events := decodeMany(t, "["+body+"]")
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	return events[0]
}

// decodeMany drives the real decode path — json into eventJSON, then
// extractEvents — so a struct tag typo fails the test rather than
// silently producing a zero value.
func decodeMany(t *testing.T, body string) []Event {
	t.Helper()
	c := newEventsClient(t, http.StatusOK, body, nil)
	events, err := c.FetchEvents(context.Background(), "octocat")
	if err != nil {
		t.Fatalf("FetchEvents: %v", err)
	}
	return events
}

// newEventsClient serves one canned response and records the path it was
// asked for, which is what the privacy test asserts on.
func newEventsClient(t *testing.T, code int, body string, gotPath *string, headers ...[2]string) *Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if gotPath != nil {
			*gotPath = r.URL.Path
		}
		w.Header().Set("Content-Type", "application/json")
		for _, h := range headers {
			w.Header().Set(h[0], h[1])
		}
		w.WriteHeader(code)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return &Client{
		rest:          &http.Client{Transport: &rewriteHost{host: srv.URL}},
		authenticated: true,
	}
}

func TestExtractEventsPerType(t *testing.T) {
	tests := []struct {
		name    string
		json    string
		wantTyp string
		wantNum int
		wantTtl string
		wantAct string
		wantRef string
		wantURL string
	}{
		{
			name: "push links to the compare view and shortens the ref",
			json: pushEventJSON, wantTyp: "PushEvent", wantRef: "main",
			wantURL: "https://github.com/octocat/hello-world/compare/" +
				"c3d9847e7b1c983b2081ecaea63db85ae19a0f54...9ef24dcdc5ee3a9c984d45002e83c94a77135173",
		},
		{
			name: "pull request: number survives, title does not exist, url is derived",
			json: prEventJSON, wantTyp: "PullRequestEvent", wantNum: 123, wantAct: "merged",
			wantURL: "https://github.com/octocat/hello-world/pull/123",
		},
		{
			name: "issue carries its own title and html_url",
			json: issuesEventJSON, wantTyp: "IssuesEvent", wantNum: 76,
			wantTtl: "Gists section", wantAct: "closed",
			wantURL: "https://github.com/octocat/hello-world/issues/76",
		},
		{
			name: "issue comment prefers the comment anchor over the issue",
			json: issueCommentEventJSON, wantTyp: "IssueCommentEvent", wantNum: 123,
			wantTtl: "feat(gists): the Gists tab", wantAct: "created",
			wantURL: "https://github.com/octocat/hello-world/pull/123#issuecomment-5225673218",
		},
		{
			name: "review prefers the review anchor and reports its state, not the action",
			json: reviewEventJSON, wantTyp: "PullRequestReviewEvent", wantNum: 123, wantAct: "commented",
			wantURL: "https://github.com/octocat/hello-world/pull/123#pullrequestreview-4888623480",
		},
		{
			name: "release uses the release html_url and carries its tag as the ref",
			json: releaseEventJSON, wantTyp: "ReleaseEvent", wantAct: "published",
			wantRef: "v26.4.0-rc.0",
			wantURL: "https://github.com/acme/private-thing/releases/tag/v26.4.0-rc.0",
		},
		{
			name: "created branch links at the tree",
			json: createBranchEventJSON, wantTyp: "CreateEvent", wantRef: "docs/next-version-script-trap",
			wantURL: "https://github.com/octocat/hello-world/tree/docs/next-version-script-trap",
		},
		{
			name: "deleted branch has nothing left to link to but the repo",
			json: deleteBranchEventJSON, wantTyp: "DeleteEvent", wantRef: "feat/gists-tab",
			wantURL: "https://github.com/octocat/hello-world",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			e := decodeOne(t, tc.json)
			if e.Type != tc.wantTyp {
				t.Errorf("Type = %q, want %q", e.Type, tc.wantTyp)
			}
			if e.Number != tc.wantNum {
				t.Errorf("Number = %d, want %d", e.Number, tc.wantNum)
			}
			if e.Title != tc.wantTtl {
				t.Errorf("Title = %q, want %q", e.Title, tc.wantTtl)
			}
			if e.Action != tc.wantAct {
				t.Errorf("Action = %q, want %q", e.Action, tc.wantAct)
			}
			if e.Ref != tc.wantRef {
				t.Errorf("Ref = %q, want %q", e.Ref, tc.wantRef)
			}
			if e.URL != tc.wantURL {
				t.Errorf("URL  = %q\nwant  = %q", e.URL, tc.wantURL)
			}
			if e.ID == "" {
				t.Error("ID is empty — the row would have no stable key")
			}
			if e.CreatedAt.IsZero() {
				t.Error("CreatedAt is zero — created_at did not decode")
			}
		})
	}
}

// The measured truncation, locked in. If GitHub ever starts sending the
// full pull_request object this fails, which is the correct outcome: the
// UI's "no title for PRs" copy would then be a lie and needs revisiting.
func TestPullRequestPayloadCarriesNoTitle(t *testing.T) {
	e := decodeOne(t, prEventJSON)
	if e.Title != "" {
		t.Fatalf("PullRequestEvent now carries a title (%q) — GitHub changed the "+
			"payload; revisit the UI copy that says PR rows have none", e.Title)
	}
	if !strings.HasSuffix(e.URL, "/pull/123") {
		t.Fatalf("URL = %q, want it derived from repo + number", e.URL)
	}
}

func TestPushEventOnANewBranchAvoidsTheZeroSHACompare(t *testing.T) {
	const newBranch = `{
      "id":"1","type":"PushEvent","public":true,
      "created_at":"2026-08-08T10:00:00Z",
      "repo":{"name":"octocat/hello-world"},
      "payload":{"before":"0000000000000000000000000000000000000000",
                 "head":"abc123","ref":"refs/heads/feat/new"}
    }`
	e := decodeOne(t, newBranch)
	want := "https://github.com/octocat/hello-world/commits/feat/new"
	if e.URL != want {
		t.Errorf("URL = %q, want %q (a compare against the zero SHA 404s)", e.URL, want)
	}
}

// An issue title is written by whoever opened the issue, in any repo the
// account touched, and lands in a terminal. Sanitize is the boundary.
//
// The escape arrives as `\u001b`, not as a raw byte, and that is not a
// convenience for the fixture: JSON forbids raw control characters inside
// string literals, so encoding/json would reject the response outright.
// GitHub therefore always sends the \u form and the raw byte only exists
// after the decode — which is exactly why Sanitize runs here, on the
// decoded value, rather than over the response body.
func TestExtractEventsSanitizesGitHubText(t *testing.T) {
	body := `{"id":"1","type":"IssuesEvent","public":true,
      "created_at":"2026-08-08T10:00:00Z",
      "repo":{"name":"octocat/hello-world"},
      "payload":{"action":"opened",
                 "issue":{"number":1,"title":"real\u001b]0;pwned\u0007title",
                          "html_url":"https://github.com/octocat/hello-world/issues/1"}}}`
	e := decodeOne(t, body)
	if strings.Contains(e.Title, "\x1b") {
		t.Errorf("Title kept an escape sequence: %q", e.Title)
	}
	// The whole OSC goes, payload included. That is the part that matters:
	// `ESC ] 0 ; ... BEL` retitles the user's terminal window, so leaving
	// "pwned" behind while dropping the introducer would still hand an
	// issue author a line of someone else's screen.
	if strings.Contains(e.Title, "pwned") {
		t.Errorf("Title kept the OSC payload: %q", e.Title)
	}
	if e.Title != "realtitle" {
		t.Errorf("Title = %q, want %q", e.Title, "realtitle")
	}
}

// The privacy invariant, and the reason it is an endpoint rather than a
// filter: under public-only the private events are never fetched, so
// there is nothing in memory to leak on the next paint.
func TestFetchEventsPublicOnlyAsksTheOtherEndpoint(t *testing.T) {
	for _, tc := range []struct {
		publicOnly bool
		wantPath   string
	}{
		{false, "/users/octocat/events"},
		{true, "/users/octocat/events/public"},
	} {
		var got string
		c := newEventsClient(t, http.StatusOK, "[]", &got)
		c.publicOnly = tc.publicOnly
		if _, err := c.FetchEvents(context.Background(), "octocat"); err != nil {
			t.Fatalf("publicOnly=%v: %v", tc.publicOnly, err)
		}
		if got != tc.wantPath {
			t.Errorf("publicOnly=%v asked for %q, want %q", tc.publicOnly, got, tc.wantPath)
		}
	}
}

func TestFetchEventsNeedsALogin(t *testing.T) {
	c := newEventsClient(t, http.StatusOK, "[]", nil)
	for _, login := range []string{"", "   "} {
		if _, err := c.FetchEvents(context.Background(), login); err == nil {
			t.Errorf("login %q was accepted; the endpoint has no viewer form", login)
		}
	}
}

// GitHub overloads 403 across three unrelated conditions and the advice for
// each differs, so the headers — not the status alone, and not the body's
// wording — are what separate them.
func TestFetchEventsClassifiesFailures(t *testing.T) {
	for _, tc := range []struct {
		name    string
		code    int
		headers [][2]string
		want    FetchErrorReason
	}{
		{"401 is a rejected token, not a missing scope",
			http.StatusUnauthorized, nil, ReasonAuth},
		{"bare 403 is a permission problem",
			http.StatusForbidden, nil, ReasonAuthScope},
		{"403 with the budget at zero is the hourly limit",
			http.StatusForbidden, [][2]string{{"X-RateLimit-Remaining", "0"}}, ReasonRateLimitPrimary},
		{"403 with Retry-After is the secondary throttle",
			http.StatusForbidden, [][2]string{{"Retry-After", "60"}}, ReasonRateLimitSecondary},
		{"Retry-After wins over a spent budget — it carries the wait",
			http.StatusForbidden,
			[][2]string{{"Retry-After", "60"}, {"X-RateLimit-Remaining", "0"}},
			ReasonRateLimitSecondary},
		{"403 with budget left is still a permission problem",
			http.StatusForbidden, [][2]string{{"X-RateLimit-Remaining", "4999"}}, ReasonAuthScope},
		{"429 is never a permission problem, whatever the headers say",
			http.StatusTooManyRequests, nil, ReasonRateLimitSecondary},
		{"404", http.StatusNotFound, nil, ReasonNotFound},
		{"5xx", http.StatusBadGateway, nil, ReasonServer},
		{"anything else", http.StatusTeapot, nil, ReasonUnknown},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := newEventsClient(t, tc.code, `{"message":"nope"}`, nil, tc.headers...)
			_, err := c.FetchEvents(context.Background(), "octocat")
			var fe *FetchError
			if !errors.As(err, &fe) {
				t.Fatalf("status %d: want a *FetchError, got %v", tc.code, err)
			}
			if fe.Reason != tc.want {
				t.Errorf("status %d classified as %v, want %v", tc.code, fe.Reason, tc.want)
			}
		})
	}
}

func TestShortRefTrimsOnlyTheRefspecPrefix(t *testing.T) {
	for in, want := range map[string]string{
		"refs/heads/main":             "main",
		"refs/tags/v1.0.0":            "v1.0.0",
		"main":                        "main",
		"":                            "",
		"refs/heads/refs/heads/weird": "refs/heads/weird",
	} {
		if got := shortRef(in); got != want {
			t.Errorf("shortRef(%q) = %q, want %q", in, got, want)
		}
	}
}

// An unrecognised type must still produce a usable row — GitHub adds event
// types, and a feed that dropped them would silently under-report.
func TestUnknownEventTypeStillLandsOnTheRepo(t *testing.T) {
	const future = `{"id":"1","type":"SomeFutureEvent","public":true,
      "created_at":"2026-08-08T10:00:00Z",
      "repo":{"name":"octocat/hello-world"},"payload":{}}`
	e := decodeOne(t, future)
	if e.Type != "SomeFutureEvent" {
		t.Errorf("Type = %q, want it kept verbatim", e.Type)
	}
	if e.URL != "https://github.com/octocat/hello-world" {
		t.Errorf("URL = %q, want the repo as the fallback", e.URL)
	}
}

func TestExtractEventsEmpty(t *testing.T) {
	if got := extractEvents(nil); got != nil {
		t.Errorf("extractEvents(nil) = %v, want nil", got)
	}
}

// A review's action is "created" on every review event GitHub sends, so
// the state is the only thing that separates an approval from a comment.
func TestReviewEventReportsItsState(t *testing.T) {
	approved := strings.Replace(reviewEventJSON, `"state":"commented"`, `"state":"approved"`, 1)
	if e := decodeOne(t, approved); e.Action != "approved" {
		t.Errorf("Action = %q, want %q", e.Action, "approved")
	}
}

// The join that turns a bare "PR #123" into a titled row, using only what
// the page already contains.
func TestBackfillTitlesFillsPullRequestsFromSiblingComments(t *testing.T) {
	events := decodeMany(t, "["+prEventJSON+","+issueCommentEventJSON+"]")
	if len(events) != 2 {
		t.Fatalf("got %d events", len(events))
	}
	const want = "feat(gists): the Gists tab"
	if events[0].Title != want {
		t.Errorf("PullRequestEvent title = %q, want it backfilled to %q", events[0].Title, want)
	}
	if events[1].Title != want {
		t.Errorf("IssueCommentEvent lost its own title: %q", events[1].Title)
	}
}

// Nothing is invented: without a sibling naming the same repo and number,
// the title stays empty and the UI shows the bare number.
func TestBackfillTitlesInventsNothing(t *testing.T) {
	otherRepo := strings.ReplaceAll(issueCommentEventJSON, "octocat/hello-world", "octocat/elsewhere")
	events := decodeMany(t, "["+prEventJSON+","+otherRepo+"]")
	if events[0].Title != "" {
		t.Errorf("title crossed repositories: %q", events[0].Title)
	}
}

// GitHub files pull-request comments under the issue shape, so the type
// cannot say which one a comment is about — `payload.issue.pull_request`
// is the only thing that can, and the UI labels the row from it.
func TestIsPullRequestSeparatesCommentsOnPRsFromCommentsOnIssues(t *testing.T) {
	onPR := decodeOne(t, issueCommentEventJSON)
	if !onPR.IsPullRequest {
		t.Error("a comment on a pull request was not recognised as one")
	}
	onIssue := decodeOne(t, issuesEventJSON)
	if onIssue.IsPullRequest {
		t.Error("an issue event was mistaken for a pull request")
	}
	// A review event's own payload never says, so it inherits the answer
	// from the sibling the title came from.
	both := decodeMany(t, "["+reviewEventJSON+","+issueCommentEventJSON+"]")
	if !both[0].IsPullRequest {
		t.Error("the review did not inherit its kind from the sibling comment")
	}
}
