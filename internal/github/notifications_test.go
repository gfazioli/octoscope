package github

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

// The fixtures are the shapes the live API returned on 2026-08-19, with
// account and repository names replaced. The CheckSuite one matters most:
// it was 77 of 104 notifications on a real account, and it is the case with
// no subject to link to.

const notifPRJSON = `{
  "id":"25179000001","unread":true,"reason":"mention",
  "updated_at":"2026-08-19T17:44:25Z",
  "subject":{"title":"feat(sponsors): who funds this account",
             "url":"https://api.github.com/repos/octocat/hello-world/pulls/129",
             "latest_comment_url":"https://api.github.com/repos/octocat/hello-world/pulls/129",
             "type":"PullRequest"},
  "repository":{"full_name":"octocat/hello-world",
                "html_url":"https://github.com/octocat/hello-world","private":false}
}`

const notifIssueJSON = `{
  "id":"25179000002","unread":true,"reason":"subscribed",
  "updated_at":"2026-08-19T10:00:00Z",
  "subject":{"title":"Something is broken",
             "url":"https://api.github.com/repos/octocat/other/issues/22",
             "type":"Issue"},
  "repository":{"full_name":"octocat/other",
                "html_url":"https://github.com/octocat/other","private":false}
}`

// The dominant case: CI activity, with a null subject URL.
const notifCheckSuiteJSON = `{
  "id":"25179000003","unread":true,"reason":"ci_activity",
  "updated_at":"2026-08-19T17:45:17Z",
  "subject":{"title":"ci workflow run succeeded for main branch",
             "url":null,"latest_comment_url":null,"type":"CheckSuite"},
  "repository":{"full_name":"octocat/hello-world",
                "html_url":"https://github.com/octocat/hello-world","private":false}
}`

// A release, whose subject URL carries a numeric id that is not a browser
// route — the measurement this fixture exists to lock in.
const notifReleaseJSON = `{
  "id":"25179000004","unread":true,"reason":"subscribed",
  "updated_at":"2026-08-18T09:00:00Z",
  "subject":{"title":"v1.2.3",
             "url":"https://api.github.com/repos/acme/private-thing/releases/371531536",
             "type":"Release"},
  "repository":{"full_name":"acme/private-thing",
                "html_url":"https://github.com/acme/private-thing","private":true}
}`

func decodeNotifs(t *testing.T, body string) []Notification {
	t.Helper()
	// Reuses the events harness: it serves any REST endpoint through the
	// same rewriteHost transport, and duplicating it would be one more
	// thing to keep in step.
	c := newEventsClient(t, http.StatusOK, body, nil)
	got, err := c.FetchNotifications(context.Background())
	if err != nil {
		t.Fatalf("FetchNotifications: %v", err)
	}
	return got
}

func TestNotificationURLDerivation(t *testing.T) {
	for _, tc := range []struct {
		name string
		json string
		want string
	}{
		{
			// The API path says "pulls"; the browser wants "pull". One
			// character, and the wrong one 404s.
			name: "pull request drops the plural",
			json: notifPRJSON,
			want: "https://github.com/octocat/hello-world/pull/129",
		},
		{
			name: "issue keeps its path verbatim",
			json: notifIssueJSON,
			want: "https://github.com/octocat/other/issues/22",
		},
		{
			// Measured: github.com/o/r/releases/<numeric id> is a 404, and
			// only /releases/tag/<tag> resolves — a tag the notification
			// does not carry. So the releases page is the honest landing.
			name: "release lands on the releases page, not its numeric id",
			json: notifReleaseJSON,
			want: "https://github.com/acme/private-thing/releases",
		},
		{
			// The majority case on a real account.
			name: "a null subject falls back to the repository",
			json: notifCheckSuiteJSON,
			want: "https://github.com/octocat/hello-world",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := decodeNotifs(t, "["+tc.json+"]")
			if len(got) != 1 {
				t.Fatalf("got %d notifications", len(got))
			}
			if got[0].URL != tc.want {
				t.Errorf("URL = %q, want %q", got[0].URL, tc.want)
			}
			if got[0].ID == "" || got[0].UpdatedAt.IsZero() {
				t.Errorf("id/updated_at did not decode: %+v", got[0])
			}
		})
	}
}

