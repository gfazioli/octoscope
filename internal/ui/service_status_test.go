package ui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/gfazioli/octoscope/internal/github"
)

func newStatusTestModel(t *testing.T) Model {
	t.Helper()
	t.Setenv("GITHUB_TOKEN", "test-token-not-used")
	_ = applyTheme("octoscope", "")
	client, err := github.New("octocat", github.Options{})
	if err != nil {
		t.Fatalf("github.New: %v", err)
	}
	// CheckServiceStatus mirrors the config default (true) so these
	// tests exercise the shipped configuration rather than a session
	// that has opted out.
	m := NewModel(client, "test", Options{CheckServiceStatus: true})
	m.stats = &github.Stats{Login: "octocat"}
	m.width, m.height = 150, 45
	return m
}

func impairedStatus() *github.ServiceStatus {
	return &github.ServiceStatus{
		Indicator:   "major",
		Description: "Partial System Outage",
		Affected: []github.ServiceComponent{
			{Name: "Pull Requests", Affects: "the PRs tab", State: github.ServicePartialOutage},
			{Name: "API Requests", Affects: "everything octoscope shows", State: github.ServiceDegraded},
		},
		Incidents: []github.ServiceIncident{
			{Name: "Incident with Actions and Pull Requests", Status: "investigating",
				URL: "https://stspg.io/abc123"},
		},
		FetchedAt: time.Now(),
	}
}

func healthyStatus() *github.ServiceStatus {
	return &github.ServiceStatus{Indicator: "none", Description: "All Systems Operational",
		FetchedAt: time.Now()}
}

// Silent while green is the contract, and it has to hold for the two
// shapes a caller actually holds: nothing fetched yet, and a fetch that
// came back clean.
func TestServiceStatusRendersNothingWhileHealthy(t *testing.T) {
	for name, st := range map[string]*github.ServiceStatus{
		"never fetched":  nil,
		"fetched, clean": healthyStatus(),
	} {
		t.Run(name, func(t *testing.T) {
			if got := renderServiceStatusLines(st, 100); got != "" {
				t.Errorf("dashboard rendered %q, want nothing", got)
			}
			if got := serviceStatusDetail(st); got != "" {
				t.Errorf("error screen appended %q, want nothing", got)
			}
		})
	}
}

func TestServiceStatusBannerNamesTheComponentsAndTheIncident(t *testing.T) {
	out := ansi.Strip(renderServiceStatusLines(impairedStatus(), 100))
	for _, want := range []string{
		"GitHub reports partially down",
		"Pull Requests", "API Requests",
		"Incident with Actions and Pull Requests",
		"investigating", "stspg.io/abc123",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("banner is missing %q:\n%s", want, out)
		}
	}
}

// The error screen is the half that does the real work: it has to say
// the problem is not octoscope, and say what the user would notice.
func TestServiceStatusDetailBlamesGitHubExplicitly(t *testing.T) {
	got := serviceStatusDetail(impairedStatus())
	for _, want := range []string{
		"Very likely not octoscope",
		"Pull Requests and API Requests partially down",
		"the PRs tab and everything octoscope shows",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("detail is missing %q:\n%s", want, got)
		}
	}
}

