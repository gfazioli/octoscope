package github

import (
	"context"
	"strings"
	"time"

	"github.com/shurcooL/githubv4"
)

// RepoDetail is the rich per-repository payload feeding the Repos
// drill-in view. Populated by FetchRepoDetail in a single targeted
// GraphQL query (~5–10 complexity points), so opening a detail
// never causes a fan-out across the user's whole repo list — which
// is what doomed the configurable-columns approach (issue #4).
//
// Most slice fields are nil-or-empty when the repo doesn't expose
// the corresponding data: a fresh repo has no releases, a private
// repo with no commits has a nil DefaultBranch (rendered as
// HasDefaultBranch=false), an old repo can have zero topics. The UI
// hides those sections rather than rendering "(none)" placeholders.
type RepoDetail struct {
	// Identity
	Owner       string
	Name        string
	URL         string
	Description string

	// Status flags
	IsPrivate  bool
	IsArchived bool
	IsFork     bool

	// Lifetime
	CreatedAt time.Time
	PushedAt  time.Time

	// Top-line numbers (same definitions as Stats / Repo)
	Stars      int
	Forks      int
	OpenIssues int
	OpenPRs    int

	// One-line metadata for the header chip row
	License             string // "" when unlicensed
	PrimaryLanguage     string
	PrimaryLanguageColor string

	// Body sections — each may be empty / nil
	LatestRelease     *Release
	Languages         []Language

	// Default-branch commit metrics. HasDefaultBranch is false on
	// empty repos (no pushes yet); Commits and CommitsYearAuthored
	// then both stay 0. Commits is the all-authors total on the
	// default branch; CommitsYearAuthored is the viewer's commits
	// over the last 365 days (0 when the client is unauthenticated
	// or when the viewer never contributed). The single-repo scope
	// of the detail query keeps these costs negligible — what
	// failed in #4 was asking for the same fields across 100 repos
	// in a single fan-out.
	HasDefaultBranch    bool
	Commits             int
	CommitsYearAuthored int

	// AuthorFilterApplied flags whether CommitsYearAuthored was
	// computed against an actual viewer ID. False for
	// unauthenticated clients — the count is then meaningless (no
	// "you" to filter on) and the UI hides the "by you in the last
	// year" sub-line. True for authenticated clients regardless of
	// whether the viewer is also the repo owner: "I committed to
	// torvalds/linux this year" is a real signal.
	AuthorFilterApplied bool

	RecentCommits     []Commit
	OpenIssuesPreview []IssuePreview
	OpenPRsPreview    []IssuePreview
	Topics            []string
}

// Release is the headline info for one GitHub release. Populated
// only for the most recent release in v0.10.0; future iterations
// could surface the changelog body or a list of releases.
type Release struct {
	Name        string // sometimes empty (release uses tag name as title)
	TagName     string
	PublishedAt time.Time
}

// Commit is one entry in the "Recent commits" list of a repo
// detail. Author resolves to the GitHub login when GitHub could
// match the commit's author email to a user account; falls back to
// the raw committer name otherwise (the email field itself is never
// surfaced — privacy + clutter).
type Commit struct {
	OID             string
	MessageHeadline string
	CommittedDate   time.Time
	Author          string
}

// IssuePreview is the row idiom shared by the "Open issues" and
// "Open pull requests" preview sections of a repo detail. Three of
// these per section is the cap; users who want the full list use
// the dedicated Issues / PRs tabs.
type IssuePreview struct {
	Number    int
	Title     string
	URL       string
	UpdatedAt time.Time
}

