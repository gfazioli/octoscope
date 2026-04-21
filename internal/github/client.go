// Package github is a thin wrapper around githubv4 that pulls only the
// fields octoscope needs and returns them as a Sendable, UI-friendly
// struct. Keeping the surface area narrow makes it trivial to swap the
// transport later if we ever outgrow the v4 GraphQL client.
package github

import (
	"context"
	"net/http"
	"sort"
	"time"

	"github.com/gfazioli/octoscope/internal/auth"
	"github.com/shurcooL/githubv4"
	"golang.org/x/oauth2"
)

// Client wraps a githubv4.Client. Safe to share across goroutines.
type Client struct {
	gql           *githubv4.Client
	authenticated bool
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

	// Activity (lifetime counts unless noted)
	PRsTotal                 int
	PRsMerged                int
	IssuesAuthored           int
	CommitsLastYear          int
	ContributedReposLastYear int
	Languages                []Language

	// Operational (current-state counts across owned non-fork repos)
	PublicRepos   int
	ForksReceived int
	OpenIssues    int
	OpenPRs       int

	// Network
	Organizations []Organization

	// Meta
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

// FetchStats runs a single GraphQL query that pulls everything the TUI
// needs — profile, social, activity, operational and network — then
// aggregates per-repo totals client-side (stars, forks, languages).
//
// Limitation: repository pagination is capped at 100 for now. Users
// with more repos will under-count aggregated totals (stars, forks,
// open issues/PRs, language bytes). The viewer's flat counters
// (followers, PRs, issues) are unaffected.
func (c *Client) FetchStats(ctx context.Context) (*Stats, error) {
	var q struct {
		Viewer struct {
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

			// `pullRequests` without args returns all PRs the viewer
			// has authored (any state). Named alias used via tag so
			// we don't shadow the struct field names.
			PullRequests struct {
				TotalCount githubv4.Int
			} `graphql:"pullRequests"`

			MergedPRs struct {
				TotalCount githubv4.Int
			} `graphql:"mergedPRs: pullRequests(states: MERGED)"`

			Issues struct {
				TotalCount githubv4.Int
			} `graphql:"issues"`

			// ContributionsCollection is a rolling 12-month window
			// ending now (the "last year" Activity section metric).
			ContributionsCollection struct {
				TotalCommitContributions                githubv4.Int
				TotalRepositoriesWithContributedCommits githubv4.Int
			}

			Organizations struct {
				Nodes []struct {
					Login githubv4.String
					Name  githubv4.String
				}
			} `graphql:"organizations(first: 20)"`

			Repositories struct {
				TotalCount githubv4.Int
				Nodes      []struct {
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
	}

	if err := c.gql.Query(ctx, &q, nil); err != nil {
		return nil, err
	}

	v := q.Viewer

	stats := &Stats{
		Login:                    string(v.Login),
		Name:                     string(v.Name),
		Bio:                      string(v.Bio),
		Company:                  string(v.Company),
		Location:                 string(v.Location),
		Pronouns:                 string(v.Pronouns),
		WebsiteURL:               string(v.WebsiteURL),
		TwitterUsername:          string(v.TwitterUsername),
		AvatarURL:                string(v.AvatarURL),
		CreatedAt:                v.CreatedAt.Time,
		Followers:                int(v.Followers.TotalCount),
		Following:                int(v.Following.TotalCount),
		PRsTotal:                 int(v.PullRequests.TotalCount),
		PRsMerged:                int(v.MergedPRs.TotalCount),
		IssuesAuthored:           int(v.Issues.TotalCount),
		CommitsLastYear:          int(v.ContributionsCollection.TotalCommitContributions),
		ContributedReposLastYear: int(v.ContributionsCollection.TotalRepositoriesWithContributedCommits),
		PublicRepos:              int(v.Repositories.TotalCount),
		Authenticated:            c.authenticated,
	}

	// Social accounts
	for _, sa := range v.SocialAccounts.Nodes {
		stats.SocialAccounts = append(stats.SocialAccounts, SocialAccount{
			Provider:    string(sa.Provider),
			URL:         string(sa.URL),
			DisplayName: string(sa.DisplayName),
		})
	}

	// Organizations
	for _, o := range v.Organizations.Nodes {
		stats.Organizations = append(stats.Organizations, Organization{
			Login: string(o.Login),
			Name:  string(o.Name),
		})
	}

	// Aggregate per-repo counters and languages
	langMap := map[string]*Language{}
	for _, r := range v.Repositories.Nodes {
		stats.TotalStars += int(r.StargazerCount)
		stats.ForksReceived += int(r.ForkCount)
		stats.OpenIssues += int(r.Issues.TotalCount)
		stats.OpenPRs += int(r.PullRequests.TotalCount)

		for _, e := range r.Languages.Edges {
			name := string(e.Node.Name)
			if l, ok := langMap[name]; ok {
				l.Bytes += int(e.Size)
			} else {
				langMap[name] = &Language{
					Name:  name,
					Color: string(e.Node.Color),
					Bytes: int(e.Size),
				}
			}
		}
	}

	// Flatten language map to a slice sorted desc by byte count. Cap
	// to the top 6 so the TUI bar stays readable; "others" aren't
	// rendered but their bytes aren't useful at a glance.
	for _, l := range langMap {
		stats.Languages = append(stats.Languages, *l)
	}
	sort.Slice(stats.Languages, func(i, j int) bool {
		return stats.Languages[i].Bytes > stats.Languages[j].Bytes
	})
	if len(stats.Languages) > 6 {
		stats.Languages = stats.Languages[:6]
	}

	return stats, nil
}