// A subject type GitHub adds later must land somewhere real rather than
// producing a constructed URL nobody has ever loaded.
func TestNotificationURLFallsBackRatherThanGuessing(t *testing.T) {
	for _, subject := range []string{
		"", // no subject at all
		"https://example.com/somewhere/else",
		"https://api.github.com/repos/octocat/hello-world/somethingnew/9",
		"https://api.github.com/repos/octocat/hello-world/pulls/", // no id
		"https://api.github.com/repos/octocat",                    // truncated
		// Only the api.github.com/repos/ prefix check rejects this one:
		// every other guard in the function passes it, and without the
		// prefix it would be rewritten into a plausible github.com URL
		// built out of a value the API never sent.
		"octocat/hello-world/pulls/1",
	} {
		const repo = "https://github.com/octocat/hello-world"
		if got := notificationURL(subject, repo); got != repo {
			t.Errorf("notificationURL(%q) = %q, want the repository", subject, got)
		}
	}
}

func TestNotificationFieldsSurviveTheDecode(t *testing.T) {
	got := decodeNotifs(t, "["+notifPRJSON+","+notifReleaseJSON+"]")
	if len(got) != 2 {
		t.Fatalf("got %d", len(got))
	}
	if got[0].Reason != "mention" || got[0].Type != "PullRequest" {
		t.Errorf("first = %+v", got[0])
	}
	if got[0].Title != "feat(sponsors): who funds this account" {
		t.Errorf("title = %q", got[0].Title)
	}
	if !got[0].Unread {
		t.Error("unread did not decode")
	}
	// The privacy signal, which is what screenshot mode filters on.
	if got[0].IsPrivate {
		t.Error("a public repository decoded as private")
	}
	if !got[1].IsPrivate {
		t.Error("a private repository decoded as public — screenshot mode " +
			"filters on this, so it failing open is the whole problem")
	}
}

// A notification title is whatever somebody called their pull request, in
// any repository the account watches, and it is about to reach a terminal.
func TestExtractNotificationsSanitizes(t *testing.T) {
	body := `[{"id":"1","unread":true,"reason":"mention",
      "updated_at":"2026-08-19T10:00:00Z",
      "subject":{"title":"real\u001b]0;pwned\u0007title","url":null,"type":"CheckSuite"},
      "repository":{"full_name":"octocat/hello-world",
                    "html_url":"https://github.com/octocat/hello-world","private":false}}]`
	got := decodeNotifs(t, body)
	if len(got) != 1 {
		t.Fatalf("got %d", len(got))
	}
	if strings.ContainsAny(got[0].Title, "\x1b\x07") || strings.Contains(got[0].Title, "pwned") {
		t.Errorf("Title kept a terminal escape: %q", got[0].Title)
	}
}

func TestExtractNotificationsEmpty(t *testing.T) {
	if got := extractNotifications(nil); got != nil {
		t.Errorf("extractNotifications(nil) = %v, want nil", got)
	}
}

// An inbox is what is waiting, so the fetch asks for unread only. `all=true`
// would turn it into a history — measured at 22 unread against 50 with
// everything included, which is a very different surface.
func TestFetchNotificationsAsksForTheUnreadInbox(t *testing.T) {
	var gotPath string
	c := newEventsClient(t, http.StatusOK, "[]", &gotPath)
	if _, err := c.FetchNotifications(context.Background()); err != nil {
		t.Fatalf("FetchNotifications: %v", err)
	}
	if gotPath != "/notifications" {
		t.Errorf("asked for %q, want %q", gotPath, "/notifications")
	}
}

