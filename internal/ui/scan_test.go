package ui

import (
	"strings"
	"testing"

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
	sm := ScanModel{}.Open(github.Repo{URL: "https://github.com/octocat/infected"})
	out := sm.View(80, 24)
	if !strings.Contains(out, "Scanning") {
		t.Errorf("loading view missing 'Scanning': %q", out)
	}
}

func TestScanModelViewLoaded(t *testing.T) {
	sm := ScanModel{}.
		Open(github.Repo{URL: "https://github.com/octocat/infected"}).
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
		Open(github.Repo{URL: "https://github.com/octocat/clean"}).
		applyFetched(&github.RepoScan{DefaultBranch: "main", Verdict: github.VerdictClean}, nil)
	if strings.Contains(clean.renderTitle(), "copy fix script") {
		t.Error("clean verdict must not advertise the fix-script key")
	}
}
