package github

import (
	"strings"
	"testing"
	"time"
)

func TestDetectPushBurst(t *testing.T) {
	base := time.Date(2026, 6, 3, 22, 38, 0, 0, time.UTC)
	mk := func(name string, offset time.Duration) Repo {
		return Repo{Name: name, PushedAt: base.Add(offset)}
	}

	tests := []struct {
		name      string
		repos     []Repo
		wantBurst bool
		wantCount int
	}{
		{
			name: "five repos within 49s is a burst",
			repos: []Repo{
				mk("a", 0),
				mk("b", 10*time.Second),
				mk("c", 20*time.Second),
				mk("d", 35*time.Second),
				mk("e", 49*time.Second),
			},
			wantBurst: true,
			wantCount: 5,
		},
		{
			name: "two repos never reaches the minimum",
			repos: []Repo{
				mk("a", 0),
				mk("b", 5*time.Second),
			},
			wantBurst: false,
		},
		{
			name: "three repos spread over a day is not a burst",
			repos: []Repo{
				mk("a", 0),
				mk("b", 8*time.Hour),
				mk("c", 20*time.Hour),
			},
			wantBurst: false,
		},
		{
			name: "tight trio inside a quiet history is a burst",
			repos: []Repo{
				mk("old1", -200*time.Hour),
				mk("old2", -100*time.Hour),
				mk("a", 0),
				mk("b", 15*time.Second),
				mk("c", 40*time.Second),
			},
			wantBurst: true,
			wantCount: 3,
		},
		{
			name: "repos with zero PushedAt are ignored",
			repos: []Repo{
				{Name: "never"},
				{Name: "never2"},
				mk("a", 0),
				mk("b", 10*time.Second),
			},
			wantBurst: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := DetectPushBurst(tt.repos, pushBurstMinRepos, pushBurstWindow)
			if ok != tt.wantBurst {
				t.Fatalf("DetectPushBurst ok = %v, want %v (got %+v)", ok, tt.wantBurst, got)
			}
			if ok && got.Count != tt.wantCount {
				t.Errorf("burst count = %d, want %d", got.Count, tt.wantCount)
			}
		})
	}
}

func TestMatchIgnition(t *testing.T) {
	tests := []struct {
		path      string
		wantMatch bool
		wantClass ignitionClass
	}{
		{".github/setup.js", true, classDropper},
		{".claude/settings.json", true, classAgentHook},
		{".cursor/rules/setup.mdc", true, classAgentInstr},
		{".github/copilot-instructions.md", true, classAgentInstr},
		{".github/workflows/ci.yml", true, classCI},
		{".vscode/tasks.json", true, classEditorTask},
		{"package.json", true, classPackage},
		{".devcontainer/devcontainer.json", true, classDevcontain},
		// Nested path must NOT match a single-segment glob.
		{".github/workflows/nested/ci.yml", false, ""},
		{"src/index.js", false, ""},
		{"README.md", false, ""},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			rule, ok := matchIgnition(tt.path)
			if ok != tt.wantMatch {
				t.Fatalf("matchIgnition(%q) ok = %v, want %v", tt.path, ok, tt.wantMatch)
			}
			if ok && rule.Class != tt.wantClass {
				t.Errorf("class = %q, want %q", rule.Class, tt.wantClass)
			}
		})
	}
}

func TestShannonEntropy(t *testing.T) {
	if e := shannonEntropy(nil); e != 0 {
		t.Errorf("entropy(nil) = %v, want 0", e)
	}
	if e := shannonEntropy([]byte("aaaaaaaa")); e != 0 {
		t.Errorf("entropy(uniform byte) = %v, want 0", e)
	}
	// Two equally-likely symbols → 1 bit/byte.
	if e := shannonEntropy([]byte("abababab")); e < 0.99 || e > 1.01 {
		t.Errorf("entropy(two symbols) = %v, want ~1.0", e)
	}
}

func TestLooksObfuscated(t *testing.T) {
	benign := []byte(`{"hooks":{"SessionStart":"echo hello"}}`)
	if m := looksObfuscated(benign); len(m) != 0 {
		t.Errorf("benign config flagged with markers %v", m)
	}

	packed := []byte(`const x = eval(atob("ZWNobyBwd25lZA==")); require("child_process").execSync(x)`)
	m := looksObfuscated(packed)
	joined := strings.Join(m, " | ")
	for _, want := range []string{"eval", "base64", "child process"} {
		if !strings.Contains(joined, want) {
			t.Errorf("packed payload missing marker %q (got %v)", want, m)
		}
	}

	longLine := []byte("var a=" + strings.Repeat("0123456789", 250) + ";")
	if m := looksObfuscated(longLine); len(m) == 0 {
		t.Errorf("very long single line not flagged")
	}
}

func TestIsTextContent(t *testing.T) {
	if !isTextContent([]byte("plain ascii config\n")) {
		t.Error("ascii not recognised as text")
	}
	if !isTextContent(nil) {
		t.Error("empty content should count as text")
	}
	if isTextContent([]byte{0x00, 0x01, 0x02, 0x03, 0xff, 0xfe}) {
		t.Error("binary content recognised as text")
	}
}

// --- engine: verdict matrix ---------------------------------------------

func provBranch(name string, isDefault bool) BranchProvenance {
	return BranchProvenance{
		Name:           name,
		IsDefault:      isDefault,
		TipOID:         "deadbeefcafebabe",
		Signed:         true,
		SignedByGitHub: false,
	}
}

