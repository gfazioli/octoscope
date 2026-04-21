// Package github is a thin wrapper around githubv4 that pulls only the
// fields octoscope needs and returns them as a Sendable, UI-friendly
// struct. Keeping the surface area narrow makes it trivial to swap the
// transport later if we ever outgrow the v4 GraphQL client.
package github

import (
	"context"
	"net/http"

	"github.com/gfazioli/octoscope/internal/auth"
	"github.com/shurcooL/githubv4"
	"golang.org/x/oauth2"
)

// Client wraps a githubv4.Client. Safe to share across goroutines.
type Client struct {
	gql           *githubv4.Client
	authenticated bool
}

// Stats is the snapshot consumed by the TUI. All fields are populated
// by a single GraphQL query; missing/unset fields are zero-valued.
type Stats struct {
	Login         string
	Name          string
	AvatarURL     string
	Followers     int
	Following     int
	PublicRepos   int
	TotalStars    int
	OpenIssues    int
	OpenPRs       int
	Authenticated bool
}

// New builds a client, preferring an authenticated one when a token is
// available. An unauthenticated client still works but is rate-limited
// to 60 requests/hour by GitHub.
func New() (*Client, error) {
	token := auth.Token()
	var httpClient *http.Client
	authed := false
	if token != "" {
		src := oauth2.StaticTokenSource(&oauth2.Token{AccessToken: token})
		httpClient = oauth2.NewClient(context.Background(), src)
		authed = true
	}
	return &Client{
		gql:           githubv4.NewClient(httpClient),
		authenticated: authed,
	}, nil
}

// FetchStats runs a single GraphQL query that pulls the viewer's top-level
// counters plus per-repo totals to aggregate stars, open issues and PRs.
//
// Limitation: pagination is currently capped at 100 repositories. Users
// with more repos will under-count aggregated totals. A follow-up will
// add cursor-based pagination when someone hits that cap.
func (c *Client) FetchStats(ctx context.Context) (*Stats, error) {
	var q struct {
		Viewer struct {
			Login     githubv4.String
			Name      githubv4.String
			AvatarURL githubv4.String `graphql:"avatarUrl(size: 64)"`
			Followers struct {
				TotalCount githubv4.Int
			}
			Following struct {
				TotalCount githubv4.Int
			}
			Repositories struct {
				TotalCount githubv4.Int
				Nodes      []struct {
					StargazerCount githubv4.Int
					Issues         struct {
						TotalCount githubv4.Int
					} `graphql:"issues(states: OPEN)"`
					PullRequests struct {
						TotalCount githubv4.Int
					} `graphql:"pullRequests(states: OPEN)"`
				}
			} `graphql:"repositories(first: 100, ownerAffiliations: OWNER, isFork: false)"`
		}
	}

	if err := c.gql.Query(ctx, &q, nil); err != nil {
		return nil, err
	}

	stats := &Stats{
		Login:         string(q.Viewer.Login),
		Name:          string(q.Viewer.Name),
		AvatarURL:     string(q.Viewer.AvatarURL),
		Followers:     int(q.Viewer.Followers.TotalCount),
		Following:     int(q.Viewer.Following.TotalCount),
		PublicRepos:   int(q.Viewer.Repositories.TotalCount),
		Authenticated: c.authenticated,
	}
	for _, r := range q.Viewer.Repositories.Nodes {
		stats.TotalStars += int(r.StargazerCount)
		stats.OpenIssues += int(r.Issues.TotalCount)
		stats.OpenPRs += int(r.PullRequests.TotalCount)
	}
	return stats, nil
}
