// Package github is a thin wrapper around githubv4 that pulls only the
// fields octoscope needs and returns them as a Sendable, UI-friendly
// struct. Keeping the surface area narrow makes it trivial to swap the
// transport later if we ever outgrow the v4 GraphQL client.
package github

import (
	"context"
	"errors"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/gfazioli/octoscope/internal/auth"
	"github.com/shurcooL/githubv4"
	"golang.org/x/oauth2"
)

// Client wraps a githubv4.Client. Safe to share across goroutines.
//
// `login` selects whose account we're rendering: empty string means
// "the authenticated viewer" (the token owner); any other value means
// a specific public user, queried via `user(login: $login)`. That path
// also works unauthenticated, subject to GitHub's 60 req/h limit.
//
// publicOnly, when true, drops repos / PRs / issues whose repository
// is private from the per-list surfaces (Repos, PRs, Issues tabs and
// the derived Overview aggregates: TotalStars, ForksReceived,
// OpenIssues, OpenPRs, Languages). Global counters that GitHub only
// exposes as a single totalCount (PRsTotal, PRsMerged, IssuesAuthored)
// stay complete — filtering them would need a second search query and
// they're just numbers, not leaking titles or repo names.
type Client struct {
	gql           *githubv4.Client
	rest          *http.Client // shares the oauth2 transport with gql; never nil — falls back to http.DefaultClient for unauthenticated sessions
	authenticated bool
	tokenSource   auth.Source // where the token came from — drives auth-error hints, never holds the token
	login         string
	publicOnly    bool

	// watchRepos is the live list of external "owner/name"
	// identifiers the next FetchStats will resolve into
	// Stats.WatchedRepos (v0.14.0). Driven by the user's
	// `watch_repos` config key + setter so a settings-panel
	// edit (when we add one) can refresh the set without
	// rebuilding the Client.
	watchReposMu sync.RWMutex
	watchRepos   []string

	// viewerID is the GraphQL node ID of the authenticated viewer,
	// cached lazily (see ensureViewerID). Used as the $authorFilter
	// variable in FetchRepoDetail's history query so the
	// "commits by you in the last year" count resolves server-side
	// without requiring the viewer's email or login.
	//
	// `githubv4.ID` is an interface{}, so its zero value is nil
	// (not the empty string) — we can't use `viewerID != ""` to
	// detect "fetched yet?". `viewerIDFetched` carries that bit
	// explicitly so the guard in ensureViewerID and the
	// hasAuthor flag in FetchRepoDetail stay correct regardless of
	// what concrete type the GraphQL client decoded into the
	// interface.
	//
	// All three fields are guarded by viewerIDMu because the Client
	// is documented as safe to share across goroutines and BubbleTea
	// fires fetch commands asynchronously. Two overlapping detail
	// fetches (e.g. opening a detail and pressing `r` before the
	// first response lands) would otherwise both enter the lazy
	// fetch path and race on the writes.
	viewerIDMu      sync.Mutex
	viewerID        githubv4.ID
	viewerIDFetched bool
}

// ensureViewerID populates the lazy viewer-ID cache and returns
// `(id, fetched, err)`. On first call (when authenticated) it runs
// a `viewer { id }` query; afterwards it serves from the cache.
// Returns the empty interface + fetched=false when the client is
// unauthenticated.
//
// Returning the values directly (rather than letting callers read
// c.viewerID after the call) keeps the read-modify-write inside
// the mutex — without that, a concurrent caller could observe
// fetched==true before viewerID's write was visible (memory
// ordering), or two overlapping fetches could both run the
// network round-trip. BubbleTea fires fetch commands
// asynchronously, so this matters in practice the moment the user
// hits `r` while a detail fetch is still in flight.
//
// Lives on the Client (not in detail.go) because the ID is a
// session-scoped fact about the auth context, not a detail-only
// concern. Future features that need viewer-author filters (e.g. a
// per-PR review-context tooltip) can reuse the same cache.
func (c *Client) ensureViewerID(ctx context.Context) (githubv4.ID, bool, error) {
	c.viewerIDMu.Lock()
	defer c.viewerIDMu.Unlock()
	if c.viewerIDFetched || !c.authenticated {
		return c.viewerID, c.viewerIDFetched, nil
	}
	var q struct {
		Viewer struct {
			ID githubv4.ID
		}
	}
	if err := c.gql.Query(ctx, &q, nil); err != nil {
		return c.viewerID, c.viewerIDFetched, err
	}
	c.viewerID = q.Viewer.ID
	c.viewerIDFetched = true
	return c.viewerID, c.viewerIDFetched, nil
}

// Options tweaks Client construction. Zero-valued Options (the
// default) preserves the pre-flag behaviour.
type Options struct {
	PublicOnly bool
}

// SocialAccount is one of the verified social links on the profile
// (X/Twitter, LinkedIn, Bluesky, Mastodon, …). GitHub normalises the
// provider string to upper-snake-case.
type SocialAccount struct {
	Provider    string
	URL         string
	DisplayName string
}

// Language is a programming language with its aggregated byte count
// across the user's non-fork repositories. Color is the hex string
// GitHub assigns to the language — reused as the bar colour in the
// TUI so our colour palette matches github.com.
type Language struct {
	Name  string
	Color string
	Bytes int
}

// Organization is one of the orgs the viewer is a member of.
type Organization struct {
	Login string
	Name  string
}

// ContributionDay is one cell of the 52-week contribution calendar.
// `Weekday` is 0 (Sunday) .. 6 (Saturday) matching GitHub's schema.
type ContributionDay struct {
	Date    time.Time
	Count   int
	Weekday int
}

// Repo is the per-repository snapshot surfaced in the Repos tab.
// Populated by the same FetchStats round-trip that feeds Overview,
// so list rendering is immediate after the first refresh.
type Repo struct {
	Name            string
	URL             string // https://github.com/owner/name
	PrimaryLanguage string
	LanguageColor   string
	Stars           int
	Forks           int
	OpenIssues      int
	OpenPRs         int
	PushedAt        time.Time
	IsPrivate       bool

	// CIState is the status-check rollup state of the latest
	// commit on the default branch, sourced from
	// `defaultBranchRef.target.statusCheckRollup.state`. Empty
	// string means "no rollup" (no default branch, no workflow
	// run on the head commit, fork that never ran CI). The UI
	// renders it as a coloured dot in the Repos tab. Enum
	// values from GitHub: SUCCESS, FAILURE, ERROR, PENDING,
	// EXPECTED.
	CIState string

	// LatestReleaseTag and LatestReleasePublishedAt are the
	// most recent GitHub Release on the repo, populated via
	// the repoCIFields' second parallel query in v0.14.0. Tag
	// is empty (and PublishedAt zero) when the repo has no
	// releases — common for libraries that ship via tags only,
	// or for repos that simply haven't cut anything yet.
	LatestReleaseTag         string
	LatestReleasePublishedAt time.Time
}