func TestEvaluateScanClean(t *testing.T) {
	in := scanInput{
		Owner: "o", Name: "r", DefaultBranch: "main",
		BranchesTotal: 1,
		Branches: []scanBranch{
			{
				Prov: provBranch("main", true),
				Matches: []ignitionMatch{
					{Path: "package.json", Size: 1200, BlobSHA: "p", Rule: ignitionRule{Glob: "package.json", Class: classPackage, Weight: 0}},
					{Path: ".vscode/tasks.json", Size: 800, BlobSHA: "v", Rule: ignitionRule{Glob: ".vscode/tasks.json", Class: classEditorTask, Weight: 0}},
				},
			},
		},
		Blobs: map[string]blobAnalysis{
			"p": {Size: 1200, Fetched: true, IsText: true},
			"v": {Size: 800, Fetched: true, IsText: true},
		},
	}
	got := evaluateScan(in)
	if got.Verdict != VerdictClean {
		t.Fatalf("verdict = %v, want clean (score %d, findings %+v)", got.Verdict, got.Score, got.Findings)
	}
	if got.Score != 0 {
		t.Errorf("score = %d, want 0", got.Score)
	}
	if len(got.IgnitionInventory()) != 2 {
		t.Errorf("inventory = %d, want 2", len(got.IgnitionInventory()))
	}
	if len(got.ScoredFindings()) != 0 {
		t.Errorf("scored findings = %d, want 0", len(got.ScoredFindings()))
	}
}

func TestEvaluateScanWatch(t *testing.T) {
	in := scanInput{
		Owner: "o", Name: "r", DefaultBranch: "main",
		BranchesTotal: 1,
		Branches: []scanBranch{
			{
				Prov: provBranch("main", true),
				Matches: []ignitionMatch{
					{Path: ".claude/settings.json", Size: 400, BlobSHA: "c", Rule: ignitionRule{Class: classAgentHook, Weight: wIgnitionAgentHook}},
					{Path: ".gemini/settings.json", Size: 300, BlobSHA: "g", Rule: ignitionRule{Class: classAgentHook, Weight: wIgnitionAgentHook}},
				},
			},
		},
		Blobs: map[string]blobAnalysis{
			"c": {Size: 400, Fetched: true, IsText: true},
			"g": {Size: 300, Fetched: true, IsText: true},
		},
	}
	got := evaluateScan(in)
	if got.Verdict != VerdictWatch {
		t.Fatalf("verdict = %v, want watch (score %d)", got.Verdict, got.Score)
	}
}

func TestEvaluateScanCompromised(t *testing.T) {
	prov := provBranch("main", true)
	prov.Signed = false
	prov.SignedByGitHub = false
	prov.Bot = true
	prov.AuthorName = "github-actions"

	in := scanInput{
		Owner: "o", Name: "r", DefaultBranch: "main",
		BranchesTotal: 1,
		Branches: []scanBranch{
			{
				Prov: prov,
				Matches: []ignitionMatch{
					{Path: ".github/setup.js", Size: 4500000, BlobSHA: "d", Rule: ignitionRule{Class: classDropper, Weight: wIgnitionNamedIOC}},
				},
			},
		},
		Blobs: map[string]blobAnalysis{
			"d": {Size: 4500000, Fetched: false, Markers: []string{"eval() call", "base64 decode at runtime"}},
		},
	}
	got := evaluateScan(in)
	if got.Verdict != VerdictCompromised {
		t.Fatalf("verdict = %v, want compromised (score %d, findings %+v)", got.Verdict, got.Score, got.Findings)
	}
	// Expect: ignition(4) + oversized(4) + obfuscated(5) + spoof(5) + combined(4) = 22.
	if got.Score < tCompromised {
		t.Errorf("score = %d, want >= %d", got.Score, tCompromised)
	}
	var sawSpoof, sawCombined bool
	for _, f := range got.Findings {
		if f.Axis == AxisProvenance && strings.Contains(f.Reason, "forged") {
			sawSpoof = true
		}
		if strings.Contains(f.Reason, "coincide") {
			sawCombined = true
		}
	}
	if !sawSpoof {
		t.Error("expected a spoofed-identity finding")
	}
	if !sawCombined {
		t.Error("expected a combined smoking-gun finding")
	}
}

func TestEvaluateScanUnsignedDelta(t *testing.T) {
	// Default branch signed; a side branch carrying an agent hook is
	// unsigned → unsigned-delta + divergence fire.
	main := provBranch("main", true)
	main.Signed = true
	side := provBranch("next", false)
	side.Signed = false

	in := scanInput{
		Owner: "o", Name: "r", DefaultBranch: "main",
		BranchesTotal: 2,
		Branches: []scanBranch{
			{Prov: main},
			{
				Prov: side,
				Matches: []ignitionMatch{
					{Path: ".claude/settings.json", Size: 500, BlobSHA: "c", Rule: ignitionRule{Class: classAgentHook, Weight: wIgnitionAgentHook}},
				},
			},
		},
		Blobs: map[string]blobAnalysis{"c": {Size: 500, Fetched: true, IsText: true}},
	}
	got := evaluateScan(in)
	var sawUnsigned, sawDivergence bool
	for _, f := range got.Findings {
		if strings.Contains(f.Reason, "unsigned while the repo otherwise signs") {
			sawUnsigned = true
		}
		if strings.Contains(f.Reason, "divergence") {
			sawDivergence = true
		}
	}
	if !sawUnsigned {
		t.Error("expected an unsigned-delta finding")
	}
	if !sawDivergence {
		t.Error("expected a side-branch divergence finding")
	}
}
