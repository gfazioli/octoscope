package github

// Gists — the one account-centric surface the dashboard did not represent
// (#76). Repos, PRs, issues, activity and stars were all there; this was a
// gap rather than an expansion, which is why it reuses the existing list-tab
// shape instead of introducing a mental model of its own.

import (
	"context"
	"time"

	"github.com/shurcooL/githubv4"
)

// gistsPageSize bounds what one refresh pulls. Deliberately a single page:
// paging is unbounded work inside a dashboard fetch that is bounded
// everywhere else, and a hundred gists is already far past what anyone
// scrolls. TotalCount comes back alongside, so a truncated list can *say*
// it is truncated rather than quietly looking complete — the same choice
// the scan's probes make about their own limits.
const gistsPageSize = 100

// GistFilesLimit is the cap on files fetched per gist, and it is exported
// because the UI has to know it: `Gist.files` is a plain list, not a Relay
// connection, so there is **no totalCount** to ask for. A gist at the cap
// is therefore indistinguishable from one with exactly this many files
// unless the renderer says so — which is why the row shows "20+" rather
// than a number it cannot stand behind.
//
// Keep in sync with the `files(limit: 20)` tag below; TestGistFilesLimitMatchesQuery
// fails if they drift, since a struct tag cannot interpolate a constant.
const GistFilesLimit = 20

// GistFile is one file inside a gist. Size is bytes; Language is GitHub's
// own detection and is empty when it has none.
type GistFile struct {
	Name     string
	Language string
	Size     int
}

// Gist is one gist as the dashboard shows it.
//
// Description is left exactly as GitHub returned it, **including empty** —
// 2 of 16 gists on a real account have none. Choosing what to display in
// that case is presentation and belongs to the UI, not here: inventing a
// description would put a value into `--json` output that GitHub never
// gave us.
type Gist struct {
	// Name is the gist's hash id, which is also the last path segment of
	// its URL. GitHub calls this "name"; it is not human-readable.
	Name        string
	Description string
	URL         string
	IsPublic    bool
	IsFork      bool
	Stars       int
	Files       []GistFile
	UpdatedAt   time.Time
}

// gistFields is the query shape. URL is queried as String rather than URI
// for the reason the rest of this package does: URI unmarshals through
// url.Parse, and one control character in one field aborts the decode of
// the entire response.
type gistFields struct {
	TotalCount githubv4.Int
	Nodes      []struct {
		Name           githubv4.String
		Description    githubv4.String
		URL            githubv4.String `graphql:"url"`
		IsPublic       githubv4.Boolean
		IsFork         githubv4.Boolean
		StargazerCount githubv4.Int
		UpdatedAt      githubv4.DateTime
		Files          []struct {
			Name     githubv4.String
			Size     githubv4.Int
			Language struct {
				Name githubv4.String
			}
		} `graphql:"files(limit: 20)"`
	}
}

// FetchGists returns the account's gists, newest update first, plus the
// total GitHub reports so the caller can tell a full list from a truncated
// one.
//
// **Privacy is a query argument, not a filter.** Under --public-only the
// query asks for PUBLIC and secret gists are never fetched at all, rather
// than arriving and being dropped afterwards. For a screenshot-safe mode
// that is the difference between data that cannot leak and data that is
// merely not drawn.
//
// It also keeps the *count* honest, which filtering client-side would not.
// Measured on a real account: ALL returns 16 with 6 secret, and PUBLIC
// returns `totalCount: 10` — not 16. Had this fetched everything and hidden
// the secrets, a "10 of 16" line would itself have disclosed that six secret
// gists exist, which is the kind of leak a screenshot-safe mode is for.
//
// Viewing *another* account degrades on its own: GitHub returns only their
// public gists for a PUBLIC or ALL query, with no error. Asking for SECRET
// there is what errors — measured, and it returns `totalCount: 0` **with**
// a GraphQL error attached, which is why this never asks for SECRET and why
// its caller treats a failure as best-effort. A partial response carrying
// an error is exactly the shape that would abort a shared query branch.
func (c *Client) FetchGists(ctx context.Context) ([]Gist, int, error) {
	privacy := githubv4.GistPrivacyAll
	if c.publicOnly {
		privacy = githubv4.GistPrivacyPublic
	}

	vars := map[string]interface{}{
		"first":   githubv4.Int(gistsPageSize),
		"privacy": privacy,
		"order": githubv4.GistOrder{
			Field:     githubv4.GistOrderFieldUpdatedAt,
			Direction: githubv4.OrderDirectionDesc,
		},
	}

	var fields gistFields
	if c.login == "" {
		var q struct {
			Viewer struct {
				Gists gistFields `graphql:"gists(first: $first, privacy: $privacy, orderBy: $order)"`
			}
		}
		if err := c.gql.Query(ctx, &q, vars); err != nil {
			return nil, 0, &FetchError{Reason: classifyErr(ctx, err), Err: err}
		}
		fields = q.Viewer.Gists
	} else {
		var q struct {
			User struct {
				Gists gistFields `graphql:"gists(first: $first, privacy: $privacy, orderBy: $order)"`
			} `graphql:"user(login: $login)"`
		}
		vars["login"] = githubv4.String(c.login)
		if err := c.gql.Query(ctx, &q, vars); err != nil {
			return nil, 0, &FetchError{Reason: classifyErr(ctx, err), Err: err}
		}
		fields = q.User.Gists
	}

	return extractGists(fields), int(fields.TotalCount), nil
}

// extractGists is the pure half, so the mapping is testable without a
// transport. Every string crosses Sanitize here at the extractor boundary,
// which is where this package cleans GitHub-sourced text — a gist
// description and a filename are both attacker-controlled by anyone who can
// share a gist link.
func extractGists(f gistFields) []Gist {
	if len(f.Nodes) == 0 {
		return nil
	}
	out := make([]Gist, 0, len(f.Nodes))
	for _, n := range f.Nodes {
		g := Gist{
			Name:        Sanitize(string(n.Name)),
			Description: Sanitize(string(n.Description)),
			URL:         Sanitize(string(n.URL)),
			IsPublic:    bool(n.IsPublic),
			IsFork:      bool(n.IsFork),
			Stars:       int(n.StargazerCount),
			UpdatedAt:   n.UpdatedAt.Time,
		}
		for _, file := range n.Files {
			g.Files = append(g.Files, GistFile{
				Name:     Sanitize(string(file.Name)),
				Language: Sanitize(string(file.Language.Name)),
				Size:     int(file.Size),
			})
		}
		out = append(out, g)
	}
	return out
}