// PullRequest is one open PR authored by the user, feeding the PRs
// tab. Scope note: "authored by" is user-global — it includes PRs
// opened against repos the user doesn't own. The Overview tab's
// "Open PRs" card is a different number (only PRs opened on the
// user's own repos, aggregated per-repo); the two counters can diverge.
type PullRequest struct {
	Number    int
	Title     string
	URL       string
	Repo      string // owner/name
	IsDraft   bool
	Mergeable string // MERGEABLE | CONFLICTING | UNKNOWN
	UpdatedAt time.Time
	IsPrivate bool

	// AuthorLogin is empty for the user's authored PRs (Stats
	// .OpenPullRequests) since the author is the viewer by
	// definition. It's populated for the review-requests inbox
	// (Stats.ReviewRequests) where the PR was opened by someone
	// else and the UI wants to show "who's asking" alongside the
	// title.
	AuthorLogin string
}

// Issue is one open issue authored by the user, feeding the Issues
// tab. Same "authored-by" scope semantics as PullRequest.
type Issue struct {
	Number    int
	Title     string
	URL       string
	Repo      string // owner/name
	UpdatedAt time.Time
	IsPrivate bool
}

// RateLimit captures the GraphQL rate-limit envelope that GitHub
// returns alongside every query. Cost is the points the last query
// billed; Remaining/Limit bound how many points are left in the
// current hour window; ResetAt is when the Limit bucket refills.
// Zero-cost to request (the rateLimit field itself doesn't count).
type RateLimit struct {
	Cost      int
	Limit     int
	Remaining int
	ResetAt   time.Time
}

// FetchErrorReason classifies why a FetchStats call failed so the UI
// can render an actionable message instead of a generic "refresh
// errored" line. ReasonUnknown is the fallback for errors we don't
// recognise (treated as a generic server error).
type FetchErrorReason int

const (
	ReasonUnknown            FetchErrorReason = iota
	ReasonRateLimitPrimary                    // 5000/h GraphQL budget exhausted
	ReasonRateLimitSecondary                  // short-term abuse throttle
	ReasonAuth                                // 401: token rejected — expired, revoked, or invalid
	ReasonAuthScope                           // token authenticated but lacks a scope / permission for the data
	ReasonNotFound                            // queried resource doesn't exist for this token (deleted / renamed / invisible)
	ReasonNetwork                             // DNS, TCP, TLS, context timeout
	ReasonServer                              // 5xx, HTTP/2 stream error (RST_STREAM/GOAWAY), or GraphQL-level error
)

// FetchError wraps the original error with a classified reason. Kept
// small on purpose — the UI layer only needs Reason + Err to pick a
// message, not the full HTTP response.
type FetchError struct {
	Reason FetchErrorReason
	Err    error
}

func (e *FetchError) Error() string {
	if e.Err == nil {
		return "unknown error"
	}
	return e.Err.Error()
}

func (e *FetchError) Unwrap() error {
	return e.Err
}

// Stats is the snapshot consumed by the TUI. All fields are populated
// by a single GraphQL query; missing/unset fields are zero-valued.
type Stats struct {
	// Profile
	Login           string
	Name            string
	Bio             string
	Company         string
	Location        string
	Pronouns        string
	WebsiteURL      string
	TwitterUsername string
	AvatarURL       string
	CreatedAt       time.Time
	SocialAccounts  []SocialAccount

	// Social
	Followers  int
	Following  int
	TotalStars int
	// TotalStarsWithForks is TotalStars plus the stargazer count on
	// repositories the user owns *as forks*. Surfaced as a secondary
	// number on the Social card so the dashboard reconciles with
	// counters (github-readme-stats, etc.) that include fork stars.
	// Equal to TotalStars when there are no forked repos to count or
	// when public-only mode is on (the breakdown isn't meaningful
	// once we drop private context).
	TotalStarsWithForks int

	// Activity (lifetime counts unless noted)
	PRsTotal       int
	PRsMerged      int
	IssuesAuthored int
	// OpenPRsAuthored is the count of currently-open pull requests
	// the user authored, anywhere on GitHub (including repos they
	// don't own). Distinct from Stats.OpenPRs in the Operational
	// section, which counts PRs opened *against* the user's repos
	// from anyone.
	OpenPRsAuthored          int
	CommitsLastYear          int
	ContributedReposLastYear int
	Languages                []Language

	// ContributionWeeks is the last ~52 weeks of daily contribution
	// counts, grouped by week. weeks[0] is the oldest. Each inner slice
	// is ordered by weekday (0=Sun..6=Sat) and may be shorter than 7
	// when the window doesn't line up with week boundaries. Empty when
	// the user has no public contribution data.
	ContributionWeeks [][]ContributionDay

	// Operational (current-state counts across owned non-fork repos)
	PublicRepos   int
	ForksReceived int
	OpenIssues    int
	OpenPRs       int

	// Repositories is the per-repo breakdown feeding the Repos tab —
	// one entry per owned, non-fork repository up to the 100-repo
	// GraphQL page limit shared with the Operational aggregates.
	Repositories []Repo

	// OpenPullRequests is the list of currently-open PRs the user
	// authored, sorted newest-update first, capped at 50 entries
	// server-side. Feeds the PRs tab.
	OpenPullRequests []PullRequest

	// OpenIssuesList is the list of currently-open issues the user
	// authored, sorted newest-update first, capped at 50 entries
	// server-side. Feeds the Issues tab.
	OpenIssuesList []Issue

	// Network
	Organizations []Organization

	// WatchedRepos are external "owner/name" repositories the user
	// listed in `watch_repos`. Same Repo shape as the owned set
	// (so the Repos tab can render them with CI dot, latest
	// release, language colour, etc.) but kept separate so they
	// can render in their own section and never get folded into
	// the owned aggregates (TotalStars, ForksReceived, …).
	WatchedRepos []Repo

	// WatchedSkipped lists the `watch_repos` config refs that no
	// longer resolve (renamed, deleted, gone private, or malformed)
	// — the Repos tab surfaces them so a stale entry doesn't vanish
	// silently (#37). Input (config) order. Transient fetch failures
	// are NOT counted: only NOT_FOUND marks an entry as skipped.
	WatchedSkipped []string

	// ReviewRequests are open pull requests where the viewer has
	// been asked to review (v0.15.0+). Populated only when the
	// client is authenticated AND running in viewer mode (no
	// explicit login arg) — the GitHub search query uses
	// `review-requested:@me` which is inherently personal. Empty
	// otherwise. Surfaced by the PRs tab as a sticky section
	// above "Your authored PRs"; not folded into the existing
	// OpenPullRequests list because the two answer different
	// questions ("things I created" vs "things waiting for me").
	ReviewRequests []PullRequest

	// Meta
	Authenticated bool
	// IsViewer is true when the stats belong to the authenticated
	// token owner (the classic case). False when the user passed an
	// explicit login on the command line — the UI can use this to
	// dim the "authenticated" badge so it doesn't misrepresent
	// "we have a token" as "this is your account".
	IsViewer bool

	// RateLimit is the GraphQL budget snapshot as of this fetch. Nil
	// when we haven't observed one yet (e.g. unauthenticated callers
	// against a different rate-limit endpoint). The footer reads it
	// to surface "rate N/limit · reset Xm" when available.
	RateLimit *RateLimit
}