// repoDetailQuery is the GraphQL shape for FetchRepoDetail. Mirrors
// RepoDetail closely; the conversion happens in extractRepoDetail.
//
// Cost note: history(first: 5) on a single repo is cheap (~1 point);
// languages and previews are negligible. Total query ~5–10 points,
// safely below the 5000/h budget even at aggressive refresh rates.
// This is the architectural reason detail view fits where
// per-repo-on-the-list metrics did not (issue #4 retro).
type repoDetailQuery struct {
	Repository struct {
		Name        githubv4.String
		Description githubv4.String
		URL         githubv4.String `graphql:"url"`
		IsPrivate   githubv4.Boolean
		IsArchived  githubv4.Boolean
		IsFork      githubv4.Boolean
		CreatedAt   githubv4.DateTime
		PushedAt    githubv4.DateTime

		StargazerCount githubv4.Int
		ForkCount      githubv4.Int

		LicenseInfo *struct {
			Name githubv4.String
		}
		PrimaryLanguage *struct {
			Name  githubv4.String
			Color githubv4.String
		}

		Issues struct {
			TotalCount githubv4.Int
		} `graphql:"issues(states: OPEN)"`

		PullRequests struct {
			TotalCount githubv4.Int
		} `graphql:"pullRequests(states: OPEN)"`

		// Aliased preview lists — same field re-queried with a
		// different limit + orderBy. GraphQL aliases let us coexist
		// with the totalCount-only sibling above without conflict.
		OpenIssuesPreview struct {
			Nodes []struct {
				Number    githubv4.Int
				Title     githubv4.String
				URL       githubv4.String `graphql:"url"`
				UpdatedAt githubv4.DateTime
			}
		} `graphql:"openIssuesPreview: issues(states: OPEN, first: 3, orderBy: {field: UPDATED_AT, direction: DESC})"`

		OpenPRsPreview struct {
			Nodes []struct {
				Number    githubv4.Int
				Title     githubv4.String
				URL       githubv4.String `graphql:"url"`
				UpdatedAt githubv4.DateTime
			}
		} `graphql:"openPRsPreview: pullRequests(states: OPEN, first: 3, orderBy: {field: UPDATED_AT, direction: DESC})"`

		Releases struct {
			Nodes []struct {
				Name        githubv4.String
				TagName     githubv4.String
				PublishedAt githubv4.DateTime
			}
		} `graphql:"releases(first: 1, orderBy: {field: CREATED_AT, direction: DESC})"`

		Languages struct {
			Edges []struct {
				Size githubv4.Int
				Node struct {
					Name  githubv4.String
					Color githubv4.String
				}
			}
		} `graphql:"languages(first: 10, orderBy: {field: SIZE, direction: DESC})"`

		// 5 commits on the default branch — single repo, server-side
		// scan over a tiny window, doesn't trigger the gateway 502
		// that the per-list approach hit. Same query also pulls two
		// totalCount sibling histories (all-time, and viewer-authored
		// last year) using GraphQL aliases. All in ~2 complexity
		// points because they're scoped to one repo, not a fan-out.
		DefaultBranchRef *struct {
			Target struct {
				Commit struct {
					RecentHistory struct {
						Nodes []struct {
							OID             githubv4.GitObjectID `graphql:"oid"`
							MessageHeadline githubv4.String
							CommittedDate   githubv4.DateTime
							Author          struct {
								Name githubv4.String // raw committer name; fallback when GH can't link to a user
								User *struct {
									Login githubv4.String
								}
							}
						}
					} `graphql:"recentHistory: history(first: 5)"`
					TotalHistory struct {
						TotalCount githubv4.Int
					} `graphql:"totalHistory: history"`
					// AuthoredYear is gated on $hasAuthor via GraphQL's
					// @include directive so we can skip it cleanly when
					// the client is unauthenticated (no viewerID to
					// filter on). Without the directive, $authorFilter
					// would be sent as `{id: ""}` — GitHub rejects that
					// as an invalid CommitAuthor and the entire detail
					// query fails. With @include(if: false), the server
					// never resolves authoredYear, so the empty
					// $authorFilter is accepted as input but never
					// evaluated.
					AuthoredYear struct {
						TotalCount githubv4.Int
					} `graphql:"authoredYear: history(since: $since, author: $authorFilter) @include(if: $hasAuthor)"`
				} `graphql:"... on Commit"`
			}
		}

		RepositoryTopics struct {
			Nodes []struct {
				Topic struct {
					Name githubv4.String
				}
			}
		} `graphql:"repositoryTopics(first: 10)"`
	} `graphql:"repository(owner: $owner, name: $name)"`
}

// FetchRepoDetail runs a single targeted query for one repository
// and returns the rich detail payload. Wraps transport / GraphQL
// errors in *FetchError so the UI can render the same actionable
// classifications as on dashboard refresh failures.
//
// One bootstrap round-trip happens once per session via
// ensureViewerID to capture the viewer's node ID for the
// $authorFilter variable; subsequent detail fetches reuse the
// cached value, so the steady state is the single targeted query
// the design promised.
//
// Owner/name are taken from the repo's URL via SplitOwnerName by
// callers who already have a Repo on hand — see the action-menu
// dispatch in the ui package.
func (c *Client) FetchRepoDetail(ctx context.Context, owner, name string) (*RepoDetail, error) {
	viewerID, hasAuthor, err := c.ensureViewerID(ctx)
	if err != nil {
		return nil, &FetchError{Reason: classifyErr(ctx, err), Err: err}
	}

	// $since pins the authored-year window to "365 days ago"; the
	// matching $authorFilter pins the filter to the cached viewer
	// ID. The authoredYear field is gated on $hasAuthor in the
	// query (via @include) so when we don't have a viewer ID
	// (unauthenticated client browsing a public profile) the field
	// is skipped server-side and the empty $authorFilter is never
	// validated. Sending CommitAuthor{ID: &""} would otherwise
	// cause GitHub to reject the entire query — see the @include
	// comment on AuthoredYear in the struct above.
	//
	// Both viewerID and hasAuthor come from ensureViewerID's
	// mutex-guarded return so we never read c.viewerID directly —
	// keeps overlapping fetches (e.g. retry while previous request
	// is in flight) race-free.
	since := githubv4.GitTimestamp{Time: time.Now().Add(-365 * 24 * time.Hour)}
	var authorFilter githubv4.CommitAuthor
	if hasAuthor {
		// Capture into a local so we can take its address without
		// aliasing the cache field (which would also be wrong:
		// reading it without the lock).
		id := viewerID
		authorFilter = githubv4.CommitAuthor{ID: &id}
	}

	var q repoDetailQuery
	variables := map[string]interface{}{
		"owner":        githubv4.String(owner),
		"name":         githubv4.String(name),
		"since":        since,
		"authorFilter": authorFilter,
		"hasAuthor":    githubv4.Boolean(hasAuthor),
	}
	if err := c.gql.Query(ctx, &q, variables); err != nil {
		return nil, &FetchError{Reason: classifyErr(ctx, err), Err: err}
	}
	// Pass hasAuthor (not c.authenticated) so the UI flag tracks
	// the actual filter state. They diverge in the edge case
	// "authenticated but ensureViewerID failed silently" — the
	// filter was skipped, so the UI shouldn't claim it applied.
	return extractRepoDetail(owner, q, hasAuthor), nil
}

