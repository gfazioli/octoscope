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
type Client struct {
	gql           *githubv4.Client
	authenticated bool
	login         string
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
	PrimaryLanguage string
	LanguageColor   string
	Stars           int
	Forks           int
	OpenIssues      int
	OpenPRs         int
	PushedAt        time.Time
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
}

// New builds a client, preferring an authenticated one when a token is
// available. An unauthenticated client still works but is rate-limited
// to 60 requests/hour by GitHub.
//
// `login` is the optional GitHub username to fetch. Pass "" for the
// authenticated viewer; pass any login to show that user's public
// profile (works with or without a token, though without one the
// dashboard will burn through the 60/h limit quickly).
func New(login string) (*Client, error) {
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
	return &Client{
		gql:           githubv4.NewClient(httpClient),
		authenticated: authed,
		login:         login,
	}, nil
}

// userFields is the full set of GraphQL fields we pull for a user.
// Shared between the `viewer { … }` and `user(login: $login) { … }`
// queries so they stay in lockstep — one struct, one source of truth.
type userFields struct {
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

	Repositories struct {
		TotalCount githubv4.Int
		Nodes      []struct {
			Name            githubv4.String
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

// FetchStats runs a single GraphQL query that pulls everything the TUI
// needs — profile, social, activity, operational and network — then
// aggregates per-repo totals client-side (stars, forks, languages).
//
// Routes to the `viewer` query when Client.login is empty, or to
// `user(login: $login)` otherwise. Both return the same field shape.
//
// Limitation: repository pagination is capped at 100. Users with more
// repos will under-count aggregated totals (stars, forks, open issues,
// open PRs, language bytes). Viewer-level counters (followers, PRs,
// issues) are unaffected.
func (c *Client) FetchStats(ctx context.Context) (*Stats, error) {
	var fields userFields
	var err error

	if c.login == "" {
		var q struct {
			Viewer userFields
		}
		err = c.gql.Query(ctx, &q, nil)
		fields = q.Viewer
	} else {
		var q struct {
			User userFields `graphql:"user(login: $login)"`
		}
		variables := map[string]interface{}{
			"login": githubv4.String(c.login),
		}
		err = c.gql.Query(ctx, &q, variables)
		fields = q.User
	}
	if err != nil {
		return nil, err
	}

	return c.extractStats(fields), nil
}

// extractStats flattens userFields into the UI-facing Stats struct,
// aggregating per-repo totals and deduping languages. Pure function
// aside from the client-level Authenticated/IsViewer flags.
func (c *Client) extractStats(f userFields) *Stats {
	stats := &Stats{
		Login:                    string(f.Login),
		Name:                     string(f.Name),
		Bio:                      string(f.Bio),
		Company:                  string(f.Company),
		Location:                 string(f.Location),
		Pronouns:                 string(f.Pronouns),
		WebsiteURL:               string(f.WebsiteURL),
		TwitterUsername:          string(f.TwitterUsername),
		AvatarURL:                string(f.AvatarURL),
		CreatedAt:                f.CreatedAt.Time,
		Followers:                int(f.Followers.TotalCount),
		Following:                int(f.Following.TotalCount),
		PRsTotal:                 int(f.PullRequests.TotalCount),
		PRsMerged:                int(f.MergedPRs.TotalCount),
		IssuesAuthored:           int(f.Issues.TotalCount),
		CommitsLastYear:          int(f.ContributionsCollection.TotalCommitContributions),
		ContributedReposLastYear: int(f.ContributionsCollection.TotalRepositoriesWithContributedCommits),
		PublicRepos:              int(f.Repositories.TotalCount),
		Authenticated:            c.authenticated,
		IsViewer:                 c.login == "",
	}

	for _, sa := range f.SocialAccounts.Nodes {
		stats.SocialAccounts = append(stats.SocialAccounts, SocialAccount{
			Provider:    string(sa.Provider),
			URL:         string(sa.URL),
			DisplayName: string(sa.DisplayName),
		})
	}

	for _, o := range f.Organizations.Nodes {
		stats.Organizations = append(stats.Organizations, Organization{
			Login: string(o.Login),
			Name:  string(o.Name),
		})
	}

	for _, w := range f.ContributionsCollection.ContributionCalendar.Weeks {
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
	for _, r := range f.Repositories.Nodes {
		stats.TotalStars += int(r.StargazerCount)
		stats.ForksReceived += int(r.ForkCount)
		stats.OpenIssues += int(r.Issues.TotalCount)
		stats.OpenPRs += int(r.PullRequests.TotalCount)

		stats.Repositories = append(stats.Repositories, Repo{
			Name:            string(r.Name),
			PrimaryLanguage: string(r.PrimaryLanguage.Name),
			LanguageColor:   string(r.PrimaryLanguage.Color),
			Stars:           int(r.StargazerCount),
			Forks:           int(r.ForkCount),
			OpenIssues:      int(r.Issues.TotalCount),
			OpenPRs:         int(r.PullRequests.TotalCount),
			PushedAt:        r.PushedAt.Time,
		})

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

	return stats
}