// Public returns a copy of s with private repositories, PRs, issues,
// watched repos and review requests stripped from the lists. Aggregate
// counters that depend on per-repo data (TotalStars, ForksReceived,
// OpenIssues, OpenPRs, PublicRepos) are recomputed from the kept repos
// so the Overview cards stay consistent with what the lists show.
// Languages is deliberately passed through unchanged — it's a
// profile-level metric and per-repo language byte counts aren't stored
// on Repo, so it can't be split by visibility here (see the inline
// comment in the repo loop below).
//
// Every list-bearing field that can carry a private item MUST be
// filtered here — the whole point of public-only/screenshot mode is
// that nothing private reaches the screen. When a new private-aware
// list is added to Stats, add its skip loop below (and a case to
// TestPublicStripsEveryPrivateList).
//
// Top-level viewer counters that don't depend on per-repo data
// (Followers, Following, PRsTotal, PRsMerged, IssuesAuthored,
// CommitsLastYear, ContributionWeeks, ...) are passed through
// unchanged because they're aggregate numbers — there's no per-item
// title or repo to leak.
//
// This runs at render time, so toggling public-only mode in the
// in-app settings panel reflects instantly without a refetch.
func (s *Stats) Public() *Stats {
	if s == nil {
		return nil
	}
	out := *s

	out.Repositories = nil
	out.TotalStars = 0
	out.ForksReceived = 0
	out.OpenIssues = 0
	out.OpenPRs = 0
	// The "with forks" total is computed off a separate fork-only
	// query and we don't track per-fork privacy here; collapse it to
	// TotalStars in public-only mode so the secondary line vanishes
	// rather than misrepresenting a partial number.
	out.TotalStarsWithForks = 0
	for _, r := range s.Repositories {
		if r.IsPrivate {
			continue
		}
		out.Repositories = append(out.Repositories, r)
		out.TotalStars += r.Stars
		out.ForksReceived += r.Forks
		out.OpenIssues += r.OpenIssues
		out.OpenPRs += r.OpenPRs
		// Per-repo Languages aren't stored on Repo (only the primary
		// language is), so language byte counts can't be split here.
		// We keep s.Languages unchanged — the bar reflects the
		// aggregate across all owned repos and is a profile-level
		// metric, not a per-item leak.
	}
	out.PublicRepos = len(out.Repositories)

	out.TotalStarsWithForks = out.TotalStars

	out.OpenPullRequests = nil
	for _, pr := range s.OpenPullRequests {
		if pr.IsPrivate {
			continue
		}
		out.OpenPullRequests = append(out.OpenPullRequests, pr)
	}
	// Mirror the count to the visible subset so the Activity card
	// matches what the PRs tab actually shows. Edge case: users with
	// >50 open authored PRs will under-count (server caps the list at
	// 50), but the count tracks what's on screen — same trade-off as
	// the lists themselves.
	out.OpenPRsAuthored = len(out.OpenPullRequests)

	out.OpenIssuesList = nil
	for _, is := range s.OpenIssuesList {
		if is.IsPrivate {
			continue
		}
		out.OpenIssuesList = append(out.OpenIssuesList, is)
	}

	// Watched repos (v0.14.0) carry the same Repo shape as the owned
	// set, so a watched *private* repo would leak its name / CI /
	// release in screenshot mode. Strip them. They're intentionally
	// kept out of the owned aggregates, so nothing to recompute.
	out.WatchedRepos = nil
	for _, r := range s.WatchedRepos {
		if r.IsPrivate {
			continue
		}
		out.WatchedRepos = append(out.WatchedRepos, r)
	}

	// Review requests (v0.15.0) are PRs awaiting the viewer's review;
	// a private-repo PR would leak its title / repo / author. Strip
	// them too.
	out.ReviewRequests = nil
	for _, pr := range s.ReviewRequests {
		if pr.IsPrivate {
			continue
		}
		out.ReviewRequests = append(out.ReviewRequests, pr)
	}

	// WatchedSkipped names watch_repos refs that failed to resolve —
	// one may reference a repo that just went private, so screenshot
	// mode drops the notice entirely rather than leak the name.
	out.WatchedSkipped = nil

	return &out
}

// New builds a client, preferring an authenticated one when a token is
// available. An unauthenticated client still works but is rate-limited
// to 60 requests/hour by GitHub.
//
// `login` is the optional GitHub username to fetch. Pass "" for the
// authenticated viewer; pass any login to show that user's public
// profile (works with or without a token, though without one the
// dashboard will burn through the 60/h limit quickly).
func New(login string, opts Options) (*Client, error) {
	token, tokenSrc := auth.TokenSource()
	var httpClient *http.Client
	authed := false
	if token != "" {
		src := oauth2.StaticTokenSource(&oauth2.Token{AccessToken: token})
		httpClient = oauth2.NewClient(context.Background(), src)
		authed = true
	}
	if login == "" && !authed {
		return nil, errors.New(
			"no GitHub token and no username specified.\n" +
				"Either export GITHUB_TOKEN, run 'gh auth login', " +
				"or pass a username: octoscope <username>",
		)
	}
	rest := httpClient
	if rest == nil {
		rest = http.DefaultClient
	}
	return &Client{
		gql:           githubv4.NewClient(httpClient),
		rest:          rest,
		authenticated: authed,
		tokenSource:   tokenSrc,
		login:         login,
		publicOnly:    opts.PublicOnly,
	}, nil
}

// SetPublicOnly toggles the publicOnly filter at runtime. The change
// takes effect on the next FetchStats call — already-cached Stats in
// the TUI keep showing whatever filter was active when they were
// fetched, so callers should usually pair this with a forced refetch.
func (c *Client) SetPublicOnly(v bool) {
	c.publicOnly = v
}

// PublicOnly reports whether the client is currently filtering out
// private items. Useful for the in-app settings panel which needs to
// reflect the live state, not the launch-time value.
func (c *Client) PublicOnly() bool {
	return c.publicOnly
}