// SplitOwnerName parses a github.com URL into its (owner, name)
// pair. Defensive against trailing slashes / fragments / extra
// path segments — anything past the second slash after the host is
// dropped. Returns ("", "") when the URL doesn't look like a
// github.com repo URL, so callers can short-circuit cleanly.
func SplitOwnerName(repoURL string) (string, string) {
	const prefix = "https://github.com/"
	if !strings.HasPrefix(repoURL, prefix) {
		return "", ""
	}
	rest := strings.TrimPrefix(repoURL, prefix)
	parts := strings.Split(rest, "/")
	if len(parts) < 2 || parts[0] == "" || parts[1] == "" {
		return "", ""
	}
	return parts[0], parts[1]
}

// extractRepoDetail flattens the GraphQL response into the
// UI-facing RepoDetail. Pure function: every choice (which fields
// to forward, how to fall back when a sub-node is nil) lives here
// so the rest of the package doesn't reach into raw query types.
//
// `authorFilterApplied` propagates whether the caller was
// authenticated when running the query — needed because the
// authoredYear.totalCount payload itself can't tell us "0 because
// the viewer didn't commit" apart from "0 because we sent an empty
// author filter".
func extractRepoDetail(owner string, q repoDetailQuery, authorFilterApplied bool) *RepoDetail {
	r := q.Repository
	d := &RepoDetail{
		Owner:               owner,
		Name:                string(r.Name),
		URL:                 string(r.URL),
		Description:         string(r.Description),
		IsPrivate:           bool(r.IsPrivate),
		IsArchived:          bool(r.IsArchived),
		IsFork:              bool(r.IsFork),
		CreatedAt:           r.CreatedAt.Time,
		PushedAt:            r.PushedAt.Time,
		Stars:               int(r.StargazerCount),
		Forks:               int(r.ForkCount),
		OpenIssues:          int(r.Issues.TotalCount),
		OpenPRs:             int(r.PullRequests.TotalCount),
		AuthorFilterApplied: authorFilterApplied,
	}

	if r.LicenseInfo != nil {
		d.License = string(r.LicenseInfo.Name)
	}
	if r.PrimaryLanguage != nil {
		d.PrimaryLanguage = string(r.PrimaryLanguage.Name)
		d.PrimaryLanguageColor = string(r.PrimaryLanguage.Color)
	}

	if len(r.Releases.Nodes) > 0 {
		rel := r.Releases.Nodes[0]
		d.LatestRelease = &Release{
			Name:        string(rel.Name),
			TagName:     string(rel.TagName),
			PublishedAt: rel.PublishedAt.Time,
		}
	}

	for _, e := range r.Languages.Edges {
		d.Languages = append(d.Languages, Language{
			Name:  string(e.Node.Name),
			Color: string(e.Node.Color),
			Bytes: int(e.Size),
		})
	}

	if r.DefaultBranchRef != nil {
		d.HasDefaultBranch = true
		d.Commits = int(r.DefaultBranchRef.Target.Commit.TotalHistory.TotalCount)
		d.CommitsYearAuthored = int(r.DefaultBranchRef.Target.Commit.AuthoredYear.TotalCount)
		for _, c := range r.DefaultBranchRef.Target.Commit.RecentHistory.Nodes {
			author := string(c.Author.Name)
			if c.Author.User != nil && string(c.Author.User.Login) != "" {
				author = string(c.Author.User.Login)
			}
			d.RecentCommits = append(d.RecentCommits, Commit{
				OID:             string(c.OID),
				MessageHeadline: string(c.MessageHeadline),
				CommittedDate:   c.CommittedDate.Time,
				Author:          author,
			})
		}
	}

	for _, n := range r.OpenIssuesPreview.Nodes {
		d.OpenIssuesPreview = append(d.OpenIssuesPreview, IssuePreview{
			Number:    int(n.Number),
			Title:     string(n.Title),
			URL:       string(n.URL),
			UpdatedAt: n.UpdatedAt.Time,
		})
	}
	for _, n := range r.OpenPRsPreview.Nodes {
		d.OpenPRsPreview = append(d.OpenPRsPreview, IssuePreview{
			Number:    int(n.Number),
			Title:     string(n.Title),
			URL:       string(n.URL),
			UpdatedAt: n.UpdatedAt.Time,
		})
	}

	for _, t := range r.RepositoryTopics.Nodes {
		if name := string(t.Topic.Name); name != "" {
			d.Topics = append(d.Topics, name)
		}
	}

	return d
}
