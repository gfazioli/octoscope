package github

// Events — the "what happened" half of Activity (#71). The heatmap
// answers *how much*; this answers *what*, as a timeline of the account's
// last N public/private events.
//
// **This is REST, and it is the only list in the dashboard that is.**
// There is no GraphQL equivalent — the v4 schema exposes contribution
// *counts*, not the event stream — so the endpoint is
// `/users/{login}/events`, and everything below is shaped by what that
// endpoint actually returns rather than by what its documentation says.
// Three measured facts drove the design (all against the live API on
// 2026-08-08):
//
//  1. **`payload.pull_request` is truncated to `{base, head, id, number,
//     url}`.** No `title`, no `html_url`, no `merged`. GitHub's reference
//     still documents the full pull-request object; the feed does not send
//     it. So a PR line can name the number and not the title, and its HTML
//     URL has to be *derived*. `payload.issue`, by contrast, arrives whole
//     — title included. That asymmetry is GitHub's, and the UI states it
//     rather than papering over it with a fetched-later title.
//
//  2. **`PushEvent.payload` carries no `commits` and no `size`** — only
//     `before`, `head`, `push_id`, `ref`, `repository_id`. Again, still
//     documented, not sent. A push row therefore names the branch and
//     links to the compare view; it cannot show a commit message, and it
//     must not pretend to know how many commits there were.
//
//  3. **Pagination hard-stops at 3 pages.** `page=4` is an HTTP 422, not an
//     empty list. Irrelevant to us — one page is what we ask for — but it
//     is the reason this file never grows a paging loop.
//
// The window is short and that is inherent: GitHub keeps roughly 90 days of
// events, and one page of 100 covered **barely two days** on the account
// this was measured against. The UI shows the span it actually got rather
// than implying it is a history.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// eventsPageSize bounds one fetch. Deliberately a single page, for the
// same reason gists are: paging is unbounded work, and GitHub caps this
// resource at three pages anyway.
const eventsPageSize = 100

// EventsPageSize is the cap the UI discloses. Exported so the feed header
// can say "showing the last 100" without hard-coding a second copy of the
// number that would drift the moment this one changed.
const EventsPageSize = eventsPageSize

// Event is one entry in the account's activity feed.
//
// The fields are **raw facts, not a rendered sentence**. Phrasing lives in
// the UI, which is the same rule Gist.Description follows: a display string
// invented here would end up in machine-readable output as a value GitHub
// never returned. Type and Action are GitHub's own vocabulary, verbatim.
type Event struct {
	// ID is GitHub's event id — unique, and the stable key for a row.
	ID string

	// Type is the event type verbatim ("PushEvent", "IssuesEvent", ...).
	// Kept as a string rather than an enum because the set is open: GitHub
	// adds types, and an unknown one must still render as a dated line in
	// the right repo instead of vanishing.
	Type string

	// Repo is "owner/name" as the event reports it. Note this is the repo's
	// name *at event time* — a later rename is not reflected, and GitHub
	// redirects the URL, so the link still resolves.
	Repo string

	// CreatedAt is when GitHub recorded the event, in UTC.
	CreatedAt time.Time

	// IsPublic mirrors the event's `public` flag, which is false for
	// anything that happened in a private repository. It is the privacy
	// signal for screenshot mode — see visibleEvents in the ui package.
	IsPublic bool

	// Action is payload.action verbatim when the type has one ("opened",
	// "closed", "merged", "created", "published", "assigned"), otherwise
	// empty. Worth knowing: PullRequestEvent emits a literal "merged"
	// action in this feed, which the reference does not list — it
	// documents "closed" plus a merged flag. Both are handled.
	//
	// **One deliberate exception**: for PullRequestReviewEvent this holds
	// the *review state* ("approved", "changes_requested", "commented")
	// rather than the action. The action there was "created" on all 20
	// review events in the measured sample, and it is created-only by
	// construction — a field with one value carries nothing, while the
	// difference between an approval and a drive-by comment is the most
	// significant thing a review row can say.
	Action string

	// Ref is the short branch or tag name for Push / Create / Delete, and
	// the tag for a Release. RefType is "branch", "tag" or "repository"
	// for Create / Delete, and empty elsewhere — a release's ref is a tag
	// by definition, so the type carries no information there.
	Ref     string
	RefType string

	// Number is the issue or pull-request number the event is about, 0 when
	// the event has no numbered subject (a push, a star, a fork).
	Number int

	// IsPullRequest distinguishes the two things a number can name.
	// GitHub files pull-request comments under the *issue* shape, so an
	// IssueCommentEvent is about a PR as often as an issue and the type
	// alone cannot tell them apart — `payload.issue.pull_request` is what
	// separates them, and without it the UI would label half its comment
	// rows wrong.
	IsPullRequest bool

	// Title is the subject's title, and it arrives by two routes.
	//
	// An issue event carries its own (payload.issue is sent whole). A pull
	// request event does not — the truncated payload has no title field at
	// all, see the file comment — so it is **filled in from a sibling event
	// in the same page** whenever one names the same repo and number.
	// Comment events on a PR do carry the title, because GitHub files PR
	// comments under the issue shape, and on any account busy enough to
	// have PR events there is usually a comment event beside them.
	//
	// It is never backfilled with a *request*: one row is not worth a round
	// trip and a hundred is a rate-limit incident. Empty therefore means
	// "nothing in this page knew it", and the UI shows the bare number
	// rather than inventing anything.
	Title string

	// URL is where the event's subject lives on github.com. Derived when
	// the payload has no html_url of its own (pushes, PR events), taken
	// verbatim when it does (issues, comments, reviews, releases).
	URL string
}