// TokenSource reports where the client's token came from (env var,
// gh CLI, or none) so auth-error surfaces can point at the matching
// fix. Nil-safe because footer render paths consult it on Models that
// unit tests build without a client.
func (c *Client) TokenSource() auth.Source {
	if c == nil {
		return auth.SourceNone
	}
	return c.tokenSource
}

// SetWatchRepos replaces the list of external "owner/name"
// identifiers the next FetchStats will resolve into
// Stats.WatchedRepos. Takes a copy so callers can mutate their
// slice without disturbing the client. RWMutex-guarded because
// the BubbleTea runtime may interleave a SetWatchRepos and a
// FetchStats in flight.
func (c *Client) SetWatchRepos(refs []string) {
	c.watchReposMu.Lock()
	defer c.watchReposMu.Unlock()
	c.watchRepos = append([]string(nil), refs...)
}

// WatchRepos returns the current list of watched repos —
// snapshot copy so the caller can iterate without holding the
// lock.
func (c *Client) WatchRepos() []string {
	c.watchReposMu.RLock()
	defer c.watchReposMu.RUnlock()
	return append([]string(nil), c.watchRepos...)
}

// profileFields is the "everything-except-the-repo-list" half of
// the dashboard fetch: profile metadata, viewer-level counters
// (Followers, Following, PRs, Issues, ...), the open-PRs and
// open-issues nodes lists, the 52-week contribution calendar, the
// organizations the viewer is a member of, and a tiny ForkedRepos
// stub (just stargazerCount per fork — leaves the per-repo bulk
// out of this query).
//
// Why split?  Through v0.10.0 we packed every field into one
// userFields struct and ran a single query. As the user's GitHub
// footprint grows, that query started hitting GitHub's gateway
// complexity ceiling and getting 502'd before it ever reached the
// backend. Pulling the heavy "100 owned repos with full metadata"
// out into its own query (repoFields) keeps each individual call
// well under the budget; FetchStats then runs the two in parallel
// so total wall-clock latency stays close to the slower of the two
// rather than their sum.
type profileFields struct {
	Login           githubv4.String
	Name            githubv4.String
	Bio             githubv4.String
	Company         githubv4.String
	Location        githubv4.String
	Pronouns        githubv4.String
	WebsiteURL      githubv4.String `graphql:"websiteUrl"`
	TwitterUsername githubv4.String
	AvatarURL       githubv4.String `graphql:"avatarUrl(size: 64)"`
	CreatedAt       githubv4.DateTime

	SocialAccounts struct {
		Nodes []struct {
			Provider    githubv4.String
			URL         githubv4.String `graphql:"url"`
			DisplayName githubv4.String
		}
	} `graphql:"socialAccounts(first: 10)"`

	Followers struct {
		TotalCount githubv4.Int
	}
	Following struct {
		TotalCount githubv4.Int
	}

	PullRequests struct {
		TotalCount githubv4.Int
	} `graphql:"pullRequests"`

	MergedPRs struct {
		TotalCount githubv4.Int
	} `graphql:"mergedPRs: pullRequests(states: MERGED)"`

	Issues struct {
		TotalCount githubv4.Int
	} `graphql:"issues"`

	OpenPRs struct {
		TotalCount githubv4.Int
		Nodes      []struct {
			Number     githubv4.Int
			Title      githubv4.String
			URL        githubv4.String `graphql:"url"`
			IsDraft    githubv4.Boolean
			Mergeable  githubv4.MergeableState
			UpdatedAt  githubv4.DateTime
			Repository struct {
				NameWithOwner githubv4.String
				IsPrivate     githubv4.Boolean
			}
		}
	} `graphql:"openPRs: pullRequests(states: OPEN, first: 50, orderBy: {field: UPDATED_AT, direction: DESC})"`

	OpenIssuesList struct {
		Nodes []struct {
			Number     githubv4.Int
			Title      githubv4.String
			URL        githubv4.String `graphql:"url"`
			UpdatedAt  githubv4.DateTime
			Repository struct {
				NameWithOwner githubv4.String
				IsPrivate     githubv4.Boolean
			}
		}
	} `graphql:"openIssuesList: issues(states: OPEN, first: 50, orderBy: {field: UPDATED_AT, direction: DESC})"`

	ContributionsCollection struct {
		TotalCommitContributions                githubv4.Int
		TotalRepositoriesWithContributedCommits githubv4.Int
		ContributionCalendar                    struct {
			TotalContributions githubv4.Int
			Weeks              []struct {
				ContributionDays []struct {
					Date              githubv4.String
					ContributionCount githubv4.Int
					Weekday           githubv4.Int
				}
			}
		}
	}

	Organizations struct {
		Nodes []struct {
			Login githubv4.String
			Name  githubv4.String
		}
	} `graphql:"organizations(first: 20)"`

	// ForkedRepos pulls only the stargazerCount of repositories the
	// user owns *as forks*. Used to compute TotalStarsWithForks; we
	// don't need any other field on these so the payload stays small
	// — small enough to comfortably ride along in profileFields
	// rather than earning its own query.
	ForkedRepos struct {
		Nodes []struct {
			StargazerCount githubv4.Int
		}
	} `graphql:"forkedRepos: repositories(first: 100, ownerAffiliations: OWNER, isFork: true)"`
}

// repoFields is the second half of the split: the heavy
// `repositories(first: 100)` payload with per-repo languages,
// open-issue / open-PR counters, etc. Lives in its own query so
// the combined complexity stays under GitHub's gateway threshold
// (the moment when 100 repos × the full nested field set started
// returning 502 was the trigger for splitting at all).
type repoFields struct {
	Repositories struct {
		TotalCount githubv4.Int
		Nodes      []struct {
			Name githubv4.String
			// NameWithOwner is the merge key against repoCIFields
			// in extractStats — bare Name isn't unique when
			// ownerAffiliations expands across personal + orgs
			// (an org may legitimately own a repo called the
			// same as a personal one). NameWithOwner is the
			// canonical "owner/name" string GitHub guarantees.
			NameWithOwner   githubv4.String
			URL             githubv4.String `graphql:"url"`
			IsPrivate       githubv4.Boolean
			PushedAt        githubv4.DateTime
			PrimaryLanguage struct {
				Name  githubv4.String
				Color githubv4.String
			}
			StargazerCount githubv4.Int
			ForkCount      githubv4.Int
			Issues         struct {
				TotalCount githubv4.Int
			} `graphql:"issues(states: OPEN)"`
			PullRequests struct {
				TotalCount githubv4.Int
			} `graphql:"pullRequests(states: OPEN)"`
			Languages struct {
				Edges []struct {
					Size githubv4.Int
					Node struct {
						Name  githubv4.String
						Color githubv4.String
					}
				}
			} `graphql:"languages(first: 10, orderBy: {field: SIZE, direction: DESC})"`
		}
		PageInfo struct {
			HasNextPage githubv4.Boolean
			EndCursor   githubv4.String
		}
	} `graphql:"repositories(first: 100, after: $reposCursor, ownerAffiliations: OWNER, isFork: false)"`
}

