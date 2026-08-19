package github

// Sponsors — who funds this account, and who it funds (#72).
//
// The issue asked for the scope to be settled before anything was built,
// and settling it is what shaped this file. Measured against the live API
// on 2026-08-19 with the token octoscope already asks for
// (`admin:public_key, gist, read:org, repo, workflow`):
//
//   - `sponsors` and `sponsoring` answer fine. Both are unions of User and
//     Organization, so both are discriminated on `__typename`.
//   - **Everything interesting about a sponsorship needs `read:user`.**
//     `sponsorshipsAsMaintainer`'s `privacyLevel`, `createdAt`,
//     `isOneTimePayment` and `tier` all return INSUFFICIENT_SCOPES. So the
//     tab shows *who*, and cannot show *since when*, *how much* or
//     *public or private*. Asking users to widen a token so a dashboard
//     can print a tier name is not a trade this project makes.
//   - The money fields do answer, and only for the account itself:
//     `monthlyEstimatedSponsorsIncomeInCents` returned 2500 for the viewer
//     and **0** for another user. GitHub documents that behaviour for
//     `totalSponsorshipAmountAsSponsorInCents` — *"Only returns a value
//     when viewed by the user themselves"* — and the measurement says the
//     income field behaves the same way. Which makes 0 ambiguous with a
//     genuine zero, so it is only ever read in viewer mode.
//
// **One thing could not be verified, and the design assumes the worst.**
// Whether `sponsors` includes *private* sponsors is not answerable here:
// the schema description says only "List of sponsors for this user or
// organization", and on the account this was built against the public and
// the include-private counts are both 1, so the two are indistinguishable.
// Since `privacyLevel` is exactly the field the token cannot read, there is
// no way to tell one sponsor from another. Screenshot mode therefore drops
// the whole block rather than guessing — see Stats.Public.

import (
	"strings"

	"github.com/shurcooL/githubv4"
)

// Sponsor is one account on either side of a sponsorship.
//
// Deliberately thin: it holds what the query can actually answer for. No
// tier, no start date, no amount — see the file comment for why, and note
// that adding any of them means asking the user for a wider token.
type Sponsor struct {
	Login string
	Name  string
	URL   string
	// IsOrg separates the two members of the union. Kept as a bool rather
	// than the raw `__typename` because those are the only two GitHub
	// returns here, and a caller asking "is this an org" should not have
	// to know the schema's spelling.
	IsOrg bool
}

// Label is what the UI shows for a sponsor: the display name when GitHub
// has one, the login otherwise. Chosen here rather than in the renderer
// only because both sides of the section want the same rule; it invents
// nothing, which is the line that matters.
func (s Sponsor) Label() string {
	if n := strings.TrimSpace(s.Name); n != "" {
		return n
	}
	return s.Login
}

// sponsorsPageSize bounds each connection. Twenty is well past what any
// dashboard section can draw — the counts come back alongside, so a busy
// account renders "and 156 more" instead of a list that silently stops.
const sponsorsPageSize = 20

// SponsorsPageSize is the cap the UI discloses, exported so the renderer
// does not carry a second copy of the number.
const SponsorsPageSize = sponsorsPageSize

// sponsorConnection is the shared shape of both connections. Both are
// unions of User and Organization, and both are discriminated on
// `__typename` rather than on which fragment came back populated —
// shurcooL/githubv4 resolves shared field names across inline fragments,
// so "which one looks filled in" is not a discriminator.
type sponsorConnection struct {
	TotalCount githubv4.Int
	Nodes      []struct {
		Typename string `graphql:"__typename"`
		User     struct {
			Login githubv4.String
			Name  githubv4.String
			URL   githubv4.String `graphql:"url"`
		} `graphql:"... on User"`
		Organization struct {
			Login githubv4.String
			Name  githubv4.String
			URL   githubv4.String `graphql:"url"`
		} `graphql:"... on Organization"`
	}
}

// extractSponsors maps one connection, cleaning every GitHub-sourced
// string at this boundary like the rest of the package.
func extractSponsors(c sponsorConnection) []Sponsor {
	if len(c.Nodes) == 0 {
		return nil
	}
	out := make([]Sponsor, 0, len(c.Nodes))
	for _, n := range c.Nodes {
		var s Sponsor
		switch n.Typename {
		case "Organization":
			s = Sponsor{
				Login: Sanitize(string(n.Organization.Login)),
				Name:  Sanitize(string(n.Organization.Name)),
				URL:   Sanitize(string(n.Organization.URL)),
				IsOrg: true,
			}
		case "User":
			s = Sponsor{
				Login: Sanitize(string(n.User.Login)),
				Name:  Sanitize(string(n.User.Name)),
				URL:   Sanitize(string(n.User.URL)),
			}
		default:
			// A type GitHub adds later. Skipped rather than guessed at:
			// a row with no login is not a sponsor anyone can read.
			continue
		}
		if s.Login == "" {
			continue
		}
		out = append(out, s)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