// eventJSON is the wire shape. Only the fields actually read are declared;
// the payload is a union across event types, so every sub-object is a
// pointer and each extractor checks its own.
type eventJSON struct {
	ID        string    `json:"id"`
	Type      string    `json:"type"`
	Public    bool      `json:"public"`
	CreatedAt time.Time `json:"created_at"`
	Repo      struct {
		Name string `json:"name"`
	} `json:"repo"`
	Payload struct {
		Action  string `json:"action"`
		Ref     string `json:"ref"`
		RefType string `json:"ref_type"`
		Before  string `json:"before"`
		Head    string `json:"head"`
		Number  int    `json:"number"`

		PullRequest *struct {
			Number int `json:"number"`
		} `json:"pull_request"`

		Issue *struct {
			Number  int    `json:"number"`
			Title   string `json:"title"`
			HTMLURL string `json:"html_url"`
			// Present only when the "issue" is really a pull request.
			// A bare presence check — the object inside holds API URLs
			// this package has no use for.
			PullRequest *struct{} `json:"pull_request"`
		} `json:"issue"`

		Comment *struct {
			HTMLURL string `json:"html_url"`
		} `json:"comment"`

		Review *struct {
			State   string `json:"state"`
			HTMLURL string `json:"html_url"`
		} `json:"review"`

		Release *struct {
			TagName string `json:"tag_name"`
			HTMLURL string `json:"html_url"`
		} `json:"release"`

		Forkee *struct {
			FullName string `json:"full_name"`
			HTMLURL  string `json:"html_url"`
		} `json:"forkee"`
	} `json:"payload"`
}

// FetchEvents returns the account's most recent events, newest first.
//
// **Privacy is the endpoint, not a filter** — the same rule gists follow.
// Under --public-only this asks `/events/public`, so events in private
// repositories are never fetched at all rather than fetched and dropped.
// That distinction is the whole point of screenshot mode: measured on a
// real account, 46 of 100 events on the default endpoint were `public:
// false`, and the very first one named a private repository in a field the
// UI would otherwise have to remember not to draw.
//
// `login` is required — unlike every GraphQL query in this package there is
// no `viewer` form of this endpoint, only `/users/{login}/events`. The
// caller passes the login the dashboard resolved, which is why this is
// fetched when the feed is opened rather than alongside the profile that
// discovers it.
//
// Fetched on demand, once per open, deliberately: the response carries
// `X-Poll-Interval: 60`, and octoscope's refresh floor is 5s. Riding the
// dashboard refresh would let a user who set `--refresh 5s` poll this
// endpoint twelve times faster than GitHub asks — for a tab they may never
// open.
func (c *Client) FetchEvents(ctx context.Context, login string) ([]Event, error) {
	login = strings.TrimSpace(login)
	if login == "" {
		return nil, &FetchError{
			Reason: ReasonUnknown,
			Err:    errors.New("events need a login and none was resolved"),
		}
	}

	path := "/users/" + login + "/events"
	if c.publicOnly {
		path += "/public"
	}
	url := fmt.Sprintf("https://api.github.com%s?per_page=%d", path, eventsPageSize)

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
			Err:    fmt.Errorf("GitHub answered %d for the events feed", resp.StatusCode),
		}
	}

	var raw []eventJSON
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, &FetchError{Reason: ReasonUnknown, Err: err}
	}
	return extractEvents(raw), nil
}