// repoCIFields is the third parallel query introduced in v0.13.0
// to power the Repos-tab CI status column. Kept separate from
// repoFields because pulling statusCheckRollup inline on the main
// repository nodes blew GitHub's gateway complexity ceiling and
// 502'd on busy accounts — exactly the same failure mode that
// drove the v0.10.1 split. Each node carries only the bare
// minimum (nameWithOwner + rollup state + latest release
// header) so this query stays cheap; the merge happens by
// NameWithOwner in extractStats so org-level repos don't
// collide with personal repos that share a bare name.
//
// v0.14.0 piggybacks `releases(first: 1)` onto this query
// instead of opening a fourth parallel goroutine — the extra
// connection access is small relative to splitting cost, and
// keeps mergeRateLimit3 / extractStats signatures unchanged.
type repoCIFields struct {
	Repositories struct {
		Nodes []struct {
			NameWithOwner    githubv4.String
			DefaultBranchRef struct {
				Target struct {
					Commit struct {
						StatusCheckRollup struct {
							State githubv4.StatusState
						}
					} `graphql:"... on Commit"`
				}
			}
			Releases struct {
				Nodes []struct {
					TagName     githubv4.String
					PublishedAt githubv4.DateTime
				}
			} `graphql:"releases(first: 1, orderBy: {field: CREATED_AT, direction: DESC})"`
		}
		PageInfo struct {
			HasNextPage githubv4.Boolean
			EndCursor   githubv4.String
		}
	} `graphql:"repositories(first: 100, after: $ciCursor, ownerAffiliations: OWNER, isFork: false)"`
}

// maxRepoPages bounds repositories pagination. Pages are sequential
// (each needs the previous page's endCursor), so the page count
// multiplies the dashboard-fetch wall-clock against the 30s timeout in
// model.go. 5 × 100 = 500 repos fits comfortably and covers the vast
// majority of accounts; beyond 500 the aggregates are still far closer
// to the truth than the old hard 100 cap — a pathological account just
// isn't walked to the very end, which beats timing the whole fetch out.
// Mirrors the bounded-fan-out discipline used for watched repos and
// star history.
const maxRepoPages = 5

// fetchRepoFieldsPaged walks repositories(first: 100) with cursor
// pagination and accumulates every page's nodes into a single
// repoFields, so extractStats aggregates stars / forks / open
// issues+PRs / languages across the user's entire repo set rather than
// just the first 100 — the pre-0.19.0 cap silently under-counted
// accounts with >100 repos. TotalCount comes from the first page; the
// returned envelope sums Cost across pages and keeps the last (most
// drained) page's remaining/limit/resetAt. Bounded by maxRepoPages.
func (c *Client) fetchRepoFieldsPaged(ctx context.Context) (repoFields, rateLimitFields, error) {
	var acc repoFields
	var lastRL rateLimitFields
	var cursor *githubv4.String
	totalCost := 0

	for page := 0; page < maxRepoPages; page++ {
		var (
			rf  repoFields
			rl  rateLimitFields
			err error
		)
		if c.login == "" {
			var q struct {
				Viewer    repoFields
				RateLimit rateLimitFields
			}
			err = c.gql.Query(ctx, &q, map[string]interface{}{"reposCursor": cursor})
			rf, rl = q.Viewer, q.RateLimit
		} else {
			var q struct {
				User      repoFields `graphql:"user(login: $login)"`
				RateLimit rateLimitFields
			}
			err = c.gql.Query(ctx, &q, map[string]interface{}{
				"login":       githubv4.String(c.login),
				"reposCursor": cursor,
			})
			rf, rl = q.User, q.RateLimit
		}
		if err != nil {
			return repoFields{}, rateLimitFields{}, err
		}

		if page == 0 {
			acc.Repositories.TotalCount = rf.Repositories.TotalCount
		}
		acc.Repositories.Nodes = append(acc.Repositories.Nodes, rf.Repositories.Nodes...)
		lastRL = rl
		totalCost += int(rl.Cost)

		if !bool(rf.Repositories.PageInfo.HasNextPage) {
			break
		}
		next := rf.Repositories.PageInfo.EndCursor
		cursor = &next
	}

	lastRL.Cost = githubv4.Int(totalCost)
	return acc, lastRL, nil
}

// fetchRepoCIFieldsPaged is the repoCIFields counterpart of
// fetchRepoFieldsPaged — same cursor-pagination accumulation so the CI
// rollup + latest-release maps in extractStats cover every repo, not
// just the first 100. The merge with repoFields happens by
// NameWithOwner, so the two queries needn't paginate in lockstep.
func (c *Client) fetchRepoCIFieldsPaged(ctx context.Context) (repoCIFields, rateLimitFields, error) {
	var acc repoCIFields
	var lastRL rateLimitFields
	var cursor *githubv4.String
	totalCost := 0

	for page := 0; page < maxRepoPages; page++ {
		var (
			cf  repoCIFields
			rl  rateLimitFields
			err error
		)
		if c.login == "" {
			var q struct {
				Viewer    repoCIFields
				RateLimit rateLimitFields
			}
			err = c.gql.Query(ctx, &q, map[string]interface{}{"ciCursor": cursor})
			cf, rl = q.Viewer, q.RateLimit
		} else {
			var q struct {
				User      repoCIFields `graphql:"user(login: $login)"`
				RateLimit rateLimitFields
			}
			err = c.gql.Query(ctx, &q, map[string]interface{}{
				"login":    githubv4.String(c.login),
				"ciCursor": cursor,
			})
			cf, rl = q.User, q.RateLimit
		}
		if err != nil {
			return repoCIFields{}, rateLimitFields{}, err
		}

		acc.Repositories.Nodes = append(acc.Repositories.Nodes, cf.Repositories.Nodes...)
		lastRL = rl
		totalCost += int(rl.Cost)

		if !bool(cf.Repositories.PageInfo.HasNextPage) {
			break
		}
		next := cf.Repositories.PageInfo.EndCursor
		cursor = &next
	}

	lastRL.Cost = githubv4.Int(totalCost)
	return acc, lastRL, nil
}

