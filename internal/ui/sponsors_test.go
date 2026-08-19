package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/gfazioli/octoscope/internal/github"
)

func sp(login, name string, org bool) github.Sponsor {
	return github.Sponsor{Login: login, Name: name, IsOrg: org, URL: "https://github.com/" + login}
}

// Section absence is the default UX here, same as pinned / watched /
// review-requests: an account with no sponsorship in either direction and
// no listing grows no permanently empty row.
func TestSponsorSectionAbsentWithoutAnySponsorship(t *testing.T) {
	_ = applyTheme("octoscope", "")
	if got := renderSponsors(&github.Stats{}, 120); got != "" {
		t.Errorf("rendered %q for an account with nothing to say", got)
	}
	if got := renderSponsors(nil, 120); got != "" {
		t.Errorf("rendered %q for nil stats", got)
	}
	if cards := sponsorCards(&github.Stats{}); len(cards) != 0 {
		t.Errorf("added %d cards for an account with no sponsorship", len(cards))
	}
}

// The cards are only useful if they are actually appended to the row —
// testing sponsorCards alone leaves the wiring unasserted, and removing
// that one line passed the whole suite until this existed.
func TestSponsorCardsReachTheSocialRow(t *testing.T) {
	m := newFeedRoutingModel(t)

	with := ansi.Strip(m.renderSocial(&github.Stats{
		Followers: 1, SponsorsTotal: 3, SponsoringTotal: 2,
	}, 200))
	if !strings.Contains(with, "Sponsors") {
		t.Errorf("the Sponsors card is missing from the Social row:\n%s", with)
	}
	if !strings.Contains(with, "Sponsoring") {
		t.Errorf("the Sponsoring card is missing from the Social row:\n%s", with)
	}

	without := ansi.Strip(m.renderSocial(&github.Stats{Followers: 1}, 200))
	if strings.Contains(without, "Sponsor") {
		t.Errorf("an account with no sponsorship grew a card:\n%s", without)
	}
}

// "Nobody sponsors this account" and "this account cannot be sponsored"
// are different statements, and only one of them is worth drawing.
func TestSponsorSectionSaysNobodyYetWhenTheListingIsOpen(t *testing.T) {
	_ = applyTheme("octoscope", "")
	got := ansi.Strip(renderSponsors(&github.Stats{HasSponsorsListing: true}, 120))
	if !strings.Contains(got, "nobody yet") {
		t.Errorf("an open listing with no sponsors rendered as %q", got)
	}
	// And the counterpart: no listing at all draws nothing rather than
	// claiming nobody has sponsored them.
	if got := renderSponsors(&github.Stats{HasSponsorsListing: false}, 120); got != "" {
		t.Errorf("an account with no listing rendered %q", got)
	}
}

func TestSponsorCardsAppearOnlyForTheSideThatHasAny(t *testing.T) {
	for _, tc := range []struct {
		name    string
		stats   github.Stats
		wantIDs []string
	}{
		{"neither", github.Stats{}, nil},
		{"received only", github.Stats{SponsorsTotal: 3}, []string{"sponsors"}},
		{"given only", github.Stats{SponsoringTotal: 2}, []string{"sponsoring"}},
		{"both", github.Stats{SponsorsTotal: 3, SponsoringTotal: 2}, []string{"sponsors", "sponsoring"}},
	} {
		var ids []string
		for _, c := range sponsorCards(&tc.stats) {
			ids = append(ids, c.id)
		}
		if strings.Join(ids, ",") != strings.Join(tc.wantIDs, ",") {
			t.Errorf("%s: cards %v, want %v", tc.name, ids, tc.wantIDs)
		}
	}
}

// The remainder is computed from the total, so a short list never claims
// to be truncated and a capped one never looks complete.
func TestSponsorLabelsStateTheRemainderOnlyWhenThereIsOne(t *testing.T) {
	list := []github.Sponsor{sp("a", "Ada", false), sp("b", "Bob", false)}

	full := strings.Join(sponsorLabels(list, 2), " | ")
	if strings.Contains(full, "more") {
		t.Errorf("a complete list claimed truncation: %q", full)
	}

	capped := strings.Join(sponsorLabels(list, 176), " | ")
	if !strings.Contains(capped, "and 174 more") {
		t.Errorf("a capped list did not say so: %q", capped)
	}
}

// An organisation in a list of people reads as somebody's display name
// unless it is marked.
func TestSponsorLabelsMarkOrganisations(t *testing.T) {
	got := sponsorLabels([]github.Sponsor{
		sp("ada", "Ada Lovelace", false),
		sp("acme", "Acme Inc", true),
	}, 2)
	if got[0] != "Ada Lovelace" {
		t.Errorf("person = %q, want the bare name", got[0])
	}
	if !strings.HasPrefix(got[1], "▣ ") || !strings.Contains(got[1], "Acme Inc") {
		t.Errorf("organisation = %q, want it marked", got[1])
	}
}

// Falls back to the login, because a sponsor with no display name is
// common and a blank row is not a row.
func TestSponsorLabelsFallBackToTheLogin(t *testing.T) {
	got := sponsorLabels([]github.Sponsor{sp("kastov", "", false)}, 1)
	if got[0] != "kastov" {
		t.Errorf("got %q, want the login", got[0])
	}
}