// classifyStatus maps a REST status onto the reason vocabulary the footer
// already speaks. Kept separate from classifyErr, which reads error strings
// off the GraphQL transport and never has a status code to work with.
//
// **403 is the whole reason this takes headers.** GitHub overloads it for
// three unrelated conditions — the token lacks a scope, the hourly budget
// is spent, or a secondary throttle fired — and the advice for each is
// different enough that guessing sends people to the wrong fix. The headers
// separate them exactly, and unlike matching on the body's wording they are
// machine-readable and stable:
//
//   - `Retry-After` is only sent for a secondary throttle, and it says how
//     long to wait, so it is checked first.
//   - `X-RateLimit-Remaining: 0` on a 403 means the hourly budget is gone.
//     Measured live against this endpoint, which sends the full
//     X-RateLimit-* set on every response.
//   - Neither present on a 403 is a permission problem, which is the
//     likeliest one for a read-only feed.
//
// 401 stays separate from all of them: a rejected token is
// re-authenticate, not grant-a-scope.
func classifyStatus(code int, h http.Header) FetchErrorReason {
	switch code {
	case http.StatusUnauthorized:
		return ReasonAuth
	case http.StatusForbidden, http.StatusTooManyRequests:
		if h.Get("Retry-After") != "" {
			return ReasonRateLimitSecondary
		}
		if h.Get("X-RateLimit-Remaining") == "0" {
			return ReasonRateLimitPrimary
		}
		if code == http.StatusTooManyRequests {
			// 429 is never a permission problem, whatever the headers say.
			return ReasonRateLimitSecondary
		}
		return ReasonAuthScope
	case http.StatusNotFound:
		return ReasonNotFound
	}
	if code >= 500 {
		return ReasonServer
	}
	return ReasonUnknown
}

// extractEvents is the pure half, so the per-type mapping is testable
// against recorded payloads without a transport.
//
// Every string crosses Sanitize at this boundary, which is where the
// package cleans GitHub-sourced text. It is not theoretical here: an issue
// title is written by whoever opened the issue, in any repository the
// account touched, and it is about to be drawn into a terminal.
func extractEvents(raw []eventJSON) []Event {
	if len(raw) == 0 {
		return nil
	}
	out := make([]Event, 0, len(raw))
	for _, r := range raw {
		repo := Sanitize(r.Repo.Name)
		e := Event{
			ID:        Sanitize(r.ID),
			Type:      Sanitize(r.Type),
			Repo:      repo,
			CreatedAt: r.CreatedAt.UTC(),
			IsPublic:  r.Public,
			Action:    Sanitize(r.Payload.Action),
			RefType:   Sanitize(r.Payload.RefType),
			Ref:       shortRef(Sanitize(r.Payload.Ref)),
		}
		if r.Payload.Issue != nil {
			e.Number = r.Payload.Issue.Number
			e.Title = Sanitize(r.Payload.Issue.Title)
			e.IsPullRequest = r.Payload.Issue.PullRequest != nil
		}
		// A release's identity is its tag, and there is no other field on
		// Event that means "the thing this is about" for a type with no
		// number. Ref is that field for Push / Create / Delete already.
		if r.Payload.Release != nil && e.Ref == "" {
			e.Ref = Sanitize(r.Payload.Release.TagName)
		}
		// Reviews: prefer the state over the always-"created" action. See
		// the Action doc comment.
		if r.Type == "PullRequestReviewEvent" && r.Payload.Review != nil {
			if st := Sanitize(r.Payload.Review.State); st != "" {
				e.Action = st
			}
		}
		if r.Payload.PullRequest != nil && r.Payload.PullRequest.Number != 0 {
			e.Number = r.Payload.PullRequest.Number
			e.IsPullRequest = true
		}
		if e.Number == 0 && r.Payload.Number != 0 {
			e.Number = r.Payload.Number
		}
		e.URL = eventURL(r, repo, e.Number)
		out = append(out, e)
	}
	return backfillTitles(out)
}

