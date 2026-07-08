// Package report renders a fetched dashboard snapshot to non-interactive
// output — a machine-readable JSON document (--json) and a human-readable
// text summary (--plain). It exists so the JSON wire format is a stable
// contract defined in exactly one place, decoupled from the internal
// github.Stats struct: Stats can be refactored freely without breaking a
// script that parses octoscope's output.
package report

import (
	"encoding/json"
	"fmt"
	"io"
	"math"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/gfazioli/octoscope/internal/github"
)

// SchemaVersion is the version of the --json output contract, surfaced as
// the top-level "schema_version" field. Bump it only on a *breaking*
// change to the document shape (a renamed / removed / retyped field);
// purely additive fields (new keys) do not require a bump, so consumers
// keying on schema_version keep working across additive releases.
const SchemaVersion = 1

// plainListCap bounds how many rows each list prints in --plain mode so a
// busy account doesn't flood a status-line or terminal. --json is
// uncapped (subject only to the server-side list caps already applied by
// the fetch).
const plainListCap = 15

// Report is the versioned, JSON-tagged snapshot emitted by --json. It is
// the public wire contract — see SchemaVersion. Every list field is
// always a (possibly empty) array, never null, so consumers can iterate
// unconditionally.
type Report struct {
	SchemaVersion    int           `json:"schema_version"`
	OctoscopeVersion string        `json:"octoscope_version"`
	GeneratedAt      time.Time     `json:"generated_at"`
	Authenticated    bool          `json:"authenticated"`
	IsViewer         bool          `json:"is_viewer"`
	PublicOnly       bool          `json:"public_only"`
	Profile          Profile       `json:"profile"`
	Social           Social        `json:"social"`
	Activity         Activity      `json:"activity"`
	Operational      Operational   `json:"operational"`
	Languages        []Language    `json:"languages"`
	Repositories     []Repo        `json:"repositories"`
	OpenPullRequests []PullRequest `json:"open_pull_requests"`
	OpenIssuesList   []Issue       `json:"open_issues_list"`
	ReviewRequests   []PullRequest `json:"review_requests"`
	Organizations    []Org         `json:"organizations"`
	WatchedRepos     []Repo        `json:"watched_repos"`
	WatchedSkipped   []string      `json:"watched_skipped"`
	RateLimit        *RateLimit    `json:"rate_limit,omitempty"`
}

// Profile is the identity block: who the account belongs to.
type Profile struct {
	Login     string    `json:"login"`
	Name      string    `json:"name"`
	Bio       string    `json:"bio"`
	Company   string    `json:"company"`
	Location  string    `json:"location"`
	CreatedAt time.Time `json:"created_at"`
}

// Social holds the follower / star counters.
type Social struct {
	Followers           int `json:"followers"`
	Following           int `json:"following"`
	TotalStars          int `json:"total_stars"`
	TotalStarsWithForks int `json:"total_stars_with_forks"`
}

// Activity holds the lifetime + last-year contribution counters.
type Activity struct {
	PRsTotal                 int `json:"prs_total"`
	PRsMerged                int `json:"prs_merged"`
	IssuesAuthored           int `json:"issues_authored"`
	OpenPRsAuthored          int `json:"open_prs_authored"`
	CommitsLastYear          int `json:"commits_last_year"`
	ContributedReposLastYear int `json:"contributed_repos_last_year"`
}

// Operational holds the current-state counts across owned non-fork repos.
type Operational struct {
	PublicRepos   int `json:"public_repos"`
	ForksReceived int `json:"forks_received"`
	OpenIssues    int `json:"open_issues"`
	OpenPRs       int `json:"open_prs"`
}

// Language is one programming language with its share of the top-language
// byte total. Percent is rounded to one decimal and computed over the
// languages present in this list (matching the Overview bar).
type Language struct {
	Name    string  `json:"name"`
	Bytes   int     `json:"bytes"`
	Percent float64 `json:"percent"`
}