// FetchStats runs the dashboard fetch as several parallel branches —
// profileFields, repoFields and repoCIFields always, plus the
// watched-repos fan-out and the review-requests search when
// applicable — and combines them into the UI-facing Stats. Splitting
// was forced by GitHub's gateway 502'ing the original single-query
// approach once an account grew busy enough; each branch's complexity
// now sits well under the threshold and total wall-clock latency stays
// close to the slowest branch rather than their sum.
//
// Routes against `viewer` when Client.login is empty, otherwise
// against `user(login: $login)`. An error from any required branch
// fails the whole fetch — partial Stats would be confusing in the UI
// (e.g. profile loaded but Repos tab empty).
//
// The reported RateLimit is whichever branch has the smaller
// `remaining` (most pessimistic estimate), with costs summed. The
// profile branch is ~1 point; the repo branches now cost one point
// per page walked (see fetchRepoFieldsPaged), so the chip tracks the
// real budget drawn even on large accounts.
//
// Repository data is paginated to completion (bounded by
// maxRepoPages) so aggregated totals — stars, forks, open
// issues+PRs, language bytes — cover the account's entire repo set,
// not just the first 100. The only residual cap is fork stargazers
// for TotalStarsWithForks (profileFields.ForkedRepos stays
// first: 100; >100 starred forks is vanishingly rare).
func (c *Client) FetchStats(ctx context.Context) (*Stats, error) {
	var (
		profile          profileFields
		repos            repoFields
		repoCI           repoCIFields
		rlP, rlR, rlC    rateLimitFields
		errP, errR, errC error
		watched          []Repo
		watchedSkipped   []string
		reviewRequests   []PullRequest
		errRR            error
	)

	watchRefs := c.WatchRepos()
	// Review-requests inbox is only fetched when the client is
	// authenticated AND running in viewer mode (no explicit
	// login arg). The search uses `review-requested:@me` which
	// is inherently personal — there's no point asking GitHub
	// for someone else's inbox.
	wantReviewRequests := c.authenticated && c.login == ""

	var wg sync.WaitGroup
	wg.Add(3)
	if len(watchRefs) > 0 {
		wg.Add(1)
	}
	if wantReviewRequests {
		wg.Add(1)
	}

	go func() {
		defer wg.Done()
		if c.login == "" {
			var q struct {
				Viewer    profileFields
				RateLimit rateLimitFields
			}
			errP = c.gql.Query(ctx, &q, nil)
			profile = q.Viewer
			rlP = q.RateLimit
		} else {
			var q struct {
				User      profileFields `graphql:"user(login: $login)"`
				RateLimit rateLimitFields
			}
			vars := map[string]interface{}{"login": githubv4.String(c.login)}
			errP = c.gql.Query(ctx, &q, vars)
			profile = q.User
			rlP = q.RateLimit
		}
	}()

	go func() {
		defer wg.Done()
		repos, rlR, errR = c.fetchRepoFieldsPaged(ctx)
	}()

	// Third parallel query — CI rollup state per repo, kept on a
	// minimal payload (name + statusCheckRollup.state) so it stays
	// well under the complexity ceiling. Pulling the same field
	// inline on repoFields 502'd the gateway on busy accounts.
	go func() {
		defer wg.Done()
		repoCI, rlC, errC = c.fetchRepoCIFieldsPaged(ctx)
	}()

	// Fourth parallel branch — external watched repos. Each entry
	// resolves to its own singleRepoQuery (FetchWatchedRepos
	// fans out further internally). A failed entry never fails the
	// whole dashboard: unresolvable refs come back in watchedSkipped
	// so the Repos tab can surface them (#37); transient failures
	// are dropped for this refresh.
	if len(watchRefs) > 0 {
		go func() {
			defer wg.Done()
			watched, watchedSkipped = c.FetchWatchedRepos(ctx, watchRefs)
		}()
	}

	// Fifth parallel branch — review-requests inbox (v0.15.0).
	// Single search query, cheap enough to ride alongside the
	// rest of the dashboard refresh. Gated on authenticated-
	// viewer mode upstream (wantReviewRequests).
	if wantReviewRequests {
		go func() {
			defer wg.Done()
			rr, err := c.FetchReviewRequests(ctx)
			if err != nil {
				errRR = err
				return
			}
			reviewRequests = rr
		}()
	}

	wg.Wait()

	// Surface the first error — all three queries serve the same
	// dashboard, so a partial result would be misleading. The
	// classifier sees the actual error so the footer message
	// stays accurate (rate-limit / auth / network / 5xx).
	if errP != nil {
		return nil, &FetchError{Reason: classifyErr(ctx, errP), Err: errP}
	}
	if errR != nil {
		return nil, &FetchError{Reason: classifyErr(ctx, errR), Err: errR}
	}
	if errC != nil {
		return nil, &FetchError{Reason: classifyErr(ctx, errC), Err: errC}
	}
	if errRR != nil {
		return nil, errRR
	}

	stats := c.extractStats(profile, repos, repoCI)
	stats.RateLimit = mergeRateLimit3(rlP, rlR, rlC)
	stats.WatchedRepos = watched
	stats.WatchedSkipped = watchedSkipped
	stats.ReviewRequests = reviewRequests
	return stats, nil
}

// mergeRateLimit picks the more pessimistic side of two
// rate-limit envelopes returned by parallel queries. Remaining is
// the smaller of the two (a single budget shared across calls);
// resetAt and limit are the same on both responses, so we just
// take one (preferring the one that actually returned a non-zero
// limit, in case one side returned an empty envelope).
func mergeRateLimit(a, b rateLimitFields) *RateLimit {
	pick := a
	if int(b.Limit) > 0 && int(b.Remaining) < int(a.Remaining) {
		pick = b
	}
	if int(pick.Limit) == 0 && int(b.Limit) > 0 {
		pick = b
	}
	cost := int(a.Cost) + int(b.Cost)
	return &RateLimit{
		Cost:      cost,
		Limit:     int(pick.Limit),
		Remaining: int(pick.Remaining),
		ResetAt:   pick.ResetAt.Time,
	}
}

// mergeRateLimit3 is the 3-way variant introduced in v0.13.0
// when the parallel fetch grew a third query (repoCIFields).
// Same "most pessimistic remaining wins, costs sum" semantics
// as mergeRateLimit. Lives next to its 2-way counterpart so
// future callers can pick whichever arity fits.
//
// Iterates all three envelopes in one pass and only ever
// considers candidates with Limit>0. The previous
// initialise-with-`a`-then-loop shape produced a wrong pick
// when `a.Limit==0`: pick.Remaining started at 0, and the
// strict `< pick.Remaining` comparison meant no candidate
// could ever beat it, so the fallback path took the first
// non-zero-Limit envelope rather than the smallest
// Remaining one.
func mergeRateLimit3(a, b, c rateLimitFields) *RateLimit {
	cost := int(a.Cost) + int(b.Cost) + int(c.Cost)

	// pick is the most pessimistic envelope seen so far. nil
	// means we haven't seen any valid one yet (Limit==0 on all
	// three is an "every query returned an empty rateLimit"
	// state — rare but possible on heavily-cached responses).
	var pick *rateLimitFields
	for _, cand := range []rateLimitFields{a, b, c} {
		cand := cand
		if int(cand.Limit) == 0 {
			continue
		}
		if pick == nil || int(cand.Remaining) < int(pick.Remaining) {
			pick = &cand
		}
	}

	out := &RateLimit{Cost: cost}
	if pick != nil {
		out.Limit = int(pick.Limit)
		out.Remaining = int(pick.Remaining)
		out.ResetAt = pick.ResetAt.Time
	}
	return out
}

