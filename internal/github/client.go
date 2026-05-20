// Package github is a thin wrapper around githubv4 that pulls only the
// fields octoscope needs and returns them as a Sendable, UI-friendly
// struct. Keeping the surface area narrow makes it trivial to swap the
// transport later if we ever outgrow the v4 GraphQL client.
package github

import (
	"context"
	"errors"
	"net/http"
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
	login         string
	publicOnly    bool

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
	ReasonAuth                                // 401/403 from token rejection
	ReasonNetwork                             // DNS, TCP, TLS, context timeout
	ReasonServer                              // 5xx or GraphQL-level error
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

// Public returns a copy of s with private repositories, PRs and
// issues stripped from the lists. Aggregate counters that depend on
// per-repo data (TotalStars, ForksReceived, OpenIssues, OpenPRs,
// PublicRepos, Languages) are recomputed from the kept repos so the
// Overview cards stay consistent with what the lists show.
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
	langMap := map[string]*Language{}
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
	for _, l := range s.Languages {
		langMap[l.Name] = nil
	}
	_ = langMap // reserved for future per-repo language breakdown
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
	token := auth.Token()
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
	} `graphql:"repositories(first: 100, ownerAffiliations: OWNER, isFork: false)"`
}

// repoCIFields is the third parallel query introduced in v0.13.0
// to power the Repos-tab CI status column. Kept separate from
// repoFields because pulling statusCheckRollup inline on the main
// repository nodes blew GitHub's gateway complexity ceiling and
// 502'd on busy accounts — exactly the same failure mode that
// drove the v0.10.1 split. Each node carries only the bare
// minimum (nameWithOwner + rollup state) so this query stays
// cheap; the merge happens by NameWithOwner in extractStats so
// org-level repos don't collide with personal repos that share
// a bare name.
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
		}
	} `graphql:"repositories(first: 100, ownerAffiliations: OWNER, isFork: false)"`
}

// FetchStats runs the dashboard fetch as **two parallel GraphQL
// queries** — profileFields and repoFields — and combines them
// into the UI-facing Stats. Splitting was forced by GitHub's
// gateway 502'ing the original single-query approach once an
// account grew busy enough; the per-query complexity now sits
// well under the threshold and total wall-clock latency stays
// close to the slower of the two rather than their sum.
//
// Routes both queries against `viewer` when Client.login is
// empty, otherwise against `user(login: $login)`. Errors from
// either side fail the whole fetch — partial Stats would be
// confusing in the UI (e.g. profile loaded but Repos tab empty).
//
// The reported RateLimit is whichever side has the smaller
// `remaining` (most pessimistic estimate). Both queries cost ~1
// point each, so the chip stays accurate to the actual budget
// drawn.
//
// Limitation: repository pagination is capped at 100. Users with
// more repos will under-count aggregated totals (stars, forks,
// open issues, open PRs, language bytes). Viewer-level counters
// (followers, PRs, issues) are unaffected.
func (c *Client) FetchStats(ctx context.Context) (*Stats, error) {
	var (
		profile          profileFields
		repos            repoFields
		repoCI           repoCIFields
		rlP, rlR, rlC    rateLimitFields
		errP, errR, errC error
	)

	var wg sync.WaitGroup
	wg.Add(3)

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
		if c.login == "" {
			var q struct {
				Viewer    repoFields
				RateLimit rateLimitFields
			}
			errR = c.gql.Query(ctx, &q, nil)
			repos = q.Viewer
			rlR = q.RateLimit
		} else {
			var q struct {
				User      repoFields `graphql:"user(login: $login)"`
				RateLimit rateLimitFields
			}
			vars := map[string]interface{}{"login": githubv4.String(c.login)}
			errR = c.gql.Query(ctx, &q, vars)
			repos = q.User
			rlR = q.RateLimit
		}
	}()

	// Third parallel query — CI rollup state per repo, kept on a
	// minimal payload (name + statusCheckRollup.state) so it stays
	// well under the complexity ceiling. Pulling the same field
	// inline on repoFields 502'd the gateway on busy accounts.
	go func() {
		defer wg.Done()
		if c.login == "" {
			var q struct {
				Viewer    repoCIFields
				RateLimit rateLimitFields
			}
			errC = c.gql.Query(ctx, &q, nil)
			repoCI = q.Viewer
			rlC = q.RateLimit
		} else {
			var q struct {
				User      repoCIFields `graphql:"user(login: $login)"`
				RateLimit rateLimitFields
			}
			vars := map[string]interface{}{"login": githubv4.String(c.login)}
			errC = c.gql.Query(ctx, &q, vars)
			repoCI = q.User
			rlC = q.RateLimit
		}
	}()

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

	stats := c.extractStats(profile, repos, repoCI)
	stats.RateLimit = mergeRateLimit3(rlP, rlR, rlC)
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
	case strings.Contains(msg, "bad credentials"),
		strings.Contains(msg, "401"),
		strings.Contains(msg, "requires authentication"),
		strings.Contains(msg, "must have admin"),
		strings.Contains(msg, "resource not accessible"):
		return ReasonAuth
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
		strings.Contains(msg, "service unavailable"):
		return ReasonServer
	}
	return ReasonUnknown
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
	// Build the CI lookup once, keyed on the canonical
	// "owner/name" string. Bare Name isn't unique inside
	// ownerAffiliations: OWNER once orgs are in the picture (an
	// org may own a repo called the same as a personal one) —
	// using NameWithOwner as the key keeps the merge correct in
	// every account shape. Nodes are bounded by the
	// repositories(first: 100) cap, so the map stays small.
	ciByNameWithOwner := make(map[string]string, len(ci.Repositories.Nodes))
	for _, n := range ci.Repositories.Nodes {
		key := string(n.NameWithOwner)
		if key == "" {
			continue
		}
		ciByNameWithOwner[key] = string(n.DefaultBranchRef.Target.Commit.StatusCheckRollup.State)
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

		stats.Repositories = append(stats.Repositories, Repo{
			Name:            Sanitize(string(repo.Name)),
			URL:             Sanitize(string(repo.URL)),
			PrimaryLanguage: Sanitize(string(repo.PrimaryLanguage.Name)),
			LanguageColor:   Sanitize(string(repo.PrimaryLanguage.Color)),
			Stars:           int(repo.StargazerCount),
			Forks:           int(repo.ForkCount),
			OpenIssues:      int(repo.Issues.TotalCount),
			OpenPRs:         int(repo.PullRequests.TotalCount),
			PushedAt:        repo.PushedAt.Time,
			IsPrivate:       bool(repo.IsPrivate),
			CIState:         Sanitize(ciByNameWithOwner[string(repo.NameWithOwner)]),
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