// Repo is one repository row (owned or watched).
type Repo struct {
	Name          string       `json:"name"`
	URL           string       `json:"url"`
	Language      string       `json:"language"`
	Stars         int          `json:"stars"`
	Forks         int          `json:"forks"`
	OpenIssues    int          `json:"open_issues"`
	OpenPRs       int          `json:"open_prs"`
	PushedAt      time.Time    `json:"pushed_at"`
	Private       bool         `json:"private"`
	CIState       string       `json:"ci_state,omitempty"`
	LatestRelease *ReleaseInfo `json:"latest_release,omitempty"`
}

// ReleaseInfo is the most recent release on a repo. Nil when the repo has
// cut none.
type ReleaseInfo struct {
	Tag         string    `json:"tag"`
	PublishedAt time.Time `json:"published_at"`
}

// PullRequest is one open PR — either authored by the user
// (OpenPullRequests) or awaiting the user's review (ReviewRequests).
type PullRequest struct {
	Number    int       `json:"number"`
	Title     string    `json:"title"`
	Repo      string    `json:"repo"`
	URL       string    `json:"url"`
	Draft     bool      `json:"draft"`
	Mergeable string    `json:"mergeable"`
	Author    string    `json:"author,omitempty"`
	UpdatedAt time.Time `json:"updated_at"`
	Private   bool      `json:"private"`
}

// Issue is one open issue authored by the user.
type Issue struct {
	Number    int       `json:"number"`
	Title     string    `json:"title"`
	Repo      string    `json:"repo"`
	URL       string    `json:"url"`
	UpdatedAt time.Time `json:"updated_at"`
	Private   bool      `json:"private"`
}

// Org is one organization the user belongs to.
type Org struct {
	Login string `json:"login"`
	Name  string `json:"name"`
}

// RateLimit is the GraphQL rate-limit envelope from the fetch.
type RateLimit struct {
	Cost      int       `json:"cost"`
	Limit     int       `json:"limit"`
	Remaining int       `json:"remaining"`
	ResetAt   time.Time `json:"reset_at"`
}

// FromStats maps a fetched github.Stats into the versioned Report DTO.
// octoscopeVersion is the running binary's version, generatedAt stamps
// the snapshot, and publicOnly records whether the caller had already
// stripped private entries via Stats.Public() — it is metadata only, not
// a filter FromStats applies. Strings arrive already sanitized at the
// github extractor boundary, so no re-sanitization happens here.
func FromStats(s *github.Stats, octoscopeVersion string, generatedAt time.Time, publicOnly bool) Report {
	r := Report{
		SchemaVersion:    SchemaVersion,
		OctoscopeVersion: octoscopeVersion,
		GeneratedAt:      generatedAt,
		Authenticated:    s.Authenticated,
		IsViewer:         s.IsViewer,
		PublicOnly:       publicOnly,
		Profile: Profile{
			Login:     s.Login,
			Name:      s.Name,
			Bio:       s.Bio,
			Company:   s.Company,
			Location:  s.Location,
			CreatedAt: s.CreatedAt,
		},
		Social: Social{
			Followers:           s.Followers,
			Following:           s.Following,
			TotalStars:          s.TotalStars,
			TotalStarsWithForks: s.TotalStarsWithForks,
		},
		Activity: Activity{
			PRsTotal:                 s.PRsTotal,
			PRsMerged:                s.PRsMerged,
			IssuesAuthored:           s.IssuesAuthored,
			OpenPRsAuthored:          s.OpenPRsAuthored,
			CommitsLastYear:          s.CommitsLastYear,
			ContributedReposLastYear: s.ContributedReposLastYear,
		},
		Operational: Operational{
			PublicRepos:   s.PublicRepos,
			ForksReceived: s.ForksReceived,
			OpenIssues:    s.OpenIssues,
			OpenPRs:       s.OpenPRs,
		},
		Languages:        toLanguages(s.Languages),
		Repositories:     toRepos(s.Repositories),
		OpenPullRequests: toPRs(s.OpenPullRequests),
		OpenIssuesList:   toIssues(s.OpenIssuesList),
		ReviewRequests:   toPRs(s.ReviewRequests),
		Organizations:    toOrgs(s.Organizations),
		WatchedRepos:     toRepos(s.WatchedRepos),
		WatchedSkipped:   nonNilStrings(s.WatchedSkipped),
	}
	if s.RateLimit != nil {
		r.RateLimit = &RateLimit{
			Cost:      s.RateLimit.Cost,
			Limit:     s.RateLimit.Limit,
			Remaining: s.RateLimit.Remaining,
			ResetAt:   s.RateLimit.ResetAt,
		}
	}
	return r
}

