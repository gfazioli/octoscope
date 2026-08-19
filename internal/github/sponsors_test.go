package github

import (
	"os"
	"strings"
	"testing"

	"github.com/shurcooL/githubv4"
)

// sponsorNode is the anonymous node type of sponsorConnection, named here
// so the helpers below can build one.
type sponsorNode = struct {
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

// node builds one node with BOTH fragments populated. That is not a
// contrived shape: shurcooL/githubv4 resolves shared field names across
// inline fragments, so a User node really can come back carrying values in
// the Organization struct. Every test below therefore proves the extractor
// reads __typename rather than "whichever half looks filled in".
func node(typename, userLogin, userName, orgLogin, orgName string) sponsorNode {
	var n sponsorNode
	n.Typename = typename
	n.User.Login = githubv4.String(userLogin)
	n.User.Name = githubv4.String(userName)
	n.User.URL = githubv4.String("https://github.com/" + userLogin)
	n.Organization.Login = githubv4.String(orgLogin)
	n.Organization.Name = githubv4.String(orgName)
	n.Organization.URL = githubv4.String("https://github.com/" + orgLogin)
	return n
}

func conn(total int, nodes ...sponsorNode) sponsorConnection {
	var c sponsorConnection
	c.TotalCount = githubv4.Int(total)
	c.Nodes = append(c.Nodes, nodes...)
	return c
}

func TestExtractSponsorsDiscriminatesOnTypename(t *testing.T) {
	got := extractSponsors(conn(2,
		node("User", "kastov", "Yury Kastov", "acme", "Acme Inc"),
		node("Organization", "someone", "Some One", "charmbracelet", "Charm"),
	))
	if len(got) != 2 {
		t.Fatalf("got %d sponsors, want 2", len(got))
	}
	if got[0].Login != "kastov" || got[0].IsOrg {
		t.Errorf("first = %+v, want the User fragment", got[0])
	}
	if got[1].Login != "charmbracelet" || !got[1].IsOrg {
		t.Errorf("second = %+v, want the Organization fragment", got[1])
	}
	// Both fragments were populated on both nodes, so reading the wrong one
	// yields a plausible-looking sponsor with the wrong identity — which is
	// why this is asserted on the value and not on emptiness.
	if got[0].Login == "acme" || got[1].Login == "someone" {
		t.Error("the extractor read the fragment __typename did not name")
	}
}

// GitHub adds union members. A row with no login is not a sponsor anyone
// can read, so it is dropped rather than drawn blank.
func TestExtractSponsorsSkipsWhatItCannotName(t *testing.T) {
	got := extractSponsors(conn(3,
		node("Bot", "", "", "", ""),
		node("User", "", "No Login", "", ""),
		node("User", "real", "Real Person", "", ""),
	))
	if len(got) != 1 || got[0].Login != "real" {
		t.Fatalf("got %+v, want only the nameable one", got)
	}
}

func TestExtractSponsorsEmpty(t *testing.T) {
	if got := extractSponsors(conn(0)); got != nil {
		t.Errorf("extractSponsors(empty) = %v, want nil", got)
	}
}

// A sponsor's display name is attacker-controlled in the same way every
// other GitHub-sourced string is: whoever owns the account picks it, and
// it is about to be written to a terminal.
func TestExtractSponsorsSanitizes(t *testing.T) {
	got := extractSponsors(conn(1, node("User", "someone", "real\x1b]0;pwned\x07name", "", "")))
	if len(got) != 1 {
		t.Fatalf("got %d", len(got))
	}
	if strings.ContainsAny(got[0].Name, "\x1b\x07") {
		t.Errorf("Name kept a terminal escape: %q", got[0].Name)
	}
	if strings.Contains(got[0].Name, "pwned") {
		t.Errorf("Name kept the OSC payload: %q", got[0].Name)
	}
}

func TestSponsorLabelPrefersTheNameThenTheLogin(t *testing.T) {
	for _, tc := range []struct{ name, login, want string }{
		{"Yury Kastov", "kastov", "Yury Kastov"},
		{"", "kastov", "kastov"},
		{"   ", "kastov", "kastov"},
	} {
		if got := (Sponsor{Name: tc.name, Login: tc.login}).Label(); got != tc.want {
			t.Errorf("Label(name=%q login=%q) = %q, want %q", tc.name, tc.login, got, tc.want)
		}
	}
}

// The constant and the two struct tags are three copies of one number, and
// a tag cannot interpolate a constant, so only a test keeps them in step.
func TestSponsorsPageSizeMatchesQuery(t *testing.T) {
	if sponsorsPageSize != 20 {
		t.Fatalf("sponsorsPageSize is %d — update this test and both struct tags",
			sponsorsPageSize)
	}
	src, err := os.ReadFile("client.go")
	if err != nil {
		t.Fatalf("read client.go: %v", err)
	}
	for _, q := range []string{"sponsors(first: 20)", "sponsoring(first: 20)"} {
		if !strings.Contains(string(src), q) {
			t.Errorf("client.go no longer queries %q — it has drifted from sponsorsPageSize", q)
		}
	}
}

// Screenshot mode drops the whole block rather than filtering it, because
// there is nothing to filter on: `privacyLevel` needs a scope octoscope
// does not ask for, so every name here is of unknown visibility.
//
// There is deliberately no IsPrivate field on Sponsor, which is exactly why
// TestPublicStripsEveryPrivateList cannot reach this and this test exists.
func TestPublicDropsEverySponsorshipField(t *testing.T) {
	s := &Stats{
		Sponsors:                   []Sponsor{{Login: "kastov"}},
		SponsorsTotal:              3,
		Sponsoring:                 []Sponsor{{Login: "fisker"}},
		SponsoringTotal:            12,
		HasSponsorsListing:         true,
		MonthlySponsorsIncomeCents: 2500,
	}
	got := s.Public()

	if got.Sponsors != nil || got.Sponsoring != nil {
		t.Errorf("names survived: %+v / %+v", got.Sponsors, got.Sponsoring)
	}
	// The counts go too: "3 sponsors" printed above a list of one is its
	// own disclosure — the same second-order leak the gists total avoids.
	if got.SponsorsTotal != 0 || got.SponsoringTotal != 0 {
		t.Errorf("counts survived: %d / %d", got.SponsorsTotal, got.SponsoringTotal)
	}
	if got.MonthlySponsorsIncomeCents != 0 {
		t.Errorf("income survived: %d", got.MonthlySponsorsIncomeCents)
	}
	// The listing flag goes as well. Keeping it is not a leak — the listing
	// is on the public profile page — but it drops renderSponsors onto its
	// "nobody yet" line, so an account *with* sponsors would have screenshot
	// mode assert it has none. Hiding and none must not wear one appearance.
	if got.HasSponsorsListing {
		t.Error("HasSponsorsListing survived; the section then claims 'nobody yet' " +
			"for an account that has sponsors")
	}
}

// GitHub answers the income field with 0 for any account that is not the
// caller's, so reading it outside viewer mode would print someone else's
// income as $0.00 rather than declining to say.
func TestIncomeIsReadOnlyForTheViewer(t *testing.T) {
	var p profileFields
	p.Login = "octocat"
	p.MonthlySponsorsIncomeCents = 2500

	viewer := (&Client{login: ""}).extractStats(p, repoFields{}, repoCIFields{})
	if viewer.MonthlySponsorsIncomeCents != 2500 {
		t.Errorf("viewer income = %d, want 2500", viewer.MonthlySponsorsIncomeCents)
	}

	other := (&Client{login: "someone-else"}).extractStats(p, repoFields{}, repoCIFields{})
	if other.MonthlySponsorsIncomeCents != 0 {
		t.Errorf("income = %d for another account — GitHub answers 0 there, so any "+
			"non-zero value could only come from reading it where it means nothing",
			other.MonthlySponsorsIncomeCents)
	}
}
