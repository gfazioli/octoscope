package github

// Notifications — the real GitHub inbox (#74), distinct from the
// delta-driven OS notifications octoscope already fires on changes it spots
// between refreshes.
//
// **REST-only**, like the events feed and for the same reason: GraphQL does
// not expose the inbox at all. Measured against the live API on 2026-08-19
// with the token octoscope already asks for — the `repo` scope covers
// `/notifications`, so no widening is needed.
//
// Four measured facts shaped this, and three of them are about what the
// endpoint *cannot* tell you:
//
//  1. **`subject.url` is an API URL when it exists, and often it does not.**
//     Of 104 notifications on a real account, **77 were CheckSuite** — CI
//     activity — and every one carried `subject.url: null`. So a majority of
//     an inbox has no subject to link to, and the derivation below has to
//     degrade to the repository rather than build a URL out of nothing.
//  2. **A pull request's API path says `pulls`, the browser's says `pull`.**
//     One character, and getting it wrong produces a link that 404s.
//  3. **A release's subject URL carries its numeric id, which is not a
//     browser route.** `github.com/o/r/releases/365498256` answers **404**
//     (measured); only `/releases/tag/<tag>` works, and the notification
//     does not carry the tag. So a release links at the releases page —
//     honest, and one request cheaper than resolving the tag.
//  4. **`X-Poll-Interval: 60`**, the same as the events feed, which is why
//     this is fetched on demand rather than on the dashboard refresh.
//
// **Read-only, deliberately.** Marking a thread read is a `PATCH`, and
// octoscope does not mutate GitHub state. The issue named the alternative —
// an `--allow-mutations` opt-in — and it was declined with the maintainer:
// that flag would make this a product with a different trust model, which
// is not a thing to decide inside a release cycle. The inbox links out for
// the mutation instead.

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"
)

// notificationsPageSize bounds one fetch. A single page, like every other
// list here: an inbox longer than this is one nobody is reading through a
// dashboard, and the count comes back so a truncated list can say so.
const notificationsPageSize = 50

// NotificationsPageSize is the cap the UI discloses.
const NotificationsPageSize = notificationsPageSize

// Notification is one inbox thread.
type Notification struct {
	// ID is GitHub's thread id — the stable key for a row, and what a
	// future mark-as-read would need if the trust model ever changed.
	ID string

	// Reason is why this is in the inbox, verbatim: "mention",
	// "review_requested", "author", "subscribed", "ci_activity",
	// "team_mention", "assign", "state_change", "security_alert"… An open
	// set, kept as GitHub's own word rather than an enum.
	Reason string

	// Type is the subject's type: "PullRequest", "Issue", "Release",
	// "CheckSuite", "Discussion", "Commit". Also open.
	Type string

	// Title is the subject's title. For a CheckSuite it is a sentence
	// GitHub composes ("ci workflow run succeeded for main branch"), not
	// something a person wrote.
	Title string

	// Repo is "owner/name"; RepoURL is its browser URL.
	Repo    string
	RepoURL string

	// URL is where to land in a browser. Derived from subject.url when
	// there is one — see the file comment for the two traps — and the
	// repository otherwise. Never empty for a notification that has a
	// repository, because a row you cannot open is not worth a row.
	URL string

	// Unread mirrors the API's own flag. The default fetch asks only for
	// unread threads, so this is true for everything in practice; it is
	// carried anyway because `all=true` is one parameter away and a field
	// that silently means one thing is how the next reader gets it wrong.
	Unread bool

	// UpdatedAt is when the thread last changed, in UTC.
	UpdatedAt time.Time

	// IsPrivate is the notification's repository visibility, and the
	// privacy signal for screenshot mode. Measured: 17 of 104 on a real
	// account. Unlike sponsors, this one *is* a per-item flag, so the
	// filter is a skip loop rather than dropping the surface.
	IsPrivate bool
}

// notificationJSON is the wire shape; only what is read is declared.
type notificationJSON struct {
	ID        string    `json:"id"`
	Reason    string    `json:"reason"`
	Unread    bool      `json:"unread"`
	UpdatedAt time.Time `json:"updated_at"`
	Subject   struct {
		Title string `json:"title"`
		// Null for CheckSuite and Discussion, which is the common case
		// rather than the edge one — hence the pointer.
		URL  *string `json:"url"`
		Type string  `json:"type"`
	} `json:"subject"`
	Repository struct {
		FullName string `json:"full_name"`
		HTMLURL  string `json:"html_url"`
		Private  bool   `json:"private"`
	} `json:"repository"`
}