func toLanguages(in []github.Language) []Language {
	out := make([]Language, 0, len(in))
	var total int
	for _, l := range in {
		total += l.Bytes
	}
	for _, l := range in {
		var pct float64
		if total > 0 {
			pct = math.Round(float64(l.Bytes)/float64(total)*1000) / 10
		}
		out = append(out, Language{Name: l.Name, Bytes: l.Bytes, Percent: pct})
	}
	return out
}

func toRepos(in []github.Repo) []Repo {
	out := make([]Repo, 0, len(in))
	for _, r := range in {
		repo := Repo{
			Name:       r.Name,
			URL:        r.URL,
			Language:   r.PrimaryLanguage,
			Stars:      r.Stars,
			Forks:      r.Forks,
			OpenIssues: r.OpenIssues,
			OpenPRs:    r.OpenPRs,
			PushedAt:   r.PushedAt,
			Private:    r.IsPrivate,
			CIState:    r.CIState,
		}
		if r.LatestReleaseTag != "" {
			repo.LatestRelease = &ReleaseInfo{
				Tag:         r.LatestReleaseTag,
				PublishedAt: r.LatestReleasePublishedAt,
			}
		}
		out = append(out, repo)
	}
	return out
}

func toPRs(in []github.PullRequest) []PullRequest {
	out := make([]PullRequest, 0, len(in))
	for _, p := range in {
		out = append(out, PullRequest{
			Number:    p.Number,
			Title:     p.Title,
			Repo:      p.Repo,
			URL:       p.URL,
			Draft:     p.IsDraft,
			Mergeable: p.Mergeable,
			Author:    p.AuthorLogin,
			UpdatedAt: p.UpdatedAt,
			Private:   p.IsPrivate,
		})
	}
	return out
}

func toIssues(in []github.Issue) []Issue {
	out := make([]Issue, 0, len(in))
	for _, i := range in {
		out = append(out, Issue{
			Number:    i.Number,
			Title:     i.Title,
			Repo:      i.Repo,
			URL:       i.URL,
			UpdatedAt: i.UpdatedAt,
			Private:   i.IsPrivate,
		})
	}
	return out
}

func toOrgs(in []github.Organization) []Org {
	out := make([]Org, 0, len(in))
	for _, o := range in {
		out = append(out, Org{Login: o.Login, Name: o.Name})
	}
	return out
}

// nonNilStrings guarantees a non-nil slice so the JSON field marshals as
// [] rather than null.
func nonNilStrings(in []string) []string {
	if in == nil {
		return []string{}
	}
	return in
}

// RenderJSON writes the report as indented JSON followed by a newline.
// HTML escaping is disabled so URLs and titles read cleanly.
func RenderJSON(w io.Writer, r Report) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	return enc.Encode(r)
}