// Two impaired components that break the same thing must not say it
// twice.
func TestJoinAffectsDeduplicates(t *testing.T) {
	for _, tc := range []struct {
		in   []string
		want string
	}{
		{nil, "what octoscope shows"},
		{[]string{"", ""}, "what octoscope shows"},
		{[]string{"the PRs tab", "the PRs tab"}, "the PRs tab"},
		{[]string{"a", "b", "a"}, "a and b"},
		{[]string{"a", "b", "c"}, "a, b and c"},
	} {
		if got := joinAffects(tc.in); got != tc.want {
			t.Errorf("joinAffects(%v) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// Both gates, and the public-only one is the reason a recorded tape
// cannot pick up a warning line from a live incident.
func TestWantsServiceStatusGates(t *testing.T) {
	base := newStatusTestModel(t)

	base.checkServiceStatus = true
	if !base.wantsServiceStatus() {
		t.Error("enabled + not public-only should want the check")
	}

	off := base
	off.checkServiceStatus = false
	if off.wantsServiceStatus() {
		t.Error("check_service_status=false must opt out entirely")
	}

	pub := base
	pub.client.SetPublicOnly(true)
	if pub.wantsServiceStatus() {
		t.Error("public-only (screenshot / tape mode) must not check")
	}
	pub.client.SetPublicOnly(false)
}

// Not a poller: a recent answer is reused, and an opted-out session
// never issues the command at all.
func TestMaybeFetchServiceStatusRespectsTheTTL(t *testing.T) {
	m := newStatusTestModel(t)
	m.checkServiceStatus = true
	now := time.Now()

	if m.maybeFetchServiceStatus(now) == nil {
		t.Error("with nothing fetched yet, the command should be issued")
	}

	m.serviceStatusAt = now.Add(-serviceStatusTTL / 2)
	if m.maybeFetchServiceStatus(now) != nil {
		t.Error("a fresh answer must be reused, not refetched")
	}

	m.serviceStatusAt = now.Add(-serviceStatusTTL - time.Second)
	if m.maybeFetchServiceStatus(now) == nil {
		t.Error("past the TTL the command should be issued again")
	}

	m.checkServiceStatus = false
	if m.maybeFetchServiceStatus(now) != nil {
		t.Error("an opted-out session must issue nothing, TTL or not")
	}
}

// octoscope stops asserting a measurement it can no longer stand behind.
func TestVisibleServiceStatusExpires(t *testing.T) {
	m := newStatusTestModel(t)
	m.serviceStatus = impairedStatus()

	m.serviceStatusAt = time.Now()
	if m.visibleServiceStatus() == nil {
		t.Error("a just-fetched impairment should render")
	}

	m.serviceStatusAt = time.Now().Add(-serviceStatusMaxAge - time.Minute)
	if m.visibleServiceStatus() != nil {
		t.Error("an answer older than the max age must stop being asserted")
	}

	// A fetch that came back clean is not something to offer the
	// renderer either, even though every renderer re-guards: the
	// accessor's contract is "what may be shown", and a healthy status
	// is never that.
	m.serviceStatus, m.serviceStatusAt = healthyStatus(), time.Now()
	if m.visibleServiceStatus() != nil {
		t.Error("a healthy fetched status was offered to the renderer")
	}

	// A failed fetch stores nil, which must never fall back to the last
	// known state.
	m.serviceStatus, m.serviceStatusAt = nil, time.Now()
	if m.visibleServiceStatus() != nil {
		t.Error("a failed fetch must render nothing, not the previous answer")
	}
}

// The whole feature, through the real View: present when impaired,
// absent when not.
func TestDashboardShowsAndHidesTheStatusBanner(t *testing.T) {
	m := newStatusTestModel(t)
	m.activeTab = TabOverview

	clean := ansi.Strip(m.View())
	if strings.Contains(clean, "GitHub reports") {
		t.Errorf("a healthy session rendered a status banner:\n%s", clean)
	}

	m.serviceStatus = impairedStatus()
	m.serviceStatusAt = time.Now()
	out := ansi.Strip(m.View())
	if !strings.Contains(out, "GitHub reports partially down") {
		t.Errorf("the banner did not reach the dashboard:\n%s", out)
	}
}

// The wiring, not the function: a fetch failure is one of the two
// moments this feature exists for, and a command that is perfect and
// never issued is the failure mode that hides best. Counting through
// the seam records the decision itself, with no network involved.
func TestAFailedFetchAsksGitHubAboutGitHub(t *testing.T) {
	var asked int
	restore := serviceStatusCmd
	serviceStatusCmd = func() tea.Cmd { asked++; return nil }
	t.Cleanup(func() { serviceStatusCmd = restore })

	failed := github.FetchError{Reason: github.ReasonServer, Err: errServerForTest}

	for _, tc := range []struct {
		name     string
		err      error
		impaired bool
		want     int
	}{
		{"a failed fetch asks", &failed, false, 1},
		{"a healthy fetch while a banner is up asks, so it can clear", nil, true, 1},
		{"a healthy fetch on a healthy GitHub asks nothing", nil, false, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := newStatusTestModel(t)
			m.checkServiceStatus = true
			if tc.impaired {
				m.serviceStatus = impairedStatus()
			}
			asked = 0
			m.Update(fetchMsg{err: tc.err, manual: true, at: time.Now()})
			if asked != tc.want {
				t.Errorf("asked %d times, want %d", asked, tc.want)
			}
		})
	}

	// And the opt-out really opts out, at the source rather than in the
	// renderer.
	m := newStatusTestModel(t)
	m.checkServiceStatus = false
	asked = 0
	m.Update(fetchMsg{err: &failed, manual: true, at: time.Now()})
	if asked != 0 {
		t.Errorf("an opted-out session asked %d times", asked)
	}
}

// A failed status fetch clears the banner rather than leaving the last
// known state on screen: once octoscope cannot verify it, it stops
// saying it.
func TestAFailedStatusFetchClearsRatherThanKeeps(t *testing.T) {
	m := newStatusTestModel(t)
	m.serviceStatus = impairedStatus()
	m.serviceStatusAt = time.Now().Add(-time.Hour)

	updated, _ := m.Update(serviceStatusMsg{st: nil})
	got := updated.(Model)
	if got.serviceStatus != nil {
		t.Error("a failed status fetch kept the previous answer")
	}
	if got.serviceStatusAt.IsZero() {
		t.Error("the clock must be stamped even on failure, or repeated " +
			"failures become a poll of somebody else's status page")
	}
	if got.visibleServiceStatus() != nil {
		t.Error("nothing should render after a failed status fetch")
	}
}

var errServerForTest = errForTest("github said 502")

type errForTest string

func (e errForTest) Error() string { return string(e) }

// The error screen through the real View. The function above can be
// perfect while the screen never calls it, which is the half of a
// feature that fails without anything going red.
func TestErrorScreenCarriesTheDiagnosis(t *testing.T) {
	base := newStatusTestModel(t)
	base.stats = nil
	base.loading = false
	base.err = &github.FetchError{Reason: github.ReasonServer, Err: errServerForTest}
	base.errReason = github.ReasonServer

	clean := ansi.Strip(base.View())
	if !strings.Contains(clean, "GitHub had a hiccup") {
		t.Fatalf("precondition: this is not the error screen:\n%s", clean)
	}
	if strings.Contains(clean, "not octoscope") {
		t.Errorf("a healthy GitHub changed the error screen:\n%s", clean)
	}

	base.serviceStatus = impairedStatus()
	base.serviceStatusAt = time.Now()
	out := ansi.Strip(base.View())
	for _, want := range []string{
		"Very likely not octoscope",
		"Pull Requests and API Requests",
		// The original advice is added to, never replaced.
		"GitHub had a hiccup", "Press r to try again",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("error screen is missing %q:\n%s", want, out)
		}
	}
}

// A GitHub incident explains a 502 or a dead connection. It does not
// explain a rejected token or a spent rate-limit budget, and pinning
// those on GitHub would bury a correct diagnosis under an irrelevant
// one — the same crying-wolf failure the component selection avoids,
// one layer up.
func TestOnlyPlausibleFailuresGetBlamedOnGitHub(t *testing.T) {
	for _, tc := range []struct {
		reason github.FetchErrorReason
		want   bool
	}{
		{github.ReasonServer, true},
		{github.ReasonNetwork, true},
		{github.ReasonUnknown, true},
		{github.ReasonAuth, false},
		{github.ReasonAuthScope, false},
		{github.ReasonNotFound, false},
		{github.ReasonRateLimitPrimary, false},
		{github.ReasonRateLimitSecondary, false},
	} {
		if got := serviceStatusExplains(tc.reason); got != tc.want {
			t.Errorf("serviceStatusExplains(%v) = %v, want %v", tc.reason, got, tc.want)
		}
	}
}

// And the gate is actually wired into the screen, not merely available.
func TestErrorScreenWithheldForAnUnrelatedFailure(t *testing.T) {
	m := newStatusTestModel(t)
	m.stats = nil
	m.loading = false
	m.serviceStatus = impairedStatus()
	m.serviceStatusAt = time.Now()

	m.err = &github.FetchError{Reason: github.ReasonAuth, Err: errServerForTest}
	m.errReason = github.ReasonAuth
	out := ansi.Strip(m.View())
	if !strings.Contains(out, "Token expired") && !strings.Contains(out, "Authentication failed") {
		t.Fatalf("precondition: not the auth error screen:\n%s", out)
	}
	if strings.Contains(out, "not octoscope") {
		t.Errorf("a rejected token was blamed on a GitHub incident:\n%s", out)
	}

	// The same model, one reason over, does say it.
	m.err = &github.FetchError{Reason: github.ReasonServer, Err: errServerForTest}
	m.errReason = github.ReasonServer
	if out := ansi.Strip(m.View()); !strings.Contains(out, "not octoscope") {
		t.Errorf("a 502 during an incident said nothing:\n%s", out)
	}
}

// Toggling public-only with `p` mid-session must hide a warning that
// is already on screen, not merely stop the next fetch.
//
// The gate was written as a fetch-time decision, which is exactly wrong
// for the reason it exists: someone presses p precisely when they are
// about to show the screen to somebody. The update notice one line above
// it in the view has always been a render-time check; this now matches.
func TestPublicOnlyHidesAStatusWarningAlreadyOnScreen(t *testing.T) {
	m := newStatusTestModel(t)
	m.checkServiceStatus = true
	m.activeTab = TabOverview
	m.serviceStatus = impairedStatus()
	m.serviceStatusAt = time.Now()

	if !strings.Contains(ansi.Strip(m.View()), "GitHub reports") {
		t.Fatal("precondition: the banner should be up before pressing p")
	}

	// Through the real key path, not by setting the field.
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'p'}})
	got := updated.(Model)
	if !got.client.PublicOnly() {
		t.Fatal("precondition: p did not enter public-only mode")
	}
	defer got.client.SetPublicOnly(false)

	if out := ansi.Strip(got.View()); strings.Contains(out, "GitHub reports") {
		t.Errorf("the banner survived the switch into public-only mode:\n%s", out)
	}
}