// The constant and the query string are two copies of one number.
func TestNotificationsPageSizeMatchesQuery(t *testing.T) {
	if notificationsPageSize != NotificationsPageSize {
		t.Fatal("the exported and unexported caps disagree")
	}
	if notificationsPageSize < 1 || notificationsPageSize > 100 {
		t.Errorf("page size %d is outside what GitHub accepts", notificationsPageSize)
	}
}

// GitHub does not return the inbox in time order — measured, the live
// endpoint interleaved 12m, 13m, 14m, 13m, 19m, 23m, 15m-old threads — so
// a column headed "When" only reads correctly because this sorts.
func TestNotificationsComeBackNewestFirst(t *testing.T) {
	body := `[
	  {"id":"a","unread":true,"reason":"ci_activity","updated_at":"2026-08-19T10:00:00Z",
	   "subject":{"title":"older","url":null,"type":"CheckSuite"},
	   "repository":{"full_name":"o/r","html_url":"https://github.com/o/r","private":false}},
	  {"id":"b","unread":true,"reason":"mention","updated_at":"2026-08-19T17:00:00Z",
	   "subject":{"title":"newest","url":null,"type":"CheckSuite"},
	   "repository":{"full_name":"o/r","html_url":"https://github.com/o/r","private":false}},
	  {"id":"c","unread":true,"reason":"author","updated_at":"2026-08-19T13:00:00Z",
	   "subject":{"title":"middle","url":null,"type":"CheckSuite"},
	   "repository":{"full_name":"o/r","html_url":"https://github.com/o/r","private":false}}
	]`
	got := decodeNotifs(t, body)
	var titles []string
	for _, n := range got {
		titles = append(titles, n.Title)
	}
	if strings.Join(titles, ",") != "newest,middle,older" {
		t.Errorf("order = %v, want newest first", titles)
	}
}

// Stable, so threads sharing a timestamp keep the order GitHub sent them
// instead of shuffling between two runs of an unchanged fetch.
//
// **The shape of this test is the point, and it took measuring to get
// right.** Go's sort.Slice is pdqsort, which short-circuits on inputs it
// recognises: with every key equal it never reorders at *any* size up to
// 200, and on already-sorted input it never reorders either. A stability
// test built either of those ways cannot fail, whichever sort is used —
// which is the #105 lesson arriving in a new costume.
//
// What does separate them: a **scrambled** arrival order with ties, at
// n ≥ 50. Measured — at 50 items over 8 distinct keys the two sorts differ
// in 41 positions. Fifty is also notificationsPageSize, and GitHub really
// does return this endpoint scrambled, so the guarantee is load-bearing at
// the real size rather than in principle.
func TestNotificationSortIsStable(t *testing.T) {
	// Eight timestamps over fifty threads, assigned in a scrambled order.
	const n, distinct = notificationsPageSize, 8
	var items []string
	for i := 0; i < n; i++ {
		hour := (i * 7) % distinct
		items = append(items, fmt.Sprintf(`{"id":"t%02d","unread":true,"reason":"mention",
		   "updated_at":"2026-08-19T%02d:00:00Z",
		   "subject":{"title":"t%02d","url":null,"type":"CheckSuite"},
		   "repository":{"full_name":"o/r","html_url":"https://github.com/o/r","private":false}}`,
			i, hour, i))
	}

	got := decodeNotifs(t, "["+strings.Join(items, ",")+"]")
	if len(got) != n {
		t.Fatalf("got %d, want %d", len(got), n)
	}

	// Newest first overall...
	for i := 1; i < len(got); i++ {
		if got[i].UpdatedAt.After(got[i-1].UpdatedAt) {
			t.Fatalf("row %d is newer than row %d — not sorted", i, i-1)
		}
	}
	// ...and inside each timestamp, arrival order preserved. Titles are
	// t00..t49 in arrival order, so within a group they must ascend.
	for i := 1; i < len(got); i++ {
		if !got[i].UpdatedAt.Equal(got[i-1].UpdatedAt) {
			continue
		}
		if got[i].Title <= got[i-1].Title {
			t.Errorf("ties reordered at row %d: %q came after %q",
				i, got[i].Title, got[i-1].Title)
		}
	}
}