// FetchNotifications returns the account's unread inbox, newest first.
//
// **The sort is ours, and it has to be.** GitHub does not return this
// endpoint in any time order — measured on a real account, the first
// eighteen threads ran 12m, 12m, 13m, 14m, 14m, 13m, 19m, 23m, 15m, 28m,
// 45m, 46m, 1h, 1h, 12m, 2h, 2h, 5h old. Whatever that ordering is, a
// column headed "When" that walks backwards and then forwards again reads
// as a bug, and nobody scanning an inbox is looking for GitHub's internal
// ordering. Found by running the tab, not by a test.
//
// Unread-only by design: an inbox is what is waiting, and `all=true` turns
// it into a history nobody asked for — measured, 22 unread against 50 with
// everything included.
//
// The endpoint is viewer-scoped with no login parameter, so unlike the
// events feed there is nothing to pass. That also means it is meaningless
// when octoscope is showing somebody else's profile: it would answer with
// *your* inbox under their name, which is the caller's problem to avoid and
// is why the UI gates it on viewer mode.
func (c *Client) FetchNotifications(ctx context.Context) ([]Notification, error) {
	url := fmt.Sprintf("https://api.github.com/notifications?per_page=%d", notificationsPageSize)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, &FetchError{Reason: ReasonUnknown, Err: err}
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	resp, err := c.rest.Do(req)
	if err != nil {
		return nil, &FetchError{Reason: classifyErr(ctx, err), Err: err}
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, &FetchError{
			Reason: classifyStatus(resp.StatusCode, resp.Header),
			Err:    fmt.Errorf("GitHub answered %d for the notifications inbox", resp.StatusCode),
		}
	}

	var raw []notificationJSON
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, &FetchError{Reason: ReasonUnknown, Err: err}
	}
	return extractNotifications(raw), nil
}

// extractNotifications is the pure half. Every GitHub-sourced string
// crosses Sanitize here: a notification title is whatever somebody called
// their pull request, in any repository the account watches.
func extractNotifications(raw []notificationJSON) []Notification {
	if len(raw) == 0 {
		return nil
	}
	out := make([]Notification, 0, len(raw))
	for _, r := range raw {
		n := Notification{
			ID:        Sanitize(r.ID),
			Reason:    Sanitize(r.Reason),
			Type:      Sanitize(r.Subject.Type),
			Title:     Sanitize(r.Subject.Title),
			Repo:      Sanitize(r.Repository.FullName),
			RepoURL:   Sanitize(r.Repository.HTMLURL),
			Unread:    r.Unread,
			UpdatedAt: r.UpdatedAt.UTC(),
			IsPrivate: r.Repository.Private,
		}
		var subject string
		if r.Subject.URL != nil {
			subject = Sanitize(*r.Subject.URL)
		}
		n.URL = notificationURL(subject, n.RepoURL)
		out = append(out, n)
	}
	// Newest first — see FetchNotifications. Stable, so threads sharing a
	// timestamp keep whatever order GitHub gave them rather than shuffling
	// between two runs of the same fetch.
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].UpdatedAt.After(out[j].UpdatedAt)
	})
	return out
}

// apiRepoPrefix is the API path every subject URL starts with.
const apiRepoPrefix = "https://api.github.com/repos/"

// notificationURL turns a subject's API URL into a browser one, falling
// back to the repository whenever it cannot.
//
// Three cases are handled and the rest fall back, on purpose: a link that
// lands on the repository is a mild disappointment, and one that 404s is a
// bug the user reports. See the file comment for the measurements.
func notificationURL(subjectURL, repoURL string) string {
	if subjectURL == "" || !strings.HasPrefix(subjectURL, apiRepoPrefix) {
		return repoURL
	}
	rest := strings.TrimPrefix(subjectURL, apiRepoPrefix)
	parts := strings.Split(rest, "/")
	// owner / name / kind / id
	if len(parts) < 4 || parts[0] == "" || parts[1] == "" {
		return repoURL
	}
	base := "https://github.com/" + parts[0] + "/" + parts[1]
	kind, id := parts[2], parts[3]
	if id == "" {
		return repoURL
	}

	switch kind {
	case "pulls":
		// The API says "pulls", the browser says "pull". One character,
		// and the wrong one is a 404.
		return base + "/pull/" + id
	case "issues":
		return base + "/issues/" + id
	case "releases":
		// The subject URL carries the release's numeric id, and
		// github.com/o/r/releases/<id> answers 404 — measured. Only the
		// tag form resolves, and the notification does not carry the tag,
		// so the releases page is the honest landing.
		return base + "/releases"
	case "commits":
		return base + "/commit/" + id
	case "discussions":
		return base + "/discussions/" + id
	}
	return repoURL
}
