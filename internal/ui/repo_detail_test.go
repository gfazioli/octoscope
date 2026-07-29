package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/gfazioli/octoscope/internal/github"
)

// newLoadedRepoDetail builds a drill-in already holding a fetched
// payload, which is the only state that renders a body.
func newLoadedRepoDetail(d *github.RepoDetail) RepoDetailModel {
	rd := RepoDetailModel{}.Open(github.Repo{
		Name: "octoscope",
		URL:  "https://github.com/gfazioli/octoscope",
	}, StarModeDensity)
	return rd.applyFetched(d, nil)
}

// TestRepoDetailChecksSection covers the wiring, which the
// renderCheckList unit tests can't reach: that the drill-in actually
// paints a Checks section, with the rollup summary on the heading and
// the failing check in the list.
func TestRepoDetailChecksSection(t *testing.T) {
	_ = applyTheme("octoscope", "")

	rd := newLoadedRepoDetail(&github.RepoDetail{
		Owner:   "gfazioli",
		Name:    "octoscope",
		URL:     "https://github.com/gfazioli/octoscope",
		CIState: "FAILURE",
		Checks: []github.CheckSummary{
			{Name: "lint", Conclusion: "SUCCESS"},
			{Name: "govulncheck", Conclusion: "FAILURE", URL: "https://github.com/gfazioli/octoscope/actions/runs/1"},
		},
		ChecksTotal: 12,
	})

	body := rd.computeBody(100)
	plain := ansi.Strip(body)

	if !strings.Contains(plain, "Checks") {
		t.Fatalf("no Checks heading in the drill-in body:\n%s", plain)
	}
	if !strings.Contains(plain, "failing") {
		t.Error("the rollup summary should read 'failing' next to the heading")
	}
	if !strings.Contains(plain, "govulncheck") || !strings.Contains(plain, "lint") {
		t.Errorf("check names missing from the body:\n%s", plain)
	}
	// 12 exist, 2 fetched, both shown -> 10 hidden.
	if !strings.Contains(plain, "+10 more") {
		t.Errorf("overflow should count against ChecksTotal:\n%s", plain)
	}
	// The failing check's run must be a live hyperlink target, not just
	// text — that is the second half of what #34 asked for.
	if !strings.Contains(body, ansi.SetHyperlink("https://github.com/gfazioli/octoscope/actions/runs/1")) {
		t.Error("the failing check's run URL should be an OSC 8 target in the rendered body")
	}
}

// TestRepoDetailNoChecksSection is the other half: a repo with no CI at
// all shows no heading, rather than an empty section. Absence is how the
// drill-in says "no checks here" — the same convention the release,
// languages and topics sections follow.
func TestRepoDetailNoChecksSection(t *testing.T) {
	_ = applyTheme("octoscope", "")

	rd := newLoadedRepoDetail(&github.RepoDetail{
		Owner: "gfazioli",
		Name:  "octoscope",
		URL:   "https://github.com/gfazioli/octoscope",
		// no CIState, no Checks
	})

	if plain := ansi.Strip(rd.computeBody(100)); strings.Contains(plain, "Checks") {
		t.Errorf("a repo with no rollup must not render a Checks heading:\n%s", plain)
	}
}

// TestRepoDetailChecksMonochrome guards the theme contract at the
// section level: under a monochromatic theme the section still has to
// distinguish the failure, which it can only do by glyph.
func TestRepoDetailChecksMonochrome(t *testing.T) {
	_ = applyTheme("monochrome", "")
	t.Cleanup(func() { _ = applyTheme("octoscope", "") })

	rd := newLoadedRepoDetail(&github.RepoDetail{
		Owner:   "gfazioli",
		Name:    "octoscope",
		URL:     "https://github.com/gfazioli/octoscope",
		CIState: "FAILURE",
		Checks: []github.CheckSummary{
			{Name: "lint", Conclusion: "SUCCESS"},
			{Name: "govulncheck", Conclusion: "FAILURE"},
		},
		ChecksTotal: 2,
	})

	plain := ansi.Strip(rd.computeBody(100))
	if !strings.Contains(plain, "✗") || !strings.Contains(plain, "✓") {
		t.Errorf("monochrome section must still separate pass from fail by glyph:\n%s", plain)
	}
}