// rateLimitFields mirrors GitHub's top-level rateLimit envelope. Kept
// private so the UI layer consumes only the plain RateLimit struct.
type rateLimitFields struct {
	Cost      githubv4.Int
	Limit     githubv4.Int
	Remaining githubv4.Int
	ResetAt   githubv4.DateTime
}

// classifyErr inspects the error from a GraphQL round-trip and tags
// it with a FetchErrorReason. Pattern matching on err.Error() is
// blunt but avoids tying the UI to oauth2/http internals — the
// relevant signals (status codes, GitHub's "rate limit exceeded"
// message, secondary-limit wording) are all stable.
func classifyErr(ctx context.Context, err error) FetchErrorReason {
	if err == nil {
		return ReasonUnknown
	}
	if ctx.Err() == context.DeadlineExceeded || errors.Is(err, context.DeadlineExceeded) {
		return ReasonNetwork
	}
	msg := strings.ToLower(err.Error())
	switch {
	case strings.Contains(msg, "secondary rate limit"),
		strings.Contains(msg, "abuse detection"):
		return ReasonRateLimitSecondary
	case strings.Contains(msg, "rate limit exceeded"),
		strings.Contains(msg, "api rate limit"):
		return ReasonRateLimitPrimary
	// Scope / permission failures sit apart from plain rejection: the
	// token authenticated fine but can't see the field or resource, so
	// the fix is "grant a scope", not "regenerate a dead token".
	case strings.Contains(msg, "not been granted the required scopes"),
		strings.Contains(msg, "resource not accessible"),
		strings.Contains(msg, "must have admin"):
		return ReasonAuthScope
	case strings.Contains(msg, "bad credentials"),
		strings.Contains(msg, "401"),
		strings.Contains(msg, "requires authentication"):
		return ReasonAuth
	// NOT_FOUND: the queried resource doesn't exist for this token
	// (deleted, renamed, or private-and-invisible). Distinct from the
	// transient classes — retrying won't make it reappear.
	case strings.Contains(msg, "could not resolve to"),
		strings.Contains(msg, "404"):
		return ReasonNotFound
	case strings.Contains(msg, "no such host"),
		strings.Contains(msg, "connection refused"),
		strings.Contains(msg, "network is unreachable"),
		strings.Contains(msg, "i/o timeout"),
		strings.Contains(msg, "tls"),
		strings.Contains(msg, "eof"):
		return ReasonNetwork
	case strings.Contains(msg, "500"),
		strings.Contains(msg, "502"),
		strings.Contains(msg, "503"),
		strings.Contains(msg, "504"),
		strings.Contains(msg, "internal server error"),
		strings.Contains(msg, "bad gateway"),
		strings.Contains(msg, "service unavailable"),
		// HTTP/2 transport errors from the peer (GitHub) under load:
		// RST_STREAM / GOAWAY surface as "stream error: ... CANCEL;
		// received from peer" or "http2: ...". Same transient
		// gateway-congestion class as a 502 — retry-worthy. (Kept off
		// bare "cancel" so it can't swallow a context-cancellation.)
		strings.Contains(msg, "stream error"),
		strings.Contains(msg, "received from peer"),
		strings.Contains(msg, "http2:"),
		strings.Contains(msg, "goaway"):
		return ReasonServer
	}
	return ReasonUnknown
}

// scopeListPattern matches the bracketed list in GitHub's GraphQL
// insufficient-scopes error: "… requires one of the following scopes:
// ['read:user', 'read:org'], but your token has only been granted
// the: […] scopes." Anchoring on "following scopes:" captures only
// the required list — the granted list is introduced by "granted
// the:" and never matches.
var scopeListPattern = regexp.MustCompile(`following scopes: \[([^\]]*)\]`)

// MissingScopes extracts the OAuth scope names an insufficient-scopes
// error says the query needs, so the error UI can tell the user which
// scope to add instead of a generic "check your token". Returns nil
// when the error carries no scope list (the fine-grained-PAT wording
// "Resource not accessible…" names no scopes). Extracted names pass
// through Sanitize and are deduped + capped: they arrive inside a
// GitHub-controlled error body, and the boundary rule applies to
// error strings the same as to field data.
func MissingScopes(err error) []string {
	if err == nil {
		return nil
	}
	const maxScopes = 6
	seen := make(map[string]bool)
	var scopes []string
	for _, m := range scopeListPattern.FindAllStringSubmatch(err.Error(), -1) {
		for _, raw := range strings.Split(m[1], ",") {
			s := Sanitize(strings.Trim(strings.TrimSpace(raw), `'"`))
			if s == "" || seen[s] {
				continue
			}
			seen[s] = true
			scopes = append(scopes, s)
			if len(scopes) >= maxScopes {
				return scopes
			}
		}
	}
	return scopes
}