// backfillTitles copies each subject's title onto the events about that
// subject that did not get one.
//
// This exists because of the truncation documented at the top of the file:
// a PullRequestEvent has no title, while an IssueCommentEvent on the very
// same pull request has one, because GitHub files PR comments under the
// issue shape. The page already contains the answer — it is just attached
// to a different row.
//
// The join key is repo + number, which is unambiguous: GitHub numbers
// issues and pull requests from one sequence per repository, so #123 is
// one object, never two. Nothing is invented and nothing is fetched — a
// row with no match in the page keeps its empty title.
func backfillTitles(events []Event) []Event {
	titles := make(map[string]string)
	for _, e := range events {
		if e.Title == "" || e.Number == 0 {
			continue
		}
		key := fmt.Sprintf("%s#%d", e.Repo, e.Number)
		if _, seen := titles[key]; !seen {
			titles[key] = e.Title
		}
	}
	if len(titles) == 0 {
		return events
	}
	for i := range events {
		if events[i].Title != "" || events[i].Number == 0 {
			continue
		}
		if t, ok := titles[fmt.Sprintf("%s#%d", events[i].Repo, events[i].Number)]; ok {
			events[i].Title = t
			// A sibling that knew the title also knew what kind of object
			// it was. Only a pull request reaches this branch: an issue
			// event already carried its own title and never needs the join.
			events[i].IsPullRequest = true
		}
	}
	return events
}

// shortRef strips the `refs/heads/` or `refs/tags/` prefix a PushEvent's
// ref carries. Create and Delete already send the short form, so this has
// to be a trim rather than a split: a branch legitimately named
// `refs/heads/x` would otherwise lose a segment.
func shortRef(ref string) string {
	for _, p := range []string{"refs/heads/", "refs/tags/"} {
		if strings.HasPrefix(ref, p) {
			return strings.TrimPrefix(ref, p)
		}
	}
	return ref
}

// repoHTMLURL builds the browser URL for an "owner/name" pair. The events
// API only ever sends the API URL (api.github.com/repos/...), which is not
// something to open in a browser.
func repoHTMLURL(repo string) string {
	if repo == "" {
		return ""
	}
	return "https://github.com/" + repo
}

// zeroSHA is what `before` holds when a push created the branch — there is
// no previous commit to compare against.
const zeroSHA = "0000000000000000000000000000000000000000"

// eventURL picks the most specific github.com URL the event's payload
// supports, preferring one GitHub sent over one we construct.
//
// Where a URL is constructed it is from GitHub's own identifiers (the repo
// slug, an integer number, a commit SHA) rather than from free text, which
// is what keeps the result a github.com URL. The UI gates every open on
// isSafeOpenURL regardless — this function is not the security boundary,
// it is the correctness one.
func eventURL(r eventJSON, repo string, number int) string {
	base := repoHTMLURL(repo)
	if base == "" {
		return ""
	}

	// Payload-supplied URLs first: these are exact, and cover comments,
	// reviews, issues, releases and forks.
	switch {
	case r.Payload.Comment != nil && r.Payload.Comment.HTMLURL != "":
		return Sanitize(r.Payload.Comment.HTMLURL)
	case r.Payload.Review != nil && r.Payload.Review.HTMLURL != "":
		return Sanitize(r.Payload.Review.HTMLURL)
	case r.Payload.Release != nil && r.Payload.Release.HTMLURL != "":
		return Sanitize(r.Payload.Release.HTMLURL)
	case r.Payload.Forkee != nil && r.Payload.Forkee.HTMLURL != "":
		return Sanitize(r.Payload.Forkee.HTMLURL)
	case r.Type == "IssuesEvent" && r.Payload.Issue != nil && r.Payload.Issue.HTMLURL != "":
		return Sanitize(r.Payload.Issue.HTMLURL)
	}

	switch r.Type {
	case "PushEvent":
		before, head := Sanitize(r.Payload.Before), Sanitize(r.Payload.Head)
		if head == "" {
			return base
		}
		// A push that created the branch has no `before` to compare
		// against, so link at the branch's commit list instead of building
		// a compare URL against the zero SHA (which 404s).
		if before == "" || before == zeroSHA {
			if ref := shortRef(Sanitize(r.Payload.Ref)); ref != "" {
				return base + "/commits/" + ref
			}
			return base + "/commit/" + head
		}
		return base + "/compare/" + before + "..." + head

	case "PullRequestEvent", "PullRequestReviewEvent", "PullRequestReviewCommentEvent":
		// Derived, because the truncated pull_request object has no
		// html_url — see the file comment.
		if number > 0 {
			return fmt.Sprintf("%s/pull/%d", base, number)
		}

	case "CreateEvent":
		ref := shortRef(Sanitize(r.Payload.Ref))
		switch r.Payload.RefType {
		case "branch":
			if ref != "" {
				return base + "/tree/" + ref
			}
		case "tag":
			if ref != "" {
				return base + "/releases/tag/" + ref
			}
		}

	case "DeleteEvent":
		// The ref is gone, so there is nothing specific left to link to.
		return base
	}

	return base
}