// GitHub answers the income field with 0 for any account that is not the
// caller's, so a zero must never reach the screen as "$0.00" — it means
// "not ours to say", and the two are indistinguishable once printed.
func TestIncomeIsDrawnOnlyWhenItIsOurs(t *testing.T) {
	_ = applyTheme("octoscope", "")

	other := ansi.Strip(renderSponsors(&github.Stats{
		Sponsors: []github.Sponsor{sp("ada", "Ada", false)}, SponsorsTotal: 1,
	}, 120))
	if strings.Contains(other, "$") || strings.Contains(other, "Income") {
		t.Errorf("income rendered for an account that reports none: %q", other)
	}

	ours := ansi.Strip(renderSponsors(&github.Stats{
		Sponsors: []github.Sponsor{sp("ada", "Ada", false)}, SponsorsTotal: 1,
		MonthlySponsorsIncomeCents: 2500,
	}, 120))
	if !strings.Contains(ours, "$25.00") {
		t.Errorf("our own income did not render: %q", ours)
	}
}

func TestFormatCents(t *testing.T) {
	for cents, want := range map[int]string{
		0:      "$0.00",
		5:      "$0.05",
		250:    "$2.50",
		2500:   "$25.00",
		123456: "$1234.56",
	} {
		if got := formatCents(cents); got != want {
			t.Errorf("formatCents(%d) = %q, want %q", cents, got, want)
		}
	}
}

// The user-visible half of "section absence is the default": an account
// with no sponsorship must not grow a bare "Sponsors" heading over nothing.
// Only renderOverviewTab can show that — renderSponsors returning "" is
// necessary and not sufficient, and the guard that drops the heading went
// unasserted until this existed.
func TestOverviewOmitsTheSponsorsHeadingWhenThereIsNothingToSay(t *testing.T) {
	m := newFeedRoutingModel(t)

	bare := ansi.Strip(m.renderOverviewTab(&github.Stats{Login: "octocat"}, 140))
	if strings.Contains(bare, "Sponsors") {
		t.Errorf("an account with no sponsorship grew a Sponsors section:\n%s", bare)
	}
	// Network still renders, so this is not "the whole tab collapsed".
	if !strings.Contains(bare, "Network") {
		t.Fatalf("precondition: the rest of the Overview should still render:\n%s", bare)
	}

	with := ansi.Strip(m.renderOverviewTab(&github.Stats{
		Login: "octocat", HasSponsorsListing: true,
	}, 140))
	if !strings.Contains(with, "Sponsors") {
		t.Errorf("an open listing should still earn the section:\n%s", with)
	}
}

// The whole point of screenshot mode here: the section must not survive
// it, because nothing in the data says which sponsors are private.
func TestOverviewDropsTheSponsorsSectionUnderPublicOnly(t *testing.T) {
	m := newFeedRoutingModel(t)
	s := &github.Stats{
		Login:                      "octocat",
		Sponsors:                   []github.Sponsor{sp("kastov", "Yury Kastov", false)},
		SponsorsTotal:              1,
		Sponsoring:                 []github.Sponsor{sp("fisker", "Fisker", false)},
		SponsoringTotal:            1,
		HasSponsorsListing:         true,
		MonthlySponsorsIncomeCents: 2500,
	}

	full := ansi.Strip(m.renderOverviewTab(s, 140))
	if !strings.Contains(full, "Yury Kastov") || !strings.Contains(full, "$25.00") {
		t.Fatalf("precondition: the section should render in normal mode:\n%s", full)
	}

	public := ansi.Strip(m.renderOverviewTab(s.Public(), 140))
	for _, leak := range []string{"Yury Kastov", "kastov", "Fisker", "fisker", "$25.00"} {
		if strings.Contains(public, leak) {
			t.Errorf("public-only leaked %q", leak)
		}
	}
	// And it must vanish rather than degrade. Falling back to the
	// open-listing line would have screenshot mode state "nobody yet" over
	// an account that has a sponsor — an omission turning into a false
	// claim, which is worse than what the mode exists to prevent.
	if strings.Contains(public, "nobody yet") {
		t.Errorf("public-only claims the account has no sponsors:\n%s", public)
	}
	if strings.Contains(public, "Sponsors") {
		t.Errorf("the Sponsors section survived public-only:\n%s", public)
	}
}

// Rendering at absurd sizes must not panic — the terminal can be anything.
func TestRenderSponsorsSurvivesExtremeGeometry(t *testing.T) {
	_ = applyTheme("octoscope", "")
	s := &github.Stats{SponsorsTotal: 200, HasSponsorsListing: true,
		MonthlySponsorsIncomeCents: 999999}
	for i := 0; i < 40; i++ {
		s.Sponsors = append(s.Sponsors, sp("someone-with-a-long-login", strings.Repeat("Name ", 8), i%3 == 0))
	}
	for _, w := range []int{0, 1, 12, 40, 120, 400} {
		_ = renderSponsors(s, w)
	}
}