// extractStats flattens the two GraphQL response halves
// (profileFields + repoFields + repoCIFields) into the UI-facing
// Stats struct, aggregating per-repo totals, deduping languages
// and merging the parallel CI rollup payload by repo name. Pure
// function aside from the client-level Authenticated/IsViewer
// flags. Lives downstream of FetchStats's parallel goroutines so
// the data merge happens in one place rather than scattered
// across the call sites.
func (c *Client) extractStats(p profileFields, r repoFields, ci repoCIFields) *Stats {
	// Build two lookups from the repoCIFields payload — one
	// for the CI rollup state (v0.13.0), one for the latest
	// release header (v0.14.0). Both keyed on the canonical
	// "owner/name" string; bare Name isn't unique once orgs
	// enter the picture. Nodes are bounded by the
	// repositories(first: 100) cap, so the maps stay small.
	ciByNameWithOwner := make(map[string]string, len(ci.Repositories.Nodes))
	type releaseSummary struct {
		tag         string
		publishedAt time.Time
	}
	releaseByNameWithOwner := make(map[string]releaseSummary, len(ci.Repositories.Nodes))
	for _, n := range ci.Repositories.Nodes {
		key := string(n.NameWithOwner)
		if key == "" {
			continue
		}
		ciByNameWithOwner[key] = string(n.DefaultBranchRef.Target.Commit.StatusCheckRollup.State)
		if len(n.Releases.Nodes) > 0 {
			rel := n.Releases.Nodes[0]
			releaseByNameWithOwner[key] = releaseSummary{
				tag:         string(rel.TagName),
				publishedAt: rel.PublishedAt.Time,
			}
		}
	}
	// Sanitize at the boundary — every GitHub-sourced string
	// flowing into Stats passes through Sanitize so the UI layer
	// downstream renders without worrying about embedded
	// terminal-control sequences. See Sanitize doc.
	stats := &Stats{
		Login:                    Sanitize(string(p.Login)),
		Name:                     Sanitize(string(p.Name)),
		Bio:                      Sanitize(string(p.Bio)),
		Company:                  Sanitize(string(p.Company)),
		Location:                 Sanitize(string(p.Location)),
		Pronouns:                 Sanitize(string(p.Pronouns)),
		WebsiteURL:               Sanitize(string(p.WebsiteURL)),
		TwitterUsername:          Sanitize(string(p.TwitterUsername)),
		AvatarURL:                Sanitize(string(p.AvatarURL)),
		CreatedAt:                p.CreatedAt.Time,
		Followers:                int(p.Followers.TotalCount),
		Following:                int(p.Following.TotalCount),
		PRsTotal:                 int(p.PullRequests.TotalCount),
		PRsMerged:                int(p.MergedPRs.TotalCount),
		OpenPRsAuthored:          int(p.OpenPRs.TotalCount),
		IssuesAuthored:           int(p.Issues.TotalCount),
		CommitsLastYear:          int(p.ContributionsCollection.TotalCommitContributions),
		ContributedReposLastYear: int(p.ContributionsCollection.TotalRepositoriesWithContributedCommits),
		PublicRepos:              int(r.Repositories.TotalCount),
		Authenticated:            c.authenticated,
		IsViewer:                 c.login == "",
	}

	for _, sa := range p.SocialAccounts.Nodes {
		stats.SocialAccounts = append(stats.SocialAccounts, SocialAccount{
			Provider:    Sanitize(string(sa.Provider)),
			URL:         Sanitize(string(sa.URL)),
			DisplayName: Sanitize(string(sa.DisplayName)),
		})
	}

	for _, o := range p.Organizations.Nodes {
		stats.Organizations = append(stats.Organizations, Organization{
			Login: Sanitize(string(o.Login)),
			Name:  Sanitize(string(o.Name)),
		})
	}

	// Always include everything in the slice — the publicOnly filter
	// has moved to the render path (see Stats.Public) so toggling it
	// at runtime no longer requires a refetch.
	for _, pr := range p.OpenPRs.Nodes {
		stats.OpenPullRequests = append(stats.OpenPullRequests, PullRequest{
			Number:    int(pr.Number),
			Title:     Sanitize(string(pr.Title)),
			URL:       Sanitize(string(pr.URL)),
			Repo:      Sanitize(string(pr.Repository.NameWithOwner)),
			IsDraft:   bool(pr.IsDraft),
			Mergeable: string(pr.Mergeable),
			UpdatedAt: pr.UpdatedAt.Time,
			IsPrivate: bool(pr.Repository.IsPrivate),
		})
	}

	for _, is := range p.OpenIssuesList.Nodes {
		stats.OpenIssuesList = append(stats.OpenIssuesList, Issue{
			Number:    int(is.Number),
			Title:     Sanitize(string(is.Title)),
			URL:       Sanitize(string(is.URL)),
			Repo:      Sanitize(string(is.Repository.NameWithOwner)),
			UpdatedAt: is.UpdatedAt.Time,
			IsPrivate: bool(is.Repository.IsPrivate),
		})
	}

	for _, w := range p.ContributionsCollection.ContributionCalendar.Weeks {
		week := make([]ContributionDay, 0, len(w.ContributionDays))
		for _, d := range w.ContributionDays {
			parsed, _ := time.Parse("2006-01-02", string(d.Date))
			week = append(week, ContributionDay{
				Date:    parsed,
				Count:   int(d.ContributionCount),
				Weekday: int(d.Weekday),
			})
		}
		stats.ContributionWeeks = append(stats.ContributionWeeks, week)
	}

	langMap := map[string]*Language{}
	for _, repo := range r.Repositories.Nodes {
		stats.TotalStars += int(repo.StargazerCount)
		stats.ForksReceived += int(repo.ForkCount)
		stats.OpenIssues += int(repo.Issues.TotalCount)
		stats.OpenPRs += int(repo.PullRequests.TotalCount)

		key := string(repo.NameWithOwner)
		rel := releaseByNameWithOwner[key]
		stats.Repositories = append(stats.Repositories, Repo{
			Name:                     Sanitize(string(repo.Name)),
			URL:                      Sanitize(string(repo.URL)),
			PrimaryLanguage:          Sanitize(string(repo.PrimaryLanguage.Name)),
			LanguageColor:            Sanitize(string(repo.PrimaryLanguage.Color)),
			Stars:                    int(repo.StargazerCount),
			Forks:                    int(repo.ForkCount),
			OpenIssues:               int(repo.Issues.TotalCount),
			OpenPRs:                  int(repo.PullRequests.TotalCount),
			PushedAt:                 repo.PushedAt.Time,
			IsPrivate:                bool(repo.IsPrivate),
			CIState:                  Sanitize(ciByNameWithOwner[key]),
			LatestReleaseTag:         Sanitize(rel.tag),
			LatestReleasePublishedAt: rel.publishedAt,
		})

		for _, e := range repo.Languages.Edges {
			name := Sanitize(string(e.Node.Name))
			if l, ok := langMap[name]; ok {
				l.Bytes += int(e.Size)
			} else {
				langMap[name] = &Language{
					Name:  name,
					Color: Sanitize(string(e.Node.Color)),
					Bytes: int(e.Size),
				}
			}
		}
	}

	// Flatten language map to a slice sorted desc by byte count. Cap
	// to the top 6 so the TUI bar stays readable.
	for _, l := range langMap {
		stats.Languages = append(stats.Languages, *l)
	}
	sort.Slice(stats.Languages, func(i, j int) bool {
		return stats.Languages[i].Bytes > stats.Languages[j].Bytes
	})
	if len(stats.Languages) > 6 {
		stats.Languages = stats.Languages[:6]
	}

	stats.TotalStarsWithForks = stats.TotalStars
	for _, fr := range p.ForkedRepos.Nodes {
		stats.TotalStarsWithForks += int(fr.StargazerCount)
	}

	return stats
}
