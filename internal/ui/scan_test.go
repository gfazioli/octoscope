package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/gfazioli/octoscope/internal/github"
)

// compromisedScan builds a realistic "likely compromised" RepoScan for
// the render / remediation tests — the 2026-06 reference shape: an
// oversized dropper on the default branch with a forged bot tip.
func compromisedScan() *github.RepoScan {
	return &github.RepoScan{
		Owner: "octocat", Name: "infected",
		URL:           "https://github.com/octocat/infected",
		DefaultBranch: "main", BranchesScanned: 2, BranchesTotal: 2,
		Score: 22, Verdict: github.VerdictCompromised,
		Findings: []github.Finding{
			{Axis: github.AxisIgnition, Branch: "main", Path: ".github/setup.js", Weight: 4, Reason: "known dropper filename"},
			{Axis: github.AxisBlob, Branch: "main", Path: ".github/setup.js", Weight: 4, Reason: "oversized for its type: 4.3 MiB"},
			{Axis: github.AxisProvenance, Branch: "main", Weight: 5, Reason: `tip deadbee forged as "github-actions" but not signed by GitHub`},
		},
		Branches: []github.BranchProvenance{
			{Name: "main", IsDefault: true, TipOID: "deadbeefcafe", Bot: true, SignedByGitHub: false},
			{Name: "next", TipOID: "feedface1234", Signed: true},
		},
	}
}

// TestScanReportPartialCoverage pins #85: a scan that didn't reach every
// branch says so, and a Clean verdict with a gap is qualified so it can't
// read as a complete all-clear. A fully-scanned repo shows no such note.
func TestScanReportPartialCoverage(t *testing.T) {
	t.Run("clean but partial names the gap and qualifies the verdict", func(t *testing.T) {
		s := &github.RepoScan{
			Owner: "octocat", Name: "big", DefaultBranch: "main",
			BranchesScanned: 20, BranchesTotal: 250, Truncated: true,
			Verdict: github.VerdictClean,
		}
		out := ansi.Strip(ScanModel{scan: s}.computeBody(100))
		if !strings.Contains(out, "230 branches not scanned") {
			t.Errorf("report must state how many branches weren't scanned:\n%s", out)
		}
		if !strings.Contains(out, "covers only what was scanned") {
			t.Errorf("a clean-but-partial report must qualify the verdict:\n%s", out)
		}
	})

	t.Run("fully scanned clean shows no partial-coverage note", func(t *testing.T) {
		s := &github.RepoScan{
			Owner: "octocat", Name: "small", DefaultBranch: "main",
			BranchesScanned: 3, BranchesTotal: 3, Truncated: false,
			Verdict: github.VerdictClean,
		}
		out := ansi.Strip(ScanModel{scan: s}.computeBody(100))
		if strings.Contains(out, "Partial coverage") {
			t.Errorf("a complete scan must not claim partial coverage:\n%s", out)
		}
	})
}

func TestRemediationScript(t *testing.T) {
	script := remediationScript(compromisedScan())
	for _, want := range []string{
		"octocat/infected",
		"git clone --no-checkout",
		".github/setup.js",
		"settings/applications",
		"main", // the affected branch
	} {
		if !strings.Contains(script, want) {
			t.Errorf("remediation script missing %q\n---\n%s", want, script)
		}
	}
	// The article's load-bearing rule: reset, never revert (a revert
	// leaves the payload retrievable at the old commit).
	if strings.Contains(script, "git revert") {
		t.Error("remediation script must reset, not revert")
	}
}

func TestScanVerdictStyleDistinctGlyphs(t *testing.T) {
	seen := map[string]github.ScanVerdict{}
	for _, v := range []github.ScanVerdict{
		github.VerdictClean, github.VerdictWatch,
		github.VerdictSuspicious, github.VerdictCompromised,
	} {
		g, _ := scanVerdictStyle(v)
		if g == "" {
			t.Errorf("empty glyph for verdict %v", v)
		}
		if prev, dup := seen[g]; dup {
			t.Errorf("glyph %q reused for %v and %v", g, prev, v)
		}
		seen[g] = v
	}
}