// RenderPlain writes a human-readable, colourless summary: profile
// counters followed by the key lists. Unlike RenderJSON its exact layout
// is not a contract and may change between releases.
func RenderPlain(w io.Writer, r Report) error {
	var b strings.Builder

	name := r.Profile.Login
	if r.Profile.Name != "" {
		name = fmt.Sprintf("%s (%s)", r.Profile.Login, r.Profile.Name)
	}
	fmt.Fprintf(&b, "octoscope %s — %s\n", r.OctoscopeVersion, name)

	auth := "unauthenticated"
	if r.Authenticated {
		auth = "authenticated"
	}
	if r.PublicOnly {
		auth += " · public-only"
	}
	fmt.Fprintf(&b, "generated %s · %s\n\n", r.GeneratedAt.Format(time.RFC3339), auth)

	fmt.Fprintf(&b, "Social       followers %d · following %d · stars %d",
		r.Social.Followers, r.Social.Following, r.Social.TotalStars)
	if r.Social.TotalStarsWithForks != r.Social.TotalStars {
		fmt.Fprintf(&b, " (with forks %d)", r.Social.TotalStarsWithForks)
	}
	b.WriteByte('\n')
	fmt.Fprintf(&b, "Activity     PRs %d (merged %d) · issues %d · open PRs %d · commits/yr %d\n",
		r.Activity.PRsTotal, r.Activity.PRsMerged, r.Activity.IssuesAuthored,
		r.Activity.OpenPRsAuthored, r.Activity.CommitsLastYear)
	fmt.Fprintf(&b, "Operational  repos %d · forks %d · open issues %d · open PRs %d\n",
		r.Operational.PublicRepos, r.Operational.ForksReceived,
		r.Operational.OpenIssues, r.Operational.OpenPRs)

	if len(r.Languages) > 0 {
		parts := make([]string, 0, len(r.Languages))
		for _, l := range r.Languages {
			parts = append(parts, fmt.Sprintf("%s %.1f%%", l.Name, l.Percent))
		}
		fmt.Fprintf(&b, "Languages    %s\n", strings.Join(parts, " · "))
	}

	writeRepoList(&b, "Repositories", r.Repositories)
	writePRList(&b, "Open pull requests", r.OpenPullRequests)
	writeIssueList(&b, "Open issues", r.OpenIssuesList)
	writePRList(&b, "Review requests", r.ReviewRequests)
	writeRepoList(&b, "Watched", r.WatchedRepos)
	if len(r.WatchedSkipped) > 0 {
		fmt.Fprintf(&b, "\n%d watched %s skipped: %s\n",
			len(r.WatchedSkipped), plural(len(r.WatchedSkipped), "entry", "entries"),
			strings.Join(r.WatchedSkipped, ", "))
	}

	_, err := io.WriteString(w, b.String())
	return err
}

func writeRepoList(b *strings.Builder, title string, repos []Repo) {
	if len(repos) == 0 {
		return
	}
	fmt.Fprintf(b, "\n%s (%d)\n", title, len(repos))
	tw := tabwriter.NewWriter(b, 0, 0, 2, ' ', 0)
	for _, r := range capRepos(repos) {
		lang := r.Language
		if lang == "" {
			lang = "-"
		}
		fmt.Fprintf(tw, "  ★ %d\t⑂ %d\t%s\t%s\n", r.Stars, r.Forks, lang, r.Name)
	}
	tw.Flush()
	writeMore(b, len(repos))
}

func writePRList(b *strings.Builder, title string, prs []PullRequest) {
	if len(prs) == 0 {
		return
	}
	fmt.Fprintf(b, "\n%s (%d)\n", title, len(prs))
	tw := tabwriter.NewWriter(b, 0, 0, 2, ' ', 0)
	for _, p := range capPRs(prs) {
		meta := ""
		switch {
		case p.Draft:
			meta = "  [draft]"
		case p.Mergeable == "CONFLICTING":
			meta = "  [conflicts]"
		}
		if p.Author != "" {
			meta += "  by " + p.Author
		}
		fmt.Fprintf(tw, "  #%d\t%s\t%s%s\n", p.Number, p.Title, p.Repo, meta)
	}
	tw.Flush()
	writeMore(b, len(prs))
}

func writeIssueList(b *strings.Builder, title string, issues []Issue) {
	if len(issues) == 0 {
		return
	}
	fmt.Fprintf(b, "\n%s (%d)\n", title, len(issues))
	tw := tabwriter.NewWriter(b, 0, 0, 2, ' ', 0)
	for _, i := range capIssues(issues) {
		fmt.Fprintf(tw, "  #%d\t%s\t%s\n", i.Number, i.Title, i.Repo)
	}
	tw.Flush()
	writeMore(b, len(issues))
}

// writeMore appends the "…and N more" line when a list was capped.
func writeMore(b *strings.Builder, total int) {
	if total > plainListCap {
		fmt.Fprintf(b, "  …and %d more\n", total-plainListCap)
	}
}

func capRepos(in []Repo) []Repo {
	if len(in) > plainListCap {
		return in[:plainListCap]
	}
	return in
}

func capPRs(in []PullRequest) []PullRequest {
	if len(in) > plainListCap {
		return in[:plainListCap]
	}
	return in
}

func capIssues(in []Issue) []Issue {
	if len(in) > plainListCap {
		return in[:plainListCap]
	}
	return in
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}