func TestScanModelViewLoading(t *testing.T) {
	sm := ScanModel{}.Open(github.Repo{URL: "https://github.com/octocat/infected"}, nil, "")
	out := sm.View(80, 24)
	if !strings.Contains(out, "Scanning") {
		t.Errorf("loading view missing 'Scanning': %q", out)
	}
}

func TestScanModelViewLoaded(t *testing.T) {
	sm := ScanModel{}.
		Open(github.Repo{URL: "https://github.com/octocat/infected"}, nil, "").
		applyFetched(compromisedScan(), nil)

	// Large height so the whole body sits inside the viewport window.
	out := sm.View(120, 200)
	for _, want := range []string{"LIKELY COMPROMISED", "Findings", "setup.js", "Remediation"} {
		if !strings.Contains(out, want) {
			t.Errorf("loaded view missing %q", want)
		}
	}

	// The copy-fix-script key is advertised only when there's
	// something to remediate.
	if !strings.Contains(sm.renderTitle(), "copy fix script") {
		t.Error("compromised verdict should advertise the fix-script key")
	}
	clean := ScanModel{}.
		Open(github.Repo{URL: "https://github.com/octocat/clean"}, nil, "").
		applyFetched(&github.RepoScan{DefaultBranch: "main", Verdict: github.VerdictClean}, nil)
	if strings.Contains(clean.renderTitle(), "copy fix script") {
		t.Error("clean verdict must not advertise the fix-script key")
	}
}

// A weight-0 finding is invisible to both ScoredFindings and the
// ignition inventory, so without the Context section the first-run
// notice would never reach the screen — and a scan with no baseline
// would read as a confirmed "nothing changed". Assert on the rendered
// output, not on the data.
func TestScanViewShowsContextFindings(t *testing.T) {
	scan := &github.RepoScan{
		DefaultBranch: "main",
		Verdict:       github.VerdictClean,
		Findings: []github.Finding{{
			Axis:   github.AxisDelta,
			Weight: 0,
			Reason: "no previous scan of this repository to compare against — this scan records the baseline for the next one",
		}},
	}
	sm := ScanModel{}.
		Open(github.Repo{URL: "https://github.com/octocat/fresh"}, nil, "").
		applyFetched(scan, nil)

	out := sm.View(120, 200)
	for _, want := range []string{"Context", "no previous scan"} {
		if !strings.Contains(out, want) {
			t.Errorf("rendered report is missing %q:\n%s", want, out)
		}
	}
}

// Context rows must not carry a "+0" weight column: a warn-coloured
// zero contradicts the header that calls them verdict-neutral, and the
// scored rows above are the only place a weight belongs.
func TestContextRowsCarryNoWeightColumn(t *testing.T) {
	scan := &github.RepoScan{
		DefaultBranch: "main",
		Verdict:       github.VerdictWatch,
		Score:         2,
		Findings: []github.Finding{
			{Axis: github.AxisIgnition, Branch: "main", Path: ".claude/settings.json", Weight: 2, Reason: "agent hook present"},
			{Axis: github.AxisDelta, Weight: 0, Reason: "no previous scan of this repository to compare against"},
		},
	}
	out := ansi.Strip(ScanModel{scan: scan}.computeBody(120))
	if strings.Contains(out, "+0") {
		t.Errorf("a weight-0 context row rendered a +0 column:\n%s", out)
	}
	// The scored row must still show its weight.
	if !strings.Contains(out, "+2") {
		t.Errorf("scored finding lost its weight column:\n%s", out)
	}
}

// Probes that could not run must be named in the report. Failing open
// keeps the scan working on a minimal token; staying quiet about it
// would let a clean verdict read as a complete one.
func TestScanViewDeclaresUncheckedProbes(t *testing.T) {
	scan := &github.RepoScan{
		DefaultBranch: "main",
		Verdict:       github.VerdictClean,
		Unchecked: []github.UncheckedProbe{
			{Name: "deploy keys", Reason: "the token lacks the scope this needs"},
			{Name: "self-hosted runners", Reason: "the token lacks the scope this needs"},
		},
	}
	out := ansi.Strip(ScanModel{scan: scan}.computeBody(140))
	for _, want := range []string{
		"Not checked",
		"deploy keys",
		"self-hosted runners",
		"covers only what was checked",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("report does not disclose %q:\n%s", want, out)
		}
	}
}
